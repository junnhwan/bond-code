package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
)

func TestCurrentTurnAgentsCollectsSubagentIDs(t *testing.T) {
	m := Model{timeline: TimelineState{Turns: []Turn{{
		ID: "turn-1",
		Blocks: []Block{
			{ID: "sub-1", Kind: BlockSubagent},
			{ID: "turn-1-tool-1", Kind: BlockTool},
			{ID: "sub-2", Kind: BlockSubagent},
		},
	}}}}
	got := m.conversationAgents()
	if len(got) != 2 || got[0] != "sub-1" || got[1] != "sub-2" {
		t.Fatalf("expected [sub-1 sub-2], got %v", got)
	}
}

func TestCurrentTurnAgentsEmptyWhenNoTurn(t *testing.T) {
	m := Model{timeline: TimelineState{}}
	if got := m.conversationAgents(); len(got) != 0 {
		t.Fatalf("expected no agents without a turn, got %v", got)
	}
}

func TestMoveAgentInBarWrapsAndRecoversStaleSelection(t *testing.T) {
	m := Model{
		timeline: TimelineState{Turns: []Turn{{
			ID: "turn-1",
			Blocks: []Block{
				{ID: "sub-1", Kind: BlockSubagent},
				{ID: "sub-2", Kind: BlockSubagent},
				{ID: "sub-3", Kind: BlockSubagent},
			},
		}}},
		agentBarSelected: "sub-2",
	}
	if next := m.moveAgentInBar(1); next != "sub-3" {
		t.Fatalf("expected next sub-3, got %q", next)
	}
	if prev := m.moveAgentInBar(-1); prev != "sub-1" {
		t.Fatalf("expected prev sub-1, got %q", prev)
	}
	// Stale selection falls back to the latest agent.
	m.agentBarSelected = "gone"
	if got := m.moveAgentInBar(1); got != "sub-3" {
		t.Fatalf("expected stale selection to fall back to sub-3, got %q", got)
	}
}

func TestAgentTraceUpsertToolCallPairsRunningThenDone(t *testing.T) {
	trace := &AgentTrace{TaskID: "sub-1"}
	trace.upsertToolBlock(agent.Event{
		Type:       agent.EventSubagentToolCall,
		ToolCallID: "sub-1",
		ToolName:   "read_file",
		Input:      `{"path":"a.go"}`,
		Message:    "running",
	})
	if len(trace.Blocks) != 1 || trace.Blocks[0].Tool == nil || trace.Blocks[0].Tool.Status != ToolRunning {
		t.Fatalf("expected one running block, got %#v", trace.Blocks)
	}
	trace.upsertToolBlock(agent.Event{
		Type:       agent.EventSubagentToolCall,
		ToolCallID: "sub-1",
		ToolName:   "read_file",
		Output:     "file content",
		Message:    "done",
	})
	if len(trace.Blocks) != 1 {
		t.Fatalf("expected done to merge into the running block, got %#v", trace.Blocks)
	}
	tool := trace.Blocks[0].Tool
	if tool.Status != ToolDone || tool.Output != "file content" {
		t.Fatalf("expected merged done block with output, got %#v", tool)
	}
}

func TestAgentTraceUpsertToolCallFailedSetsFailedStatus(t *testing.T) {
	trace := &AgentTrace{TaskID: "sub-1"}
	trace.upsertToolBlock(agent.Event{
		Type:       agent.EventSubagentToolCall,
		ToolCallID: "sub-1",
		ToolName:   "run_command",
		Message:    "running",
	})
	trace.upsertToolBlock(agent.Event{
		Type:       agent.EventSubagentToolCall,
		ToolCallID: "sub-1",
		ToolName:   "run_command",
		Error:      "boom",
		Message:    "failed",
	})
	if len(trace.Blocks) != 1 || trace.Blocks[0].Tool.Status != ToolFailed || trace.Blocks[0].Tool.Error != "boom" {
		t.Fatalf("expected failed block, got %#v", trace.Blocks)
	}
}

