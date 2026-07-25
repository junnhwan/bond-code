package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
)

func TestTimelineOnlyStandaloneBlocksRenderWithoutEmptyUserEcho(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.AppendBlock(BlockCommand, "/status", "ready")
	model.timeline = model.timeline.AppendBlock(BlockError, "agent error", "boom")

	plain := ansi.Strip(strings.Join(model.workspaceTimelineLines(80), "\n"))
	if !strings.Contains(plain, "ready") || !strings.Contains(plain, "boom") {
		t.Fatalf("standalone timeline blocks were not rendered: %q", plain)
	}
	if strings.Contains(plain, "❯") {
		t.Fatalf("synthetic empty turn rendered a user prompt marker: %q", plain)
	}
}

func TestLatestCopyableOutputReadsTimelineOnly(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.AppendBlock(BlockAssistant, "agent", "timeline answer")

	got, ok := model.latestCopyableOutput()
	if !ok || got != "timeline answer" {
		t.Fatalf("latest copyable output = %q, %v; want timeline answer, true", got, ok)
	}
}

func TestLatestToolBlockReadsTimelineOnly(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.UpsertToolBlock(&ToolBlock{
		ID:     "call-1",
		Name:   "run_command",
		Status: ToolRunning,
		Output: "working",
	})

	got := model.latestToolBlock()
	if got == nil || got.ID != "call-1" || got.Status != ToolRunning {
		t.Fatalf("latest timeline tool = %#v", got)
	}
}

func TestLiveDeltasDoNotMutateCommittedTimeline(t *testing.T) {
	tests := []struct {
		name  string
		event agent.Event
	}{
		{name: "assistant", event: agent.Event{Type: agent.EventModelChunk, Message: "answer"}},
		{name: "reasoning", event: agent.Event{Type: agent.EventReasoningChunk, Message: "thinking"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(Config{})
			model.timeline = model.timeline.StartUserTurn("prompt")
			model.timeline = model.timeline.AppendBlock(BlockCommand, "before", "committed")
			before := model.timeline

			model = model.ApplyAgentEvent(tt.event)

			if !reflect.DeepEqual(model.timeline, before) {
				t.Fatalf("live delta mutated committed timeline:\nbefore=%#v\nafter=%#v", before, model.timeline)
			}
		})
	}
}

func latestTimelineBlock(timeline TimelineState, kind BlockKind) (Block, bool) {
	for turnIdx := len(timeline.Turns) - 1; turnIdx >= 0; turnIdx-- {
		blocks := timeline.Turns[turnIdx].Blocks
		for blockIdx := len(blocks) - 1; blockIdx >= 0; blockIdx-- {
			if blocks[blockIdx].Kind == kind {
				return blocks[blockIdx], true
			}
		}
	}
	return Block{}, false
}
