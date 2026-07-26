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

// TestTextDegenerationBreakerRecoversFromRepeatedChunks mimics the real failure
// mode in session-20260701-152834: the model streamed 306 identical "ognition"
// chunks in a single response, running for ~5 minutes with no guard firing.
// The breaker must cancel the stream early and let the model recover on a
// no-tools follow-up turn instead of returning the repeated garbage. Here 50
// identical chunks feed the loop; recovery is a clean second response.
func TestTextDegenerationBreakerRecoversFromRepeatedChunks(t *testing.T) {
	degenerated := make([]llm.Chunk, 50)
	for i := range degenerated {
		degenerated[i] = llm.Chunk{Content: "ognition"}
	}
	recovery := []llm.Chunk{
		{Content: "I was implementing the safety module."},
		{Done: true, StopReason: "end_turn"},
	}
	client := llmfake.New([][]llm.Chunk{degenerated, recovery})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	var sawBreaker, sawFinish bool
	var finished string
	result, err := loop.RunWithEvents(context.Background(), "build mini bondcode", func(e Event) {
		switch e.Type {
		case EventTextDegeneration:
			sawBreaker = true
		case EventAgentFinished:
			sawFinish = true
			finished = e.Message
		}
	})
	if err != nil {
		t.Fatalf("breaker tripping must not surface as a run error: %v", err)
	}
	if !sawBreaker {
		t.Fatal("expected EventTextDegeneration for the repeated chunks")
	}
	if !sawFinish {
		t.Fatal("expected EventAgentFinished after recovery")
	}
	const want = "I was implementing the safety module."
	if result.FinalAnswer != want {
		t.Fatalf("expected recovery answer %q, got %q (finished=%q)", want, result.FinalAnswer, finished)
	}
	if strings.Contains(result.FinalAnswer, "ognitionognition") {
		t.Fatalf("repeated garbage leaked into final answer: %q", result.FinalAnswer)
	}
}

// TestTextDegenerationBreakerFallbackWhenRecoveryEmpty: if the recovery stream
// yields nothing, the loop falls back to a truncated prefix + notice rather
// than surfacing an error or an empty/agent_finished.
func TestTextDegenerationBreakerFallbackWhenRecoveryEmpty(t *testing.T) {
	degenerated := make([]llm.Chunk, 30)
	for i := range degenerated {
		degenerated[i] = llm.Chunk{Content: "abc"}
	}
	empty := []llm.Chunk{{Done: true, StopReason: "end_turn"}} // recovery yields no text
	client := llmfake.New([][]llm.Chunk{degenerated, empty})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	var sawBreaker bool
	result, err := loop.RunWithEvents(context.Background(), "hi", func(e Event) {
		if e.Type == EventTextDegeneration {
			sawBreaker = true
		}
	})
	if err != nil {
		t.Fatalf("fallback must not surface as error: %v", err)
	}
	if !sawBreaker {
		t.Fatal("expected EventTextDegeneration")
	}
	if !strings.Contains(result.FinalAnswer, "circuit breaker tripped") {
		t.Fatalf("expected fallback notice in final answer, got %q", result.FinalAnswer)
	}
}

// TestTextDegenerationBreakerDoesNotTripOnNormalOutput: a long, varied response
// must stream to completion without the breaker firing.
func TestTextDegenerationBreakerDoesNotTripOnNormalOutput(t *testing.T) {
	normal := []llm.Chunk{
		{Content: "Let me "}, {Content: "read "}, {Content: "the "}, {Content: "file "},
		{Content: "and "}, {Content: "explain "}, {Content: "its "}, {Content: "structure."},
		{Done: true, StopReason: "end_turn"},
	}
	client := llm.NewFakeClient(normal)
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	var sawBreaker bool
	result, err := loop.RunWithEvents(context.Background(), "explain", func(e Event) {
		if e.Type == EventTextDegeneration {
			sawBreaker = true
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sawBreaker {
		t.Fatal("breaker must not trip on varied normal output")
	}
	if !strings.Contains(result.FinalAnswer, "structure.") {
		t.Fatalf("expected normal answer, got %q", result.FinalAnswer)
	}
}

// TestReasoningDegenerationBreakerRecoversFromRepeatedChunks covers the plane
// that is invisible to answer text. Repeated reasoning must cancel the stream
// and enter the bounded no-tools recovery path instead of consuming the whole
// output budget.
func TestReasoningDegenerationBreakerRecoversFromRepeatedChunks(t *testing.T) {
	// Reasoning Gate A needs a longer identical-chunk run than answer text
	// (see reasoningTextGuardConfig) so healthy multi-paragraph thinking is not
	// cancelled mid-stream.
	degenerated := make([]llm.Chunk, 80)
	for i := range degenerated {
		degenerated[i] = llm.Chunk{Reasoning: "I will now write App.jsx. "}
	}
	recovery := []llm.Chunk{{Content: "I stopped the repeated reasoning and summarized the result.", Done: true, StopReason: "end_turn"}}
	client := llmfake.New([][]llm.Chunk{degenerated, recovery})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	var sawBreaker bool
	result, err := loop.RunWithEvents(context.Background(), "continue", func(event Event) {
		if event.Type == EventTextDegeneration {
			sawBreaker = true
		}
	})
	if err != nil {
		t.Fatalf("expected bounded reasoning recovery, got %v", err)
	}
	if !sawBreaker {
		t.Fatal("expected repeated reasoning to trip the degeneration breaker")
	}
	if result.FinalAnswer != "I stopped the repeated reasoning and summarized the result." {
		t.Fatalf("unexpected recovery answer %q", result.FinalAnswer)
	}
}
