package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/llm"
)

const (
	stepBudgetFinalizationReminder     = "Tool step budget has been reached. Tools are unavailable for this finalization turn. Use the existing tool results to return only a concise synthesis with concrete evidence and any unfinished items. Do not emit a tool call, tool-protocol markup such as <tool_call>/<arg_key>, or copy a raw tool result verbatim. Stop after the synthesis."
	stepBudgetFinalizationRepair       = "Your previous finalization was unusable (%s). Do not try another tool. Using only the evidence already present, write a short standalone answer now: findings, supporting file/command evidence, and any unfinished item. Emit ordinary prose only; no tool syntax and no verbatim raw tool output."
	textDegenerationReminder           = "The previous response degenerated into repeated output. Do not repeat yourself. Summarize what was established before the repetition began and answer the user concisely, then stop. Do not call tools."
	textDegenerationNotice             = "text degeneration circuit breaker: model output degenerated into repetition; stream cancelled, recovering"
	reasoningDegenerationNotice        = "text degeneration circuit breaker: model reasoning degenerated into repetition; stream cancelled, recovering"
	noProgressFinalizationReminder     = "The repeated-tool no-progress guard has stopped tool execution. Do not call more tools. Use the existing tool results to answer the user directly, briefly explain anything that could not be completed, and stop."
	skippedAfterNoProgressGuardMessage = "loop_guard: skipped because an earlier call in this response triggered the repeated-call no-progress guard."
)

// unusableFinalizationError identifies model output that is safe to retry once:
// empty/truncated text, tool calls or tool-protocol markup, and verbatim replay
// of the latest tool output. Transport and degeneration failures are not
// retried because another request is unlikely to repair them.
type unusableFinalizationError struct{ reason string }

func (e *unusableFinalizationError) Error() string { return "unusable finalization: " + e.reason }

// finalizeWithReminder runs one no-tools recovery stream. The finalization
// stream is guarded on both visible text and reasoning, just like the main
// model stream. Native tool calls are observed but never emitted as requested
// tools or executed, preserving tool-use/result pairing.
func (l *Loop) finalizeWithReminder(ctx context.Context, messages []llm.Message, emit EventSink, reminder string, stepLabel int) (string, error) {
	return l.finalizeWithReminderAvoiding(ctx, messages, emit, reminder, stepLabel, "")
}

func (l *Loop) finalizeWithReminderAvoiding(ctx context.Context, messages []llm.Message, emit EventSink, reminder string, stepLabel int, disallowedOutput string) (string, error) {
	finalizationMessages := appendReminderToSystem(messages, reminder)
	modelMessages := finalizationMessages
	if l.contextManager != nil {
		modelMessages = l.prepareModelMessages(ctx, finalizationMessages, emit, stepLabel)
	}
	l.debugLLMReq(stepLabel, modelMessages)
	var lastUsage *llm.Usage
	var stopReason string
	var toolCalls []llm.ToolCall
	streamCtx, cancelStream := context.WithCancel(ctx)
	chunks, errs := l.client.Stream(streamCtx, modelMessages, nil)
	var answer strings.Builder
	reasoningLen := 0
	degenerate := false
	guardCfg := textGuardConfig{
		MaxRepeatedTextChunks:     l.cfg.MaxRepeatedTextChunks,
		MaxRepeatedTextSubstrings: l.cfg.MaxRepeatedTextSubstrings,
	}
	textGuard := newTextGuard(guardCfg)
	reasoningGuard := newTextGuard(reasoningTextGuardConfig(guardCfg))
	for chunk := range chunks {
		if degenerate {
			continue
		}
		if chunk.Usage != nil {
			lastUsage = chunk.Usage
			emitMeasuredUsage(emit, chunk.Usage, false)
		}
		if chunk.Content != "" {
			answer.WriteString(chunk.Content)
			emit(Event{Type: EventModelChunk, Message: chunk.Content})
			if d, _ := textGuard.Saw(chunk.Content, answer.Len()); d {
				degenerate = true
				emit(Event{Type: EventTextDegeneration, Message: "finalization " + textDegenerationNotice})
				cancelStream()
			}
		}
		if chunk.Reasoning != "" && !degenerate {
			reasoningLen += len(chunk.Reasoning)
			emit(Event{Type: EventReasoningChunk, Message: chunk.Reasoning})
			if d, _ := reasoningGuard.Saw(chunk.Reasoning, reasoningLen); d {
				degenerate = true
				emit(Event{Type: EventTextDegeneration, Message: "finalization " + reasoningDegenerationNotice})
				cancelStream()
			}
		}
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}
		if chunk.Done {
			stopReason = chunk.StopReason
		}
	}
	var streamErr error
	for err := range errs {
		if err != nil {
			streamErr = err
		}
	}
	cancelStream()
	emitMeasuredUsage(emit, lastUsage, true)
	l.debugLLMResp(stepLabel, answer.String(), toolCalls, stopReason, lastUsage)
	if degenerate {
		return "", fmt.Errorf("finalization output degenerated into repetition")
	}
	if streamErr != nil {
		return "", fmt.Errorf("finalization stream failed: %w", streamErr)
	}
	final := strings.TrimSpace(answer.String())
	if err := validateFinalization(final, toolCalls, stopReason, disallowedOutput); err != nil {
		return "", err
	}
	return final, nil
}

