package tui

import (
	"fmt"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
)

var availableAgentIDsSink []string

func TestAvailableAgentIDsReusesIndexForUnchangedMembership(t *testing.T) {
	const timelineAgents = 128
	model := newAvailableAgentIDsFixture(timelineAgents, 32)

	availableAgentIDsSink = model.availableAgentIDs()
	if got, want := len(availableAgentIDsSink), timelineAgents+32; got != want {
		t.Fatalf("available Agent count = %d, want %d", got, want)
	}

	allocs := testing.AllocsPerRun(100, func() {
		availableAgentIDsSink = model.availableAgentIDs()
	})
	if allocs != 0 {
		t.Fatalf("unchanged Agent index allocated %.2f times per lookup, want 0", allocs)
	}
}

func TestAvailableAgentIDsInvalidatesOnTimelineAndTraceMembershipChanges(t *testing.T) {
	t.Run("timeline version", func(t *testing.T) {
		model := NewModel(Config{})
		if got := model.availableAgentIDs(); len(got) != 0 {
			t.Fatalf("initial Agent IDs = %v, want none", got)
		}

		model.timeline = model.timeline.StartUserTurn("delegate")
		model.timeline = model.timeline.UpsertSubagentBlock("timeline-agent", "reviewer", "running", "work")
		assertAvailableAgentIDs(t, model, "timeline-agent")
	})

	t.Run("trace addition", func(t *testing.T) {
		model := NewModel(Config{})
		if got := model.availableAgentIDs(); len(got) != 0 {
			t.Fatalf("initial Agent IDs = %v, want none", got)
		}

		model = model.ApplyAgentEvent(agent.Event{
			Type:       agent.EventSubagentToolCall,
			ToolCallID: "trace-agent",
			ToolName:   "read_file",
		})
		assertAvailableAgentIDs(t, model, "trace-agent")
	})

	t.Run("session switch", func(t *testing.T) {
		model := NewModel(Config{
			Status: Status{SessionID: "session-old"},
			ReloadSessionSeed: func(string) []SeedMessage {
				return nil
			},
		})
		model = model.ApplyAgentEvent(agent.Event{
			Type:       agent.EventSubagentToolCall,
			ToolCallID: "old-session-agent",
			ToolName:   "read_file",
		})
		assertAvailableAgentIDs(t, model, "old-session-agent")

		model = model.reloadSessionView("session-new")
		if got := model.availableAgentIDs(); len(got) != 0 {
			t.Fatalf("Agent IDs after session switch = %v, want none", got)
		}
	})
}

func assertAvailableAgentIDs(t *testing.T, model Model, want ...string) {
	t.Helper()
	got := model.availableAgentIDs()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("available Agent IDs = %v, want %v", got, want)
	}
}

func BenchmarkAvailableAgentIDsCached(b *testing.B) {
	for _, turns := range []int{300, 1000} {
		b.Run(fmt.Sprintf("%dTurns", turns), func(b *testing.B) {
			model := newAvailableAgentIDsFixture(turns, 32)
			availableAgentIDsSink = model.availableAgentIDs()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				availableAgentIDsSink = model.availableAgentIDs()
			}
		})
	}
}

func newAvailableAgentIDsFixture(timelineAgents, traceAgents int) Model {
	model := NewModel(Config{})
	for i := 0; i < timelineAgents; i++ {
		model.timeline = model.timeline.StartUserTurn(fmt.Sprintf("delegate %d", i))
		model.timeline = model.timeline.UpsertSubagentBlock(fmt.Sprintf("timeline-%03d", i), "reviewer", "completed", "done")
	}
	for i := 0; i < traceAgents; i++ {
		id := fmt.Sprintf("trace-%03d", i)
		model.subagentTraces[id] = &AgentTrace{TaskID: id}
	}
	return model
}
