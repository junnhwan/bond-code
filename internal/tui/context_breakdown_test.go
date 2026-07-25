package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
)

func TestBreakdownBarFillsExactWidth(t *testing.T) {
	view := ContextBreakdownView{System: 25, Conversation: 25, ToolResult: 25, Summary: 25}
	const width = 20
	bar := renderContextBreakdownBar(view, width)
	// Measure with lipgloss (terminal display width), not go-runewidth: U+2588
	// is Ambiguous and runewidth may count it as 2 on Windows.
	if w := lipgloss.Width(ansi.Strip(bar)); w != width {
		t.Fatalf("expected bar to fill exactly %d cells, got %d (%q)", width, w, bar)
	}
}

func TestBreakdownBarEmptyWhenNoTokens(t *testing.T) {
	if bar := renderContextBreakdownBar(ContextBreakdownView{}, 20); bar != "" {
		t.Fatalf("expected empty bar for zero breakdown, got %q", bar)
	}
}

func TestBreakdownBarFillsWidthWithPartialSegments(t *testing.T) {
	view := ContextBreakdownView{System: 10, ToolResult: 30}
	bar := renderContextBreakdownBar(view, 20)
	if w := lipgloss.Width(ansi.Strip(bar)); w != 20 {
		t.Fatalf("expected bar to fill width with 2 segments, got %d (%q)", w, bar)
	}
}

func TestBreakdownLegendListsAllSegments(t *testing.T) {
	view := ContextBreakdownView{System: 5200, Conversation: 3100, ToolResult: 8000, Summary: 1000}
	legend := renderContextBreakdownLegend(view, 80)
	for _, want := range []string{"sys", "5.2k", "conv", "3.1k", "tool", "8.0k", "sum", "1.0k"} {
		if !strings.Contains(legend, want) {
			t.Fatalf("legend missing %q: %q", want, legend)
		}
	}
}

func TestBreakdownLegendOmitsZeroSegments(t *testing.T) {
	view := ContextBreakdownView{System: 1000, ToolResult: 2000}
	legend := renderContextBreakdownLegend(view, 80)
	if strings.Contains(legend, "conv") || strings.Contains(legend, "sum") {
		t.Fatalf("legend should omit zero segments: %q", legend)
	}
}

func TestBreakdownLinesForPanel(t *testing.T) {
	view := ContextBreakdownView{System: 1000, Conversation: 500, ToolResult: 2000, Summary: 200}
	lines := renderContextBreakdownLines(view, 80)
	if len(lines) < 2 {
		t.Fatalf("expected bar + legend, got %#v", lines)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"sys", "conv", "tool", "sum"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected legend segment %q in lines:\n%s", want, joined)
		}
	}
}

func TestRefreshStatusMergesLiveState(t *testing.T) {
	model := NewModel(Config{
		Status: Status{SessionID: "session-1", Tasks: []TaskView{{ID: "old", Subject: "old"}}},
		RefreshStatus: func() Status {
			return Status{
				ContextSummary:   "summary updated",
				ContextBreakdown: ContextBreakdownView{System: 100, Conversation: 200},
				Tasks:            []TaskView{{ID: "new", Subject: "new plan", Status: "in_progress"}},
			}
		},
	})
	model = model.refreshStatus()
	if model.live.Breakdown.System != 100 || model.live.Breakdown.Conversation != 200 {
		t.Fatalf("expected breakdown merged, got %#v", model.live.Breakdown)
	}
	if model.live.ContextSummary != "summary updated" {
		t.Fatalf("expected summary merged, got %q", model.live.ContextSummary)
	}
	if model.live.SessionID != "session-1" {
		t.Fatalf("refreshStatus should not touch non-context fields, got %q", model.live.SessionID)
	}
	if len(model.live.Tasks) != 1 || model.live.Tasks[0].ID != "new" {
		t.Fatalf("expected tasks refreshed, got %#v", model.live.Tasks)
	}
}

func TestRefreshStatusNilIsNoOp(t *testing.T) {
	model := NewModel(Config{})
	got := model.refreshStatus()
	if !reflect.DeepEqual(model, got) {
		t.Fatal("expected refreshStatus to be a no-op when RefreshStatus is nil")
	}
}

func TestCompactionFinishedTriggersRefreshStatus(t *testing.T) {
	calls := 0
	model := NewModel(Config{
		RefreshStatus: func() Status {
			calls++
			return Status{ContextBreakdown: ContextBreakdownView{System: calls * 100, ToolResult: 50}}
		},
	})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventCompactionFinished, ContextTokens: 100, ContextMaxTokens: 100000})
	if calls != 1 {
		t.Fatalf("expected RefreshStatus called once after compaction, got %d", calls)
	}
	if model.live.Breakdown.System != 100 {
		t.Fatalf("expected breakdown refreshed after compaction, got %#v", model.live.Breakdown)
	}
}

func TestLiveContextEventsDoNotSynchronouslyRefreshStatus(t *testing.T) {
	calls := 0
	model := NewModel(Config{RefreshStatus: func() Status {
		calls++
		return Status{ContextBreakdown: ContextBreakdownView{Conversation: 321}}
	}})
	model = model.ApplyAgentEvent(agent.Event{
		Type:             agent.EventContextUpdated,
		ContextTokens:    500,
		ContextMaxTokens: 1000,
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type:                agent.EventContextMeasured,
		MeasuredInputTokens: 450,
	})
	if calls != 0 {
		t.Fatalf("live context events must not run the potentially blocking full status refresh, got %d calls", calls)
	}
	if model.agent.ContextTokens != 500 || model.agent.ContextMaxTokens != 1000 || model.agent.MeasuredTokens != 450 {
		t.Fatalf("live token counters were not cached directly: %#v", model.agent)
	}
}

func TestSuccessfulTodoWriteRefreshesBeforeAgentFinishes(t *testing.T) {
	calls := 0
	model := NewModel(Config{RefreshStatus: func() Status {
		calls++
		return Status{Tasks: []TaskView{{ID: "1", Subject: "fix tui", Status: "in_progress", ActiveForm: "Fixing tui"}}}
	}})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventToolResult, ToolName: "todo_write", Output: "ok"})
	if calls != 1 || len(model.live.Tasks) != 1 || model.live.Tasks[0].Subject != "fix tui" {
		t.Fatalf("expected todo refresh before agent finish, calls=%d tasks=%#v", calls, model.live.Tasks)
	}
	if chip := todoChip(model.live.Tasks); !strings.Contains(chip, "todo") || !strings.Contains(chip, "Fixing tui") {
		t.Fatalf("expected todo chip with active form, got %q", chip)
	}
}

func TestFailedTodoWriteDoesNotRefresh(t *testing.T) {
	calls := 0
	model := NewModel(Config{RefreshStatus: func() Status { calls++; return Status{} }})
	_ = model.ApplyAgentEvent(agent.Event{Type: agent.EventToolResult, ToolName: "todo_write", Error: "failed"})
	if calls != 0 {
		t.Fatalf("failed mutation must not trigger refresh, got %d calls", calls)
	}
}
