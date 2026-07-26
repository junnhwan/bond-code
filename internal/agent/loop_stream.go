package agent

import (
	"context"
	"strings"

	"github.com/junnhwan/bond-code/internal/llm"
)

// modelResponse is the classified result of one model stream. It deliberately
// carries protocol facts only; the ReAct loop decides whether to retry,
// finalize, or execute tools.
type modelResponse struct {
	text           string
	toolCalls      []llm.ToolCall
	usage          *llm.Usage
	stopReason     string
	degenerate     bool
	healthyTextLen int
	sawReasoning   bool
}

func (l *Loop) collectModelResponse(ctx context.Context, messages []llm.Message, tools []llm.ToolSpec, emit EventSink) (modelResponse, error) {
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	chunks, errs := l.client.Stream(streamCtx, messages, tools)
	guardCfg := textGuardConfig{
		MaxRepeatedTextChunks:     l.cfg.MaxRepeatedTextChunks,
		MaxRepeatedTextSubstrings: l.cfg.MaxRepeatedTextSubstrings,
	}
	textGuard := newTextGuard(guardCfg)
	reasoningGuard := newTextGuard(reasoningTextGuardConfig(guardCfg))

	var response modelResponse
	var answer strings.Builder
	reasoningLen := 0
	for chunk := range chunks {
		if response.degenerate {
			// The breaker has cancelled the provider stream. Drain until the
			// producer closes so its goroutine cannot be stranded.
			continue
		}
		if chunk.Usage != nil {
			response.usage = chunk.Usage
			emitMeasuredUsage(emit, chunk.Usage, false)
		}
		if chunk.Content != "" {
			answer.WriteString(chunk.Content)
			emit(Event{Type: EventModelChunk, Message: chunk.Content})
			if degenerate, healthyLen := textGuard.Saw(chunk.Content, answer.Len()); degenerate {
				response.degenerate = true
				response.healthyTextLen = healthyLen
				emit(Event{Type: EventTextDegeneration, Message: textDegenerationNotice})
				cancelStream()
			}
		}
		if chunk.Reasoning != "" && !response.degenerate {
			response.sawReasoning = true
			reasoningLen += len(chunk.Reasoning)
			emit(Event{Type: EventReasoningChunk, Message: chunk.Reasoning})
			if degenerate, _ := reasoningGuard.Saw(chunk.Reasoning, reasoningLen); degenerate {
				response.degenerate = true
				response.healthyTextLen = answer.Len()
				emit(Event{Type: EventTextDegeneration, Message: reasoningDegenerationNotice})
				cancelStream()
			}
		}
		if chunk.ToolCall != nil {
			response.toolCalls = append(response.toolCalls, *chunk.ToolCall)
			emit(Event{
				Type:       EventToolRequested,
				Message:    chunk.ToolCall.Arguments,
				ToolName:   chunk.ToolCall.Name,
				ToolCallID: chunk.ToolCall.ID,
				Input:      chunk.ToolCall.Arguments,
			})
		}
		if chunk.Done {
			response.stopReason = chunk.StopReason
		}
	}
	response.text = answer.String()

	err := <-errs
	if response.degenerate {
		// Cancellation is the expected breaker signal, not a provider failure.
		err = nil
	}
	if err == nil {
		emitMeasuredUsage(emit, response.usage, true)
	}
	return response, err
}
