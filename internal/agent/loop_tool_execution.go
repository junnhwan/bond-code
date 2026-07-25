package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/junnhwan/bond-code/internal/hook"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

func (l *Loop) executeTool(ctx context.Context, call *llm.ToolCall, emit EventSink, step int) (*tool.Result, error) {
	t, ok := l.tools.Get(call.Name)
	if !ok {
		l.debugTool(step, call.Name, len(call.Arguments), "", "unknown", false, 0, 0, fmt.Sprintf("tool %q not found", call.Name))
		return emitToolError(emit, call, "", "", tool.StatusError, fmt.Errorf("tool %q not found", call.Name)), nil
	}
	if l.isToolDisabled(call.Name) {
		emit(Event{
			Type:       EventToolRejected,
			Message:    "disabled in plan mode",
			ToolName:   t.Name(),
			ToolCallID: call.ID,
			Input:      call.Arguments,
		})
		l.debugTool(step, t.Name(), len(call.Arguments), "", "disabled", false, 0, 0, "disabled in plan mode")
		return emitToolError(emit, call, t.Name(), "", tool.StatusBlocked, fmt.Errorf("tool %q is disabled in plan mode", call.Name)), nil
	}
	risk := t.Risk([]byte(call.Arguments))
	decision := l.policy.Decide(t.Name(), risk, call.Arguments)
	l.debugSafetyDecide(step, t.Name(), risk, decision)
	switch decision {
	case safety.Block:
		emit(Event{
			Type:       EventToolRejected,
			Message:    "blocked by policy",
			ToolName:   t.Name(),
			ToolCallID: call.ID,
			Risk:       string(risk),
			Input:      call.Arguments,
		})
		l.debugTool(step, t.Name(), len(call.Arguments), string(risk), "blocked", false, 0, 0, "blocked by safety policy")
		return emitToolError(emit, call, t.Name(), string(risk), tool.StatusBlocked, fmt.Errorf("tool %q blocked by safety policy", t.Name())), nil
	case safety.Confirm, safety.ConfirmHigh:
		req := safety.ConfirmationRequest{
			ToolName: t.Name(),
			Risk:     string(risk),
			Summary:  fmt.Sprintf("%s with %s", t.Name(), call.Arguments),
			Detail:   call.Arguments,
			Input:    call.Arguments,
		}
		emit(Event{
			Type:       EventToolConfirmationRequested,
			Message:    req.Summary,
			ToolName:   t.Name(),
			ToolCallID: call.ID,
			Risk:       string(risk),
			Input:      call.Arguments,
		})
		resp, err := l.confirm(ctx, req)
		if err != nil {
			return nil, err
		}
		if !resp.Approved {
			// Phase 5A: a reject reason, when the confirmer captured one, is
			// surfaced both in the audit event and in the tool-result envelope
			// so the model can adjust on its next turn. An empty reason reads
			// as a plain "user declined" — both are protocol-safe envelopes.
			rejectMsg := "rejected by user"
			rejectErr := fmt.Errorf("tool %q rejected", t.Name())
			if resp.RejectReason != "" {
				rejectMsg = "rejected by user: " + resp.RejectReason
				rejectErr = fmt.Errorf("tool %q rejected by user: %s", t.Name(), resp.RejectReason)
			}
			emit(Event{
				Type:       EventToolRejected,
				Message:    rejectMsg,
				ToolName:   t.Name(),
				ToolCallID: call.ID,
				Risk:       string(risk),
				Input:      call.Arguments,
			})
			l.debugTool(step, t.Name(), len(call.Arguments), string(risk), "rejected", false, 0, 0, rejectMsg)
			return emitToolError(emit, call, t.Name(), string(risk), tool.StatusRejected, rejectErr), nil
		}
		emit(Event{
			Type:       EventToolApproved,
			Message:    "approved",
			ToolName:   t.Name(),
			ToolCallID: call.ID,
			Risk:       string(risk),
			Input:      call.Arguments,
		})
	case safety.Allow:
		emit(Event{
			Type:       EventToolApproved,
			Message:    "allowed",
			ToolName:   t.Name(),
			ToolCallID: call.ID,
			Risk:       string(risk),
			Input:      call.Arguments,
		})
	default:
		l.debugTool(step, t.Name(), len(call.Arguments), string(risk), "error", false, 0, 0, fmt.Sprintf("unknown safety decision %q", decision))
		return emitToolError(emit, call, t.Name(), string(risk), tool.StatusError, fmt.Errorf("unknown safety decision %q", decision)), nil
	}

	// PreToolUse 钩子：safety 已批准，钩子可额外 block 或改写 input。
	toolName := t.Name()
	execArgs := call.Arguments
	if l.hooks != nil {
		preDecision, modified := l.hooks.RunPreToolUse(ctx, hook.PreToolUseInput{
			ToolName: toolName,
			Input:    call.Arguments,
			Risk:     string(risk),
		})
		if preDecision.IsBlocking() {
			emit(Event{
				Type:       EventToolRejected,
				Message:    "blocked by hook: " + preDecision.Reason,
				ToolName:   toolName,
				ToolCallID: call.ID,
				Risk:       string(risk),
				Input:      call.Arguments,
			})
			l.debugTool(step, toolName, len(call.Arguments), string(risk), "blocked", false, 0, 0, "blocked by hook: "+preDecision.Reason)
			return emitToolError(emit, call, toolName, string(risk), tool.StatusBlocked, fmt.Errorf("tool %q blocked by hook: %s", toolName, preDecision.Reason)), nil
		}
		execArgs = modified
	}

	start := time.Now()
	result, err := t.Execute(ctx, []byte(execArgs))
	durMs := time.Since(start).Milliseconds()
	if err != nil {
		l.debugTool(step, toolName, len(execArgs), string(risk), safetyDecisionLabel(decision), false, durMs, 0, err.Error())
		return emitToolError(emit, call, toolName, string(risk), tool.StatusError, err), nil
	}
	result = tool.NormalizeResult(result, toolName)

	// PostToolUse 钩子：可改写工具输出。
	if l.hooks != nil {
		postIn := l.hooks.RunPostToolUse(ctx, hook.PostToolUseInput{
			ToolName: toolName,
			Input:    execArgs,
			Output:   result.Output,
			OK:       result.OK,
			Error:    result.Error,
		})
		if postIn.Output != result.Output {
			result.Output = postIn.Output
		}
	}

	emit(Event{
		Type:       EventToolResult,
		Message:    result.Output,
		ToolName:   toolName,
		ToolCallID: call.ID,
		Risk:       string(risk),
		Input:      execArgs,
		Output:     result.Output,
		Error:      result.Error,
	})
	l.debugTool(step, toolName, len(execArgs), string(risk), safetyDecisionLabel(decision), true, durMs, len(result.Output), result.Error)
	return result, nil
}