func TestAgentWindowToolCallMarksNewOutputWhileScrolledUp(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 8)
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-1"
	trace := &AgentTrace{
		TaskID:    "task-1",
		AgentType: "reviewer",
		Title:     "review code",
		Status:    "running",
	}
	for i := 0; i < 20; i++ {
		trace.Blocks = append(trace.Blocks, Block{ID: "block-" + string(rune('a'+i)), Kind: BlockCommand, Title: "log", Body: "line " + string(rune('a'+i))})
	}
	model.subagentTraces["task-1"] = trace

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if !model.scrollPaused || model.newOutputBelow {
		t.Fatalf("expected manual scroll to pause follow without new output marker, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}

	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventSubagentToolCall,
		ToolCallID: "task-1",
		ToolName:   "read_file",
		Input:      `{"path":"README.md"}`,
		Message:    "running",
	})
	if !model.scrollPaused || !model.newOutputBelow {
		t.Fatalf("expected child tool call to mark new output below in agent window, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}
}

func TestLeavingAgentWindowClearsWindowScrollState(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 8)
	model.timeline = model.timeline.StartUserTurn("delegate")
	model.timeline = model.timeline.UpsertSubagentBlock("task-1", "review code", "running", "description: inspect")
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-1"
	trace := &AgentTrace{
		TaskID:    "task-1",
		AgentType: "reviewer",
		Title:     "review code",
		Status:    "running",
	}
	for i := 0; i < 20; i++ {
		trace.Blocks = append(trace.Blocks, Block{ID: "block-" + string(rune('a'+i)), Kind: BlockCommand, Title: "log", Body: "line " + string(rune('a'+i))})
	}
	model.subagentTraces["task-1"] = trace

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if !model.scrollPaused || model.scroll == 0 {
		t.Fatalf("expected agent window PageUp to pause at a positive scroll, scroll=%d paused=%v", model.scroll, model.scrollPaused)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.focus != FocusAgentBar {
		t.Fatalf("expected Esc to return to agent bar, got focus=%q", model.focus)
	}
	if model.scroll != 0 || model.scrollPaused || model.newOutputBelow {
		t.Fatalf("expected leaving agent window to clear window scroll state, scroll=%d paused=%v marker=%v", model.scroll, model.scrollPaused, model.newOutputBelow)
	}
}

func TestEscLeavesAgentBarForComposer(t *testing.T) {
	model := NewModel(Config{})
	model.focus = FocusAgentBar

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := updated.(Model)

	if next.focus != FocusComposer {
		t.Fatalf("Esc should leave the agent bar for the composer, got focus=%q", next.focus)
	}
}

func TestEscLeavesAgentWindowForAgentBar(t *testing.T) {
	model := NewModel(Config{})
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-1"

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := updated.(Model)

	if next.focus != FocusAgentBar {
		t.Fatalf("Esc should leave the agent window for the agent bar, got focus=%q", next.focus)
	}
}

func TestAgentFocusConsumesOrdinaryTextInput(t *testing.T) {
	for _, focus := range []Focus{FocusAgentBar, FocusAgentWindow} {
		model := NewModel(Config{})
		model.focus = focus
		model = model.SetInput("draft")

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
		next := updated.(Model)

		if got := next.inputValue(); got != "draft" {
			t.Fatalf("%s focus should not type ordinary runes into composer, got %q", focus, got)
		}
		if next.focus != focus {
			t.Fatalf("%s focus should remain active after ordinary rune, got %q", focus, next.focus)
		}
	}
}

func TestAgentFocusConsumesComposerEditingKeys(t *testing.T) {
	for _, focus := range []Focus{FocusAgentBar, FocusAgentWindow} {
		model := NewModel(Config{})
		model.focus = focus
		model = model.SetInput("draft")

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		next := updated.(Model)

		if got := next.inputValue(); got != "draft" {
			t.Fatalf("%s focus should not let Backspace edit composer, got %q", focus, got)
		}
		if next.focus != focus {
			t.Fatalf("%s focus should remain active after Backspace, got %q", focus, next.focus)
		}
	}
}

func TestAgentFocusConsumesComposerControlEditingKeys(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlH, tea.KeyCtrlD} {
		model := NewModel(Config{})
		model.focus = FocusAgentWindow
		model.focusedTaskID = "task-1"
		model = model.SetInput("draft")

		updated, cmd := model.Update(tea.KeyMsg{Type: key})
		next := updated.(Model)

		if cmd != nil {
			t.Fatalf("%s in agent focus should not quit or submit, got cmd %T", key, cmd)
		}
		if got := next.inputValue(); got != "draft" {
			t.Fatalf("%s in agent focus should preserve composer draft, got %q", key, got)
		}
		if next.focus != FocusAgentWindow {
			t.Fatalf("%s in agent focus should keep focus in window, got %q", key, next.focus)
		}
	}
}

func TestAgentWindowEnterDoesNotSubmitComposerDraft(t *testing.T) {
	model := NewModel(Config{})
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-1"
	model = model.SetInput("draft")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := updated.(Model)

	if cmd != nil {
		t.Fatalf("agent window Enter should not submit composer draft, got cmd %T", cmd)
	}
	if got := next.inputValue(); got != "draft" {
		t.Fatalf("agent window Enter should preserve composer draft, got %q", got)
	}
	if next.focus != FocusAgentWindow {
		t.Fatalf("agent window Enter should keep focus in window, got %q", next.focus)
	}
	if len(next.timeline.Turns) != 0 {
		t.Fatalf("agent window Enter should not start a composer turn, turns=%#v", next.timeline.Turns)
	}
}

