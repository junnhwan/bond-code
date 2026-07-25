package agent

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

// TestSetClientSwapsStreamingClient locks the contract the whole /model feature
// rests on: after SetClient, the next Run streams through the replacement, not
// the client the loop was built with.
func TestSetClientSwapsStreamingClient(t *testing.T) {
	fakeA := llm.NewFakeClient([]llm.Chunk{{Content: "from-a", Done: true}})
	fakeB := llm.NewFakeClient([]llm.Chunk{{Content: "from-b", Done: true}})
	registry := tool.NewRegistry()
	loop := NewLoop(LoopConfig{}, fakeA, registry, safety.Policy{}, safety.StaticConfirmer(true))
	loop.SetClient(fakeB)

	res, err := loop.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.FinalAnswer != "from-b" {
		t.Fatalf("expected the swapped client B to answer, got %q", res.FinalAnswer)
	}
	if n := len(fakeA.LastMessages()); n != 0 {
		t.Fatalf("expected original client A to be unused after the swap, got %d messages", n)
	}
}