func emitToolError(emit EventSink, call *llm.ToolCall, toolName string, risk string, status string, err error) *tool.Result {
	if toolName == "" {
		toolName = call.Name
	}
	output := "error: " + err.Error()
	var result *tool.Result
	switch status {
	case tool.StatusBlocked:
		result = tool.Blocked(toolName, "tool blocked", err.Error())
	case tool.StatusRejected:
		result = tool.Rejected(toolName, "tool rejected", err.Error())
	default:
		result = tool.ErrorResult(toolName, "tool error", err.Error())
	}
	emit(Event{
		Type:       EventToolResult,
		Message:    output,
		ToolName:   toolName,
		ToolCallID: call.ID,
		Risk:       risk,
		Input:      call.Arguments,
		Output:     output,
		Error:      err.Error(),
	})
	return result
}

func emitGuardedToolResult(emit EventSink, call *llm.ToolCall, output string) *tool.Result {
	result := tool.Guarded(call.Name, "loop guard", output)
	emit(Event{
		Type:       EventLoopGuard,
		Message:    output,
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Input:      call.Arguments,
		Output:     output,
		Error:      output,
	})
	emit(Event{
		Type:       EventToolResult,
		Message:    output,
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Input:      call.Arguments,
		Output:     output,
		Error:      output,
	})
	return result
}

func emitMeasuredUsage(emit EventSink, usage *llm.Usage, final bool) {
	if usage == nil {
		return
	}
	emit(Event{
		Type:                 EventContextMeasured,
		MeasuredInputTokens:  usage.InputTokens,
		MeasuredOutputTokens: usage.OutputTokens,
		MeasuredUsageFinal:   final,
	})
}

func modelToolResultContent(result *tool.Result) string {
	normalized := tool.NormalizeResult(result, "")
	data, err := json.Marshal(normalized)
	if err != nil {
		fallback := tool.ErrorResult(normalized.ToolName, "tool result encoding failed", err.Error())
		data, _ = json.Marshal(fallback)
	}
	return string(data)
}

func (l *Loop) toolSpecs() []llm.ToolSpec {
	names := l.tools.Names()
	specs := make([]llm.ToolSpec, 0, len(names))
	for _, name := range names {
		t, _ := l.tools.Get(name)
		specs = append(specs, llm.ToolSpec{Name: t.Name(), Description: t.Description(), Schema: t.Schema()})
	}
	return specs
}
