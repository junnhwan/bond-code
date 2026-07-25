package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TestLatestToolNamePrefersRunning confirms the agent bar surfaces the tool a
// parallel child is doing RIGHT NOW (running) over an earlier completed one.
func TestLatestToolNamePrefersRunning(t *testing.T) {
	trace := &AgentTrace{Blocks: []Block{
		{Tool: &ToolBlock{Name: "read_file", Status: ToolDone}},
		{Tool: &ToolBlock{Name: "edit_file", Status: ToolRunning}},
		{Tool: &ToolBlock{Name: "read_file", Status: ToolDone}},
	}}
	if got := latestToolName(trace); got != "edit_file" {
		t.Fatalf("running tool should win, got %q", got)
	}
}

// TestLatestToolNameFallsBackToMostRecent confirms that once every tool has
// finished the bar shows the last one (so a finished child still reports what
// it did, not nothing).
func TestLatestToolNameFallsBackToMostRecent(t *testing.T) {
	trace := &AgentTrace{Blocks: []Block{
		{Tool: &ToolBlock{Name: "read_file", Status: ToolDone}},
		{Tool: &ToolBlock{Name: "edit_file", Status: ToolDone}},
	}}
	if got := latestToolName(trace); got != "edit_file" {
		t.Fatalf("with no running tool, most recent should win, got %q", got)
	}
}

func TestLatestToolNameEmptyTrace(t *testing.T) {
	if got := latestToolName(&AgentTrace{}); got != "" {
		t.Fatalf("empty trace should yield empty, got %q", got)
	}
	if got := latestToolName(nil); got != "" {
		t.Fatalf("nil trace should yield empty, got %q", got)
	}
}

func TestAgentBarPersistsWithoutChildAgents(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 24)
	view := ansi.Strip(model.agentBarViewForWidth(80))

	if got := renderedHeight(view); got != 1 {
		t.Fatalf("persistent Agent row height = %d, want 1: %q", got, view)
	}
	for _, want := range []string{"Agent Main", "0 unread"} {
		if !strings.Contains(view, want) {
			t.Fatalf("persistent Agent row missing %q: %q", want, view)
		}
	}
}

func TestAgentBarShowsActiveAgentTotalUnreadAndTerminalState(t *testing.T) {
	model := NewModel(Config{}).SetSize(100, 24)
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-active"
	model.subagentTraces["task-old"] = &AgentTrace{TaskID: "task-old", Unread: true}
	model.subagentTraces["task-active"] = &AgentTrace{
		TaskID:    "task-active",
		AgentType: "reviewer",
		Status:    "completed",
	}
	model.subagentTraces["task-new"] = &AgentTrace{TaskID: "task-new", Unread: true}

	view := ansi.Strip(model.agentBarViewForWidth(100))
	for _, want := range []string{"Agent reviewer", "completed", "2 unread"} {
		if !strings.Contains(view, want) {
			t.Fatalf("active Agent row missing %q: %q", want, view)
		}
	}
	if strings.Contains(view, "task-old") || strings.Contains(view, "task-new") {
		t.Fatalf("concise Agent row should show only the active identity: %q", view)
	}
}

func TestAgentBarFitsWidthAndDegradesToMinimalActiveLabel(t *testing.T) {
	model := NewModel(Config{}).SetSize(100, 24)
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-active"
	model.subagentTraces["task-active"] = &AgentTrace{
		TaskID:    "task-active",
		AgentType: "qa",
		Status:    "running",
	}
	model.subagentTraces["task-unread"] = &AgentTrace{TaskID: "task-unread", Unread: true}

	for _, width := range []int{100, 28, 12, 8, 4, 1} {
		view := model.agentBarViewForWidth(width)
		if strings.Contains(view, "\n") || renderedHeight(view) != 1 {
			t.Fatalf("Agent row at width %d must be exactly one row: %q", width, ansi.Strip(view))
		}
		if got := lipgloss.Width(view); got > width {
			t.Fatalf("Agent row width = %d, want <= %d: %q", got, width, ansi.Strip(view))
		}
	}

	narrow := ansi.Strip(model.agentBarViewForWidth(12))
	if !strings.Contains(narrow, "Agent qa") {
		t.Fatalf("narrow Agent row should retain the minimal active-Agent label: %q", narrow)
	}
	if strings.Contains(narrow, "running") || strings.Contains(narrow, "unread") {
		t.Fatalf("narrow Agent row should drop secondary status: %q", narrow)
	}
}

func TestAgentBarRenderingLeavesStreamingPlanesUntouched(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("delegate")
	stream := &liveStreamState{body: "SECRET_LIVE_BODY", visibleLen: len("SECRET_LIVE_BODY")}
	model.subagentTraces["task-active"] = &AgentTrace{
		TaskID:     "task-active",
		AgentType:  "reviewer",
		Status:     "running",
		LiveStream: stream,
	}
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-active"
	version := model.timeline.Version

	view := ansi.Strip(model.agentBarViewForWidth(80))
	if strings.Contains(view, "SECRET_LIVE_BODY") {
		t.Fatalf("Agent row leaked the live stream body: %q", view)
	}
	if model.timeline.Version != version || model.subagentTraces["task-active"].LiveStream != stream || stream.body != "SECRET_LIVE_BODY" {
		t.Fatalf("Agent row rendering mutated timeline/live state: version=%d stream=%#v", model.timeline.Version, model.subagentTraces["task-active"].LiveStream)
	}
}
