package tui

import (
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
)

func TestCompactionFinishedAppendsDividerWithBeforeAfter(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventContextUpdated, ContextTokens: 5200, ContextMaxTokens: 100000})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventCompactionFinished, ContextTokens: 1000, ContextMaxTokens: 100000})

	div := findCompactionBlock(t, model.timeline)
	if !strings.Contains(div.Body, "5.2k") || !strings.Contains(div.Body, "1.0k") || !strings.Contains(div.Body, "→") {
		t.Fatalf("expected before→after body (5.2k → 1.0k tokens), got %q", div.Body)
	}
	if model.agent.ContextTokens != 1000 {
		t.Fatalf("expected after-tokens cached on the agent state, got %d", model.agent.ContextTokens)
	}
}

func TestCompactionFinishedWithoutPriorTokensShowsAfterOnly(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventCompactionFinished, ContextTokens: 1000, ContextMaxTokens: 100000})

	div := findCompactionBlock(t, model.timeline)
	if strings.Contains(div.Body, "→") {
		t.Fatalf("expected after-only body when no prior measurement, got %q", div.Body)
	}
	if !strings.Contains(div.Body, "1.0k") {
		t.Fatalf("expected after token count in body, got %q", div.Body)
	}
}

func TestCompactionFinishedFailureOmitsDivider(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventCompactionFinished, Error: "compact failed", ContextMaxTokens: 100000})

	if hasCompactionBlock(model.timeline) {
		t.Fatalf("expected no compaction divider on failure, got %#v", model.timeline.Turns)
	}
}

func TestCompactionDividerRendersInView(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventContextUpdated, ContextTokens: 5200, ContextMaxTokens: 100000})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventCompactionFinished, ContextTokens: 1000, ContextMaxTokens: 100000})

	view := model.View()
	for _, want := range []string{"compacted", "5.2k", "1.0k"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q:\n%s", want, view)
		}
	}
}

func TestTimelineAppendCompactionBlockIsImmutable(t *testing.T) {
	state := TimelineState{}
	state = state.StartUserTurn("hello")
	state = state.AppendBlock(BlockCompaction, "compact", "5.0k → 1.0k tokens")

	next := state.AppendBlock(BlockCompaction, "compact", "5.0k → 0.5k tokens")

	prev := state.Turns[0].Blocks
	if len(prev) != 1 || prev[0].Kind != BlockCompaction || prev[0].Body != "5.0k → 1.0k tokens" {
		t.Fatalf("expected previous state unchanged (COW), got %#v", prev)
	}
	nb := next.Turns[0].Blocks
	if len(nb) != 2 || nb[1].Body != "5.0k → 0.5k tokens" {
		t.Fatalf("expected next state to append a new divider, got %#v", nb)
	}
}

func findCompactionBlock(t *testing.T, state TimelineState) Block {
	t.Helper()
	for i := len(state.Turns) - 1; i >= 0; i-- {
		for _, b := range state.Turns[i].Blocks {
			if b.Kind == BlockCompaction {
				return b
			}
		}
	}
	t.Fatalf("expected a compaction divider block, got %#v", state.Turns)
	return Block{}
}

func hasCompactionBlock(state TimelineState) bool {
	for _, turn := range state.Turns {
		for _, b := range turn.Blocks {
			if b.Kind == BlockCompaction {
				return true
			}
		}
	}
	return false
}
