package agent

import (
	"encoding/json"
	"fmt"

	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

// debugLLMReq records the governed request actually sent to the model for this
// step — the "what did the model see" fact the protocol audit doesn't capture.
// No-op when debug tracing is off.
func (l *Loop) debugLLMReq(step int, modelMessages []llm.Message) {
	if l.debugLogger == nil {
		return
	}
	rec := observe.Record{
		T:          "llm_req",
		Step:       step,
		MsgCount:   len(modelMessages),
		Tools:      len(l.toolSpecs()),
		TotalBytes: messageTotalBytes(modelMessages),
	}
	if len(modelMessages) > 0 && modelMessages[0].Role == llm.RoleSystem {
		rec.SystemBytes = len(modelMessages[0].Content)
	}
	rec.Payload = governedPayload(modelMessages, l.debugLogger.Verbose())
	l.debugLogger.Log(rec)
}

// debugLLMResp records the model response for this step: text size, requested
// tool calls, the terminal stop_reason, and the prompt-cache-aware token
// breakdown. stop_reason is logged so truncation (max_tokens / length) is
// diagnosable straight from the trace instead of having to be inferred from
// out tokens hitting the max.
func (l *Loop) debugLLMResp(step int, text string, requested []llm.ToolCall, stopReason string, usage *llm.Usage) {
	if l.debugLogger == nil {
		return
	}
	rec := observe.Record{
		T:          "llm_resp",
		Step:       step,
		TextBytes:  len(text),
		StopReason: stopReason,
	}
	if len(requested) > 0 {
		tcs := make([]observe.ToolCallRec, 0, len(requested))
		for _, tc := range requested {
			tcs = append(tcs, observe.ToolCallRec{Name: tc.Name, ArgsBytes: len(tc.Arguments)})
		}
		rec.ToolCalls = tcs
	}
	if usage != nil {
		rec.Usage = &observe.UsageRec{
			In:          usage.InputTokens,
			Out:         usage.OutputTokens,
			CacheRead:   usage.CacheReadInputTokens,
			CacheCreate: usage.CacheCreationInputTokens,
		}
	}
	l.debugLogger.Log(rec)
}

func messageTotalBytes(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content)
		for _, tc := range m.ToolCalls {
			total += len(tc.Arguments)
		}
	}
	return total
}

func governedPayload(msgs []llm.Message, verbose observe.Verbose) string {
	data, err := json.Marshal(msgs)
	if err != nil {
		return ""
	}
	s := string(data)
	if verbose >= observe.VerboseFull || len(s) <= observe.PayloadCap {
		return s
	}
	return s[:observe.PayloadCap] + "...[payload truncated]"
}

// safetyDecisionLabel maps a safety.Decision to the short string used in debug
// tool/decide records.
func safetyDecisionLabel(d safety.Decision) string {
	switch d {
	case safety.Allow:
		return "allow"
	case safety.Confirm, safety.ConfirmHigh:
		return "confirm"
	case safety.Block:
		return "blocked"
	default:
		return "unknown"
	}
}

// debugTool records one tool decision/result for the debug trace. No-op when
// debug tracing is off.
func (l *Loop) debugTool(step int, name string, argsBytes int, risk, decision string, approved bool, durMs int64, outBytes int, errMsg string) {
	if l.debugLogger == nil {
		return
	}
	l.debugLogger.Log(observe.Record{
		T:         "tool",
		Step:      step,
		Name:      name,
		ArgsBytes: argsBytes,
		Risk:      risk,
		Decision:  decision,
		Approved:  approved,
		DurMs:     durMs,
		OutBytes:  outBytes,
		Error:     errMsg,
	})
}

// debugSafetyDecide records the safety policy decision for a tool call.
func (l *Loop) debugSafetyDecide(step int, name string, risk tool.RiskLevel, decision safety.Decision) {
	if l.debugLogger == nil {
		return
	}
	l.debugLogger.Log(observe.Record{
		T:        "decide",
		Step:     step,
		Kind:     "safety",
		Name:     name,
		Risk:     string(risk),
		Decision: safetyDecisionLabel(decision),
	})
}

// debugContextDecide records the context-governance decision (token before/after,
// snipped messages, summary) for one governing pass.
func (l *Loop) debugContextDecide(step int, governed contextx.GovernResult) {
	if l.debugLogger == nil {
		return
	}
	l.debugLogger.Log(observe.Record{
		T:      "decide",
		Step:   step,
		Kind:   "context",
		Detail: fmt.Sprintf("before=%d after=%d tokens; micro=%d spill=%d; %s", governed.BeforeTokens, governed.AfterTokens, governed.CompactedToolResults, governed.TruncatedToolResults, governed.Summary()),
	})
}