func TestAgentBarUpDownSelectsAgents(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("delegate")
	model.timeline = model.timeline.UpsertSubagentBlock("task-1", "first", "running", "")
	model.timeline = model.timeline.UpsertSubagentBlock("task-2", "second", "running", "")
	model.focus = FocusAgentBar
	model.agentBarSelected = "task-1"

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	next := updated.(Model)
	if next.agentBarSelected != "task-2" {
		t.Fatalf("Down should select next agent, got %q", next.agentBarSelected)
	}
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := updated.(Model).agentBarSelected; got != "task-1" {
		t.Fatalf("Up should select previous agent, got %q", got)
	}
}

func TestAgentWindowHasIndependentDraftAndRoutesInput(t *testing.T) {
	var taskID, input string
	model := NewModel(Config{SendSubagentInput: func(id, value string) error {
		taskID, input = id, value
		return nil
	}})
	model.timeline = model.timeline.StartUserTurn("delegate")
	model.timeline = model.timeline.UpsertSubagentBlock("task-1", "review", "running", "")
	model.subagentTraces["task-1"] = &AgentTrace{TaskID: "task-1", Title: "review", Status: "running"}
	model = model.SetInput("coordinator draft")
	model.focus = FocusAgentBar
	model.agentBarSelected = "task-1"

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.focus != FocusAgentWindow || model.inputValue() != "" {
		t.Fatalf("enter should open clean agent composer, focus=%q input=%q", model.focus, model.inputValue())
	}
	for _, r := range "please inspect tests" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(Model)
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("agent submit should return a delivery command")
	}
	_ = cmd()
	if taskID != "task-1" || input != "please inspect tests" {
		t.Fatalf("wrong routed input: task=%q input=%q", taskID, input)
	}
	if model.inputValue() != "" {
		t.Fatalf("successful submit should clear agent draft, got %q", model.inputValue())
	}
	trace := model.subagentTraces["task-1"]
	if len(trace.Blocks) == 0 || trace.Blocks[len(trace.Blocks)-1].Kind != BlockCommand {
		t.Fatalf("submitted input should appear in agent transcript, got %#v", trace.Blocks)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.inputValue() != "coordinator draft" {
		t.Fatalf("exit should restore coordinator draft, got %q", model.inputValue())
	}
}

func TestAgentInputFailureMarksMessageAndRestoresDraft(t *testing.T) {
	model := NewModel(Config{SendSubagentInput: func(string, string) error { return errors.New("offline") }})
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-1"
	model.subagentTraces["task-1"] = &AgentTrace{TaskID: "task-1", Status: "running"}
	model = model.SetInput("retry me")
	updated, cmd, handled := model.submitAgentInput()
	if !handled || cmd == nil {
		t.Fatal("expected submitted child input")
	}
	next, _ := updated.Update(cmd())
	updated = next.(Model)
	trace := updated.subagentTraces["task-1"]
	if len(trace.Blocks) != 1 || !strings.Contains(trace.Blocks[0].Title, "failed") {
		t.Fatalf("failed input block = %#v", trace.Blocks)
	}
	if got := updated.inputValue(); got != "retry me" {
		t.Fatalf("restored draft = %q", got)
	}
}

func TestSubagentProgressAppearsOnlyInItsTranscript(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("delegate")
	before := model.timeline.Version
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventSubagentProgress, ToolCallID: "task-1", Message: "reading package graph"})
	trace := model.subagentTraces["task-1"]
	if trace == nil || len(trace.Blocks) != 1 || trace.Blocks[0].Body != "reading package graph" {
		t.Fatalf("progress should be retained in child transcript, got %#v", trace)
	}
	if model.timeline.Version == before {
		// The compact parent status row may update, but child transcript content must not be appended as a parent assistant block.
		return
	}
	turn := model.timeline.Turns[len(model.timeline.Turns)-1]
	for _, block := range turn.Blocks {
		if block.Kind == BlockAssistant && block.Body == "reading package graph" {
			t.Fatal("child progress leaked into coordinator transcript")
		}
	}
}

