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

func TestAgentBarHiddenWithoutChildAgents(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 24)
	if got := model.agentBarView(); got != "" {
		t.Fatalf("no-child Agent strip should be empty, got %q", got)
	}
	// forWidth still paints a minimal coordinator row for focused tests.
	view := ansi.Strip(model.agentBarViewForWidth(80))
	if !strings.Contains(view, "Agent Main") && !strings.Contains(view, "Main") {
		t.Fatalf("forWidth coordinator fallback missing Main: %q", view)
	}
}

func TestAgentPassiveStripShowsWhenChildrenExist(t *testing.T) {
	model := NewModel(Config{}).SetSize(100, 24)
	model.subagentTraces["task-active"] = &AgentTrace{
		TaskID:    "task-active",
		AgentType: "reviewer",
		Status:    "running",
		Blocks:    []Block{{Tool: &ToolBlock{Name: "list_dir", Status: ToolRunning}}},
		Unread:    true,
	}
	model.focus = FocusComposer

	view := ansi.Strip(model.agentBarView())
	for _, want := range []string{"Agents", "reviewer", "unread", "Ctrl+↑", "click"} {
		if !strings.Contains(view, want) {
			t.Fatalf("passive strip missing %q: %q", want, view)
		}
	}
	if strings.Contains(view, "\n") {
		t.Fatalf("passive strip must stay one row: %q", view)
	}
}

func TestAgentBarPillsShowMultipleAgents(t *testing.T) {
	model := NewModel(Config{}).SetSize(100, 24)
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-active"
	model.subagentTraces["task-old"] = &AgentTrace{TaskID: "task-old", AgentType: "coder", Status: "completed", Unread: true}
	model.subagentTraces["task-active"] = &AgentTrace{
		TaskID:    "task-active",
		AgentType: "reviewer",
		Status:    "completed",
	}
	model.subagentTraces["task-new"] = &AgentTrace{TaskID: "task-new", AgentType: "research", Status: "running", Unread: true}

	view := ansi.Strip(model.agentBarViewForWidth(100))
	for _, want := range []string{"Main", "reviewer", "coder", "research"} {
		if !strings.Contains(view, want) {
			t.Fatalf("pills row missing %q: %q", want, view)
		}
	}
}

func TestAgentBarFitsWidthAndDegrades(t *testing.T) {
	model := NewModel(Config{}).SetSize(100, 24)
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-active"
	model.subagentTraces["task-active"] = &AgentTrace{
		TaskID:    "task-active",
		AgentType: "qa",
		Status:    "running",
	}
	model.subagentTraces["task-unread"] = &AgentTrace{TaskID: "task-unread", AgentType: "coder", Unread: true}

	for _, width := range []int{100, 28, 12, 8, 4, 1} {
		view := model.agentBarViewForWidth(width)
		// Window focus: pills only (single visual row).
		if strings.Contains(view, "\n") || renderedHeight(view) != 1 {
			t.Fatalf("Agent pills at width %d must be exactly one row: %q", width, ansi.Strip(view))
		}
		if got := lipgloss.Width(view); got > width {
			t.Fatalf("Agent row width = %d, want <= %d: %q", got, width, ansi.Strip(view))
		}
	}

	narrow := ansi.Strip(model.agentBarViewForWidth(12))
	if !strings.Contains(narrow, "qa") && !strings.Contains(narrow, "⬡") {
		t.Fatalf("narrow Agent row should retain some active label: %q", narrow)
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

func TestAgentSwitcherListShowsRosterAndActivity(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 24)
	model.focus = FocusAgentBar
	model.agentBarSelected = "task-1"
	model.subagentTraces["task-1"] = &AgentTrace{
		TaskID:    "task-1",
		AgentType: "coder",
		Status:    "running",
		Blocks:    []Block{{Tool: &ToolBlock{Name: "write_file", Status: ToolRunning}}},
	}
	model.subagentTraces["task-2"] = &AgentTrace{
		TaskID:    "task-2",
		AgentType: "reviewer",
		Status:    "completed",
		// empty completion
	}

	view := ansi.Strip(model.agentBarViewForWidth(80))
	if renderedHeight(view) < 3 {
		t.Fatalf("switcher should be multi-line (pills+list+hints), got %d: %q", renderedHeight(view), view)
	}
	for _, want := range []string{"Main", "coder", "reviewer", "write_file", "no tools", "enter open"} {
		if !strings.Contains(view, want) {
			t.Fatalf("switcher list missing %q: %q", want, view)
		}
	}
	if !strings.Contains(view, "▸") {
		t.Fatalf("selected row should show cursor: %q", view)
	}
}

func TestAgentActivityTextEmptyCompletion(t *testing.T) {
	model := NewModel(Config{})
	model.subagentTraces["t1"] = &AgentTrace{TaskID: "t1", Status: "completed"}
	got := model.agentActivityText("t1", "completed", "")
	if !strings.Contains(got, "empty") && !strings.Contains(got, "no tools") {
		t.Fatalf("empty completion activity = %q", got)
	}
}

func TestAgentPassiveStripShowsOutcomeCounts(t *testing.T) {
	model := NewModel(Config{}).SetSize(100, 24)
	model.subagentTraces["a"] = &AgentTrace{TaskID: "a", AgentType: "coder", Status: "running"}
	model.subagentTraces["b"] = &AgentTrace{TaskID: "b", AgentType: "coder", Status: "failed"}
	model.subagentTraces["c"] = &AgentTrace{TaskID: "c", AgentType: "reviewer", Status: "completed"} // empty
	model.traceMembershipVersion++

	view := ansi.Strip(model.agentBarViewForWidth(100))
	for _, want := range []string{"running", "failed", "empty"} {
		if !strings.Contains(view, want) {
			t.Fatalf("passive strip missing %q: %q", want, view)
		}
	}
}
