package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

// TestLoopRetriesEmptyResponseThenSucceeds: a max_tokens-truncated empty
// response (no text, no tool call) is retried with a nudge instead of silently
// ending the run.
func TestLoopRetriesEmptyResponseThenSucceeds(t *testing.T) {
	empty := []llm.Chunk{{Done: true, StopReason: "max_tokens"}}
	ok := []llm.Chunk{{Content: "done"}, {Done: true, StopReason: "end_turn"}}
	client := llmfake.New([][]llm.Chunk{empty, ok})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	var sawNudge bool
	result, err := loop.RunWithEvents(context.Background(), "hi", func(e Event) {
		if e.Type == EventContextUpdated && strings.Contains(e.Message, "incomplete model response") {
			sawNudge = true
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("expected final answer %q, got %q", "done", result.FinalAnswer)
	}
	if !sawNudge {
		t.Fatal("expected an incomplete-response nudge event before the successful retry")
	}
}

// TestLoopRetriesTruncatedTextThenSucceeds reproduces the second interruption:
// max_tokens cut the response off mid-text (non-empty partial) with no tool call
// made. The loop must still detect this as incomplete via stop_reason and retry,
// rather than handing the half-sentence to the user as a final answer.
func TestLoopRetriesTruncatedTextThenSucceeds(t *testing.T) {
	truncated := []llm.Chunk{{Content: "truncated mid-sentence..."}, {Done: true, StopReason: "max_tokens"}}
	ok := []llm.Chunk{{Content: "done"}, {Done: true, StopReason: "end_turn"}}
	client := llmfake.New([][]llm.Chunk{truncated, ok})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	var sawRetry bool
	var finished string
	result, err := loop.RunWithEvents(context.Background(), "hi", func(e Event) {
		if e.Type == EventContextUpdated && strings.Contains(e.Message, "incomplete model response") {
			sawRetry = true
		}
		if e.Type == EventAgentFinished {
			finished = e.Message
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawRetry {
		t.Fatal("expected an incomplete-response retry nudge event")
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("expected final answer %q, got %q", "done", result.FinalAnswer)
	}
	if strings.Contains(finished, "truncated mid-sentence") {
		t.Fatalf("truncated partial text must not leak into the final answer: %q", finished)
	}
}

// TestLoopEmptyResponseAfterRetriesErrorsNotSilent: when retries are exhausted
// the loop emits EventAgentError with a fix hint, never a silent
// EventAgentFinished with a half/empty message.
func TestLoopEmptyResponseAfterRetriesErrorsNotSilent(t *testing.T) {
	empty := []llm.Chunk{{Done: true, StopReason: "max_tokens"}}
	client := llmfake.New([][]llm.Chunk{empty, empty, empty})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	var sawErrEvent, sawFinished bool
	var errMsg string
	_, err := loop.RunWithEvents(context.Background(), "hi", func(e Event) {
		switch e.Type {
		case EventAgentError:
			sawErrEvent = true
			errMsg = e.Message
		case EventAgentFinished:
			sawFinished = true
		}
	})
	if err == nil {
		t.Fatal("expected error after exhausting incomplete-response retries")
	}
	if !sawErrEvent {
		t.Fatal("expected EventAgentError, not a silent finish")
	}
	if sawFinished {
		t.Fatal("must not emit EventAgentFinished on an incomplete response")
	}
	if !strings.Contains(errMsg, "budget_tokens") {
		t.Fatalf("error should hint at thinking.budget_tokens, got %q", errMsg)
	}
}

// TestLoopDoesNotRetryInvisibleReasoningOnlyTruncation documents the protocol
// boundary: BondCode cannot replay provider-signed thinking blocks, so an empty
// assistant message plus a "continue" nudge cannot actually resume the hidden
// trajectory. It must fail explicitly instead of restarting reasoning loops.
func TestLoopDoesNotRetryInvisibleReasoningOnlyTruncation(t *testing.T) {
	truncatedReasoning := []llm.Chunk{
		{Reasoning: "I am still planning the implementation."},
		{Done: true, StopReason: "max_tokens"},
	}
	wouldOnlySucceedAfterUnsafeRetry := []llm.Chunk{{Content: "unexpected retry", Done: true, StopReason: "end_turn"}}
	client := llmfake.New([][]llm.Chunk{truncatedReasoning, wouldOnlySucceedAfterUnsafeRetry})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	result, err := loop.RunWithEvents(context.Background(), "continue", nil)
	if err == nil {
		t.Fatalf("expected reasoning-only truncation to fail explicitly, got %q", result.FinalAnswer)
	}
	if !strings.Contains(err.Error(), "reasoning") || !strings.Contains(err.Error(), "safely") {
		t.Fatalf("expected an actionable reasoning replay error, got %v", err)
	}
}

func TestLoopRetriesToolUseStopWithoutToolCall(t *testing.T) {
	malformed := []llm.Chunk{
		{Content: "现在启动子 Agent。"},
		{Done: true, StopReason: "tool_use"},
	}
	ok := []llm.Chunk{{Content: "done"}, {Done: true, StopReason: "end_turn"}}
	client := llmfake.New([][]llm.Chunk{malformed, ok})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	var sawRetry bool
	result, err := loop.RunWithEvents(context.Background(), "启动一个子 Agent", func(e Event) {
		if e.Type == EventContextUpdated && strings.Contains(e.Message, "incomplete model response") {
			sawRetry = true
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawRetry {
		t.Fatal("tool_use stop without a tool call must be retried")
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("final answer = %q, want done", result.FinalAnswer)
	}
}