func TestAgentListKeepsCompletedAgentsFromEarlierTurns(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("first")
	model.timeline = model.timeline.UpsertSubagentBlock("task-old", "old", "completed", "done")
	model.timeline = model.timeline.StartUserTurn("second")
	model.timeline = model.timeline.UpsertSubagentBlock("task-new", "new", "running", "work")
	got := model.conversationAgents()
	if len(got) != 2 || got[0] != "task-old" || got[1] != "task-new" {
		t.Fatalf("completed agents should remain inspectable, got %v", got)
	}
}

func TestTraceOnlyUnreadAgentIsCountedAndSwitchable(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 12)
	model.subagentTraces["trace-only"] = &AgentTrace{
		TaskID:    "trace-only",
		AgentType: "reviewer",
		Status:    "running",
		Unread:    true,
	}

	status := ansi.Strip(model.agentBarViewForWidth(80))
	for _, want := range []string{"1 unread", "Ctrl+↑ switch"} {
		if !strings.Contains(status, want) {
			t.Fatalf("trace-only Agent status missing %q: %q", want, status)
		}
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlUp})
	model = updated.(Model)
	if model.focus != FocusAgentBar || model.agentBarSelected != "trace-only" {
		t.Fatalf("Ctrl+Up did not select trace-only Agent: focus=%q selected=%q", model.focus, model.agentBarSelected)
	}
	selected := ansi.Strip(model.agentBarViewForWidth(80))
	if !strings.Contains(selected, "Agent reviewer") {
		t.Fatalf("switcher did not expose trace-only Agent: %q", selected)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.focus != FocusAgentWindow || model.focusedTaskID != "trace-only" {
		t.Fatalf("selected trace-only Agent did not open: focus=%q task=%q", model.focus, model.focusedTaskID)
	}
	if model.subagentTraces["trace-only"].Unread {
		t.Fatal("opening trace-only Agent should clear unread state")
	}
}

func TestAgentUnreadClearsWhenTranscriptOpened(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventSubagentProgress, ToolCallID: "task-1", Message: "new output"})
	if !model.subagentTraces["task-1"].Unread {
		t.Fatal("background child output should become unread")
	}
	model = model.enterAgentWindow("task-1")
	if model.subagentTraces["task-1"].Unread {
		t.Fatal("opening child transcript should clear unread")
	}
}

func TestSubagentStreamingUsesIndependentLivePlane(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("delegate")
	version := model.timeline.Version
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventSubagentModelChunk, ToolCallID: "task-1", Message: "complete\npartial", Generation: 2})
	trace := model.subagentTraces["task-1"]
	if trace == nil || trace.LiveStream == nil || trace.LiveStream.body != "complete\npartial" {
		t.Fatalf("child delta should stay live, got %#v", trace)
	}
	if model.timeline.Version != version || len(trace.Blocks) != 0 {
		t.Fatalf("child delta mutated committed history: parent version %d->%d blocks=%#v", version, model.timeline.Version, trace.Blocks)
	}
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-1"
	view := model.agentWindowView(20, 80)
	if !strings.Contains(view, "complete") || strings.Contains(view, "partial") {
		t.Fatalf("live child view should expose only completed lines, got:\n%s", view)
	}
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventSubagentToolCall, ToolCallID: "task-1", ToolName: "read_file", Message: "running", Generation: 2})
	if trace.LiveStream != nil || len(trace.Blocks) != 2 || trace.Blocks[0].Body != "complete\npartial" {
		t.Fatalf("tool boundary should commit full tail once before tool, got %#v", trace)
	}
}

func TestSubagentTraceRejectsStaleGenerationEvents(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventSubagentStarted, ToolCallID: "task-1", Message: "new run", Generation: 2})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventSubagentModelChunk, ToolCallID: "task-1", Message: "fresh", Generation: 2})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventSubagentFinished, ToolCallID: "task-1", Output: "stale", Generation: 1})
	trace := model.subagentTraces["task-1"]
	if trace.Generation != 2 || trace.Status != "running" || trace.FinalAnswer == "stale" {
		t.Fatalf("stale generation mutated replacement trace: %#v", trace)
	}
}