func validateFinalization(final string, toolCalls []llm.ToolCall, stopReason, disallowedOutput string) error {
	if len(toolCalls) > 0 {
		return &unusableFinalizationError{reason: "model attempted a tool call after tools were disabled"}
	}
	if final == "" {
		return &unusableFinalizationError{reason: "model produced no visible output"}
	}
	if isTruncatedStopReason(stopReason) || stopReason == "tool_use" {
		return &unusableFinalizationError{reason: fmt.Sprintf("incomplete stop_reason %q", stopReason)}
	}
	lower := strings.ToLower(final)
	if startsWithToolProtocol(final) {
		return &unusableFinalizationError{reason: "model emitted tool-protocol markup instead of an answer"}
	}
	if strings.HasPrefix(lower, "step budget reached after tool use; returning the latest available tool result") {
		return &unusableFinalizationError{reason: "model emitted the legacy raw-tool fallback"}
	}
	if raw := normalizeFinalizationText(disallowedOutput); raw != "" && normalizeFinalizationText(final) == raw {
		return &unusableFinalizationError{reason: "model replayed the latest raw tool result verbatim"}
	}
	return nil
}

func normalizeFinalizationText(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
}

func startsWithToolProtocol(text string) bool {
	candidate := strings.ToLower(normalizeFinalizationText(text))
	if strings.HasPrefix(candidate, "```") {
		if newline := strings.IndexByte(candidate, '\n'); newline >= 0 {
			candidate = strings.TrimSpace(candidate[newline+1:])
		}
	}
	for _, marker := range []string{"<tool_call", "<arg_key", "<arg_value", "<function_calls", "<invoke"} {
		if strings.HasPrefix(candidate, marker) {
			return true
		}
	}
	return false
}

func (l *Loop) finalizeAfterNoProgressGuard(ctx context.Context, messages []llm.Message, emit EventSink, step int) (string, error) {
	return l.finalizeWithReminder(ctx, messages, emit, noProgressFinalizationReminder, step)
}

func (l *Loop) finalizeAfterStepBudget(ctx context.Context, messages []llm.Message, emit EventSink, latestToolOutput string) (string, error) {
	final, err := l.finalizeWithReminderAvoiding(ctx, messages, emit, stepBudgetFinalizationReminder, l.cfg.MaxSteps, latestToolOutput)
	if err == nil {
		return final, nil
	}
	var unusable *unusableFinalizationError
	if !errors.As(err, &unusable) {
		return "", err
	}
	repair := fmt.Sprintf(stepBudgetFinalizationRepair, unusable.reason)
	return l.finalizeWithReminderAvoiding(ctx, messages, emit, repair, l.cfg.MaxSteps+1, latestToolOutput)
}

// finalizeAfterTextDegeneration gives the model one recovery turn after the
// text-degeneration breaker tripped: a no-tools reminder asks it to summarize
// what was established before the repetition and stop, instead of returning the
// garbled repeated output as the final answer.
func (l *Loop) finalizeAfterTextDegeneration(ctx context.Context, messages []llm.Message, emit EventSink, step int) (string, error) {
	return l.finalizeWithReminder(ctx, messages, emit, textDegenerationReminder, step)
}

// truncateDegenerate cuts a degenerated answer to the boundary captured just
// before the repeating run started (the first copy of the repeated token may be
// retained, which is harmless).
func truncateDegenerate(text string, healthyLen int) string {
	if healthyLen <= 0 || healthyLen >= len(text) {
		return text
	}
	return text[:healthyLen]
}

// textDegenerationFallback is the final answer when recovery yields nothing:
// the truncated healthy prefix plus a notice that the breaker tripped.
func textDegenerationFallback(truncated string) string {
	return strings.TrimSpace(truncated) + "\n\n[output degenerated into repetition — circuit breaker tripped, stream cancelled]"
}