func TestAgentBarCanSelectCoordinator(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("delegate")
	model.timeline = model.timeline.UpsertSubagentBlock("task-1", "review", "running", "")
	model.focus = FocusAgentBar
	model.agentBarSelected = "task-1"
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.agentBarSelected != coordinatorAgentID {
		t.Fatalf("Up from first child should select coordinator, got %q", model.agentBarSelected)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.focus != FocusComposer {
		t.Fatalf("opening coordinator row should return to coordinator transcript, got %q", model.focus)
	}
}

func TestAgentBarSwitcherKeepsSelectionVisibleInOneRow(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 10)
	model.focus = FocusAgentBar
	for i := 1; i <= 12; i++ {
		id := fmt.Sprintf("task-%02d", i)
		model.timeline = model.timeline.UpsertSubagentBlock(id, "reviewer", "running", "")
		model.subagentTraces[id] = &AgentTrace{TaskID: id, AgentType: fmt.Sprintf("reviewer-%02d", i), Status: "running"}
	}
	model.agentBarSelected = "task-10"

	view := ansi.Strip(model.agentBarViewForWidth(80))
	if renderedHeight(view) != 1 || strings.Contains(view, "\n") {
		t.Fatalf("Agent switcher must stay on one row: %q", view)
	}
	if !strings.Contains(view, "reviewer-10") || !strings.Contains(view, "running") {
		t.Fatalf("selected Agent must remain visible with concise state: %q", view)
	}
}

func TestAgentSwitcherCoordinatorRemainsReachableWhenRowsAreCapped(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 10)
	for i := 1; i <= 12; i++ {
		id := fmt.Sprintf("task-%02d", i)
		model.timeline = model.timeline.UpsertSubagentBlock(id, "reviewer", "running", "")
	}
	model.agentBarSelected = "task-01"
	model.agentBarSelected = model.moveAgentInBar(-1)
	if model.agentBarSelected != coordinatorAgentID {
		t.Fatalf("up from first child = %q, want coordinator", model.agentBarSelected)
	}
}

func TestAgentBarSwitcherShowsCoordinatorOrSelectedAgentInOneRow(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("delegate")
	model.timeline = model.timeline.UpsertSubagentBlock("task-1", "review", "running", "")
	model.subagentTraces["task-1"] = &AgentTrace{TaskID: "task-1", AgentType: "reviewer", Status: "running"}

	coordinator := ansi.Strip(model.agentBarViewForWidth(80))
	if !strings.Contains(coordinator, "Agent Main") || strings.Contains(coordinator, "reviewer") || renderedHeight(coordinator) != 1 {
		t.Fatalf("base Agent row should show only the coordinator: %q", coordinator)
	}

	model.focus = FocusAgentBar
	model.agentBarSelected = "task-1"
	selected := ansi.Strip(model.agentBarViewForWidth(80))
	if !strings.Contains(selected, "Agent reviewer") || strings.Contains(selected, "Main") || renderedHeight(selected) != 1 {
		t.Fatalf("focused Agent row should show only the selection: %q", selected)
	}
}

func TestAgentWindowKeepsPersistentRowForTerminalConversation(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 20)
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-1"
	model.subagentTraces["task-1"] = &AgentTrace{
		TaskID:      "task-1",
		AgentType:   "reviewer",
		Status:      "completed",
		FinalAnswer: "done",
	}

	view := ansi.Strip(model.View())
	if !strings.Contains(view, "Agent reviewer") || !strings.Contains(view, "completed") {
		t.Fatalf("Agent window should retain its persistent status row:\n%s", view)
	}
	if strings.Count(view, "Agent reviewer") != 1 {
		t.Fatalf("Agent window should render exactly one persistent row:\n%s", view)
	}
}

func TestAgentWindowTerminalConversationResumesWithoutLosingState(t *testing.T) {
	var taskID, input string
	model := NewModel(Config{SendSubagentInput: func(id, value string) error {
		taskID, input = id, value
		return nil
	}})
	model = model.SetInput("coordinator draft")
	model.subagentTraces["task-1"] = &AgentTrace{
		TaskID:    "task-1",
		AgentType: "reviewer",
		Status:    "completed",
		Draft:     "resume this task",
		Blocks:    []Block{{ID: "prior", Kind: BlockAssistant, Body: "terminal result"}},
	}

	model = model.enterAgentWindow("task-1")
	if got := model.inputValue(); got != "resume this task" {
		t.Fatalf("terminal Agent draft = %q, want resume draft", got)
	}
	updated, cmd, handled := model.submitAgentInput()
	if !handled || cmd == nil {
		t.Fatal("terminal Agent conversation should accept follow-up input")
	}
	model = updated
	_ = cmd()
	trace := model.subagentTraces["task-1"]
	if taskID != "task-1" || input != "resume this task" {
		t.Fatalf("terminal Agent follow-up routed to task=%q input=%q", taskID, input)
	}
	if len(trace.Blocks) != 2 || trace.Blocks[0].Body != "terminal result" || trace.Blocks[1].Kind != BlockCommand {
		t.Fatalf("terminal Agent transcript was not preserved: %#v", trace.Blocks)
	}
	model = model.exitAgentWindow()
	if got := model.inputValue(); got != "coordinator draft" {
		t.Fatalf("coordinator draft after terminal Agent resume = %q", got)
	}
}
