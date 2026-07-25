package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
)

func updateWithKey(t *testing.T, model Model, key tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := model.Update(key)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want tui.Model", updated)
	}
	return next, cmd
}

func TestReverseHistorySearchAcceptsOlderMatchWithoutSubmitting(t *testing.T) {
	model := NewModel(Config{}).SetInput("original draft")
	model.composer.History = []string{"oldest prompt", "middle prompt", "newest prompt"}
	beforeTimeline := model.timeline

	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd != nil {
		t.Fatal("opening reverse history search should not return a command")
	}
	if !model.search.Active {
		t.Fatal("Ctrl+R should open reverse history search")
	}
	if got := model.inputValue(); got != "original draft" {
		t.Fatalf("opening search changed the draft: got %q", got)
	}
	if footer := model.renderFooter(model.currentLayout()); !strings.Contains(footer, "newest prompt") {
		t.Fatalf("reverse search should initially select the newest prompt, footer=%q", footer)
	}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
	model, cmd = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	// Focus/blink cmds after closing search are fine; agent submit is not.
	if model.search.Active {
		t.Fatal("Enter should close reverse history search")
	}
	if got := model.inputValue(); got != "middle prompt" {
		t.Fatalf("repeated Ctrl+R should select an older result, got %q", got)
	}
	if !reflect.DeepEqual(model.timeline, beforeTimeline) {
		t.Fatal("accepting a history result must not create or submit a timeline turn")
	}
	_ = cmd
}

func TestReverseHistorySearchNavigatesWithArrowsAndControlKeys(t *testing.T) {
	history := []string{"oldest prompt", "middle prompt", "newest prompt"}
	tests := []struct {
		name string
		keys []tea.KeyType
	}{
		{name: "arrows", keys: []tea.KeyType{tea.KeyUp, tea.KeyUp, tea.KeyDown}},
		{name: "control keys", keys: []tea.KeyType{tea.KeyCtrlP, tea.KeyCtrlP, tea.KeyCtrlN}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(Config{})
			model.composer.History = history
			model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
			for _, key := range tt.keys {
				model, _ = updateWithKey(t, model, tea.KeyMsg{Type: key})
			}
			model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

			if got := model.inputValue(); got != "middle prompt" {
				t.Fatalf("older/newer navigation accepted %q, want %q", got, "middle prompt")
			}
		})
	}
}

func TestReverseHistorySearchShowsOverlayConventionsAtStandardWidth(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 24)
	model.composer.History = []string{"newest prompt"}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})

	footer := model.renderFooter(model.currentLayout())
	for _, want := range []string{"newest prompt", "↑↓", "Enter", "Esc"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("reverse-search overlay at 80 columns should show %q convention, footer=%q", want, footer)
		}
	}
}

func TestReverseHistorySearchFiltersPersistedPrompts(t *testing.T) {
	path := t.TempDir() + "/prompt-history.json"
	if err := savePromptHistory(path, []string{"fix parser", "write docs", "fix renderer"}); err != nil {
		t.Fatalf("save prompt history: %v", err)
	}
	model := NewModel(Config{PromptHistoryPath: path}).SetInput("draft")

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("docs")})
	footer := model.renderFooter(model.currentLayout())
	if !strings.Contains(footer, "docs") || !strings.Contains(footer, "write docs") || strings.Contains(footer, "fix renderer") {
		t.Fatalf("typing should filter persisted prompt history, footer=%q", footer)
	}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if got := model.inputValue(); got != "write docs" {
		t.Fatalf("accepted filtered prompt = %q, want %q", got, "write docs")
	}
	if model.search.Active {
		t.Fatal("accept should close reverse history search without submitting a turn")
	}
}

func TestReverseHistorySearchRefreshesCachedMatchesWhenHistoryChanges(t *testing.T) {
	model := NewModel(Config{})
	model.composer.History = []string{"old needle prompt"}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("needle")})

	model.composer.History = append(model.composer.History, "new needle prompt")
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if got := model.inputValue(); got != "new needle prompt" {
		t.Fatalf("history change left cached matches stale: accepted %q", got)
	}
}

func TestReverseHistorySearchCancelRestoresExactOriginalDraft(t *testing.T) {
	model := NewModel(Config{}).SetInput("original draft")
	model.composer.Input.SetCursor(len("original"))
	model.composer.History = []string{"older original", "newest prompt"}
	model.composer.HistoryIndex = 0
	model.composer.HistoryDraft = "parked draft"
	beforeComposer := model.composer
	beforeDraft := model.inputValue()
	beforeLine := model.composer.Input.LineInfo()

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("older")})
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.search.Active {
		t.Fatal("Esc should close reverse history search")
	}
	if got := model.inputValue(); got != beforeDraft {
		t.Fatalf("Esc restored draft %q, want %q", got, beforeDraft)
	}
	if got := model.composer.Input.LineInfo(); got != beforeLine {
		t.Fatalf("Esc did not restore the original cursor state: got %#v want %#v", got, beforeLine)
	}
	if model.composer.HistoryIndex != beforeComposer.HistoryIndex || model.composer.HistoryDraft != beforeComposer.HistoryDraft {
		t.Fatalf("Esc changed composer history navigation state: index=%d draft=%q", model.composer.HistoryIndex, model.composer.HistoryDraft)
	}
}

func TestReverseHistorySearchAcceptAfterResizeReplacesDraftPayload(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 24).SetInput("original draft")
	model.composer.History = []string{"selected history prompt"}
	model.composer.Pastes = []PasteEntry{{Marker: "[Pasted ~3 lines]", Text: "old\npaste\npayload"}}
	model.composer.RawPasteCandidateAt = time.Unix(123, 456)

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if cmd != nil {
		t.Fatal("resizing during reverse search should not return a command")
	}
	model = updated.(Model)
	resizedInputWidth := model.composer.Input.Width()
	model.composer.Input.Placeholder = "current resized placeholder"
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.search.Active {
		t.Fatal("accept should close reverse search without submitting a turn")
	}
	if got := model.composer.expandedValue(); got != "selected history prompt" {
		t.Fatalf("accepted history payload = %q, want selected prompt only", got)
	}
	if len(model.composer.Pastes) != 0 || !model.composer.RawPasteCandidateAt.IsZero() {
		t.Fatalf("accept retained stale paste payload: pastes=%#v rawPasteAt=%v", model.composer.Pastes, model.composer.RawPasteCandidateAt)
	}
	if model.width != 120 || model.composer.Input.Width() != resizedInputWidth || model.currentLayout().Width != 120 {
		t.Fatalf("accept restored stale size: model=%d input=%d (want %d) layout=%d", model.width, model.composer.Input.Width(), resizedInputWidth, model.currentLayout().Width)
	}
	if got := model.composer.Input.Placeholder; got != "current resized placeholder" {
		t.Fatalf("accept restored stale composer config: placeholder=%q", got)
	}
}

func TestReverseHistorySearchCancelAfterResizeRestoresDraftPayload(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 24).SetInput("original draft")
	model.composer.Input.SetCursor(len("original"))
	model.composer.History = []string{"history prompt"}
	model.composer.HistoryIndex = 0
	model.composer.HistoryDraft = "parked draft"
	originalPastes := []PasteEntry{{Marker: "[Pasted ~3 lines]", Text: "saved\npaste\npayload"}}
	model.composer.Pastes = append([]PasteEntry(nil), originalPastes...)
	originalRawPasteAt := time.Unix(789, 123)
	model.composer.RawPasteCandidateAt = originalRawPasteAt
	originalExpanded := model.composer.expandedValue()

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if cmd != nil {
		t.Fatal("resizing during reverse search should not return a command")
	}
	model = updated.(Model)
	resizedInputWidth := model.composer.Input.Width()
	model.composer.Input.Placeholder = "current resized placeholder"
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})

	if model.search.Active {
		t.Fatal("cancel should close reverse search")
	}
	if got := model.composer.expandedValue(); got != originalExpanded {
		t.Fatalf("cancel restored expanded payload %q, want %q", got, originalExpanded)
	}
	if !reflect.DeepEqual(model.composer.Pastes, originalPastes) || model.composer.RawPasteCandidateAt != originalRawPasteAt {
		t.Fatalf("cancel did not restore paste payload: pastes=%#v rawPasteAt=%v", model.composer.Pastes, model.composer.RawPasteCandidateAt)
	}
	if model.composer.HistoryIndex != 0 || model.composer.HistoryDraft != "parked draft" {
		t.Fatalf("cancel did not restore history navigation: index=%d draft=%q", model.composer.HistoryIndex, model.composer.HistoryDraft)
	}
	line := model.composer.Input.LineInfo()
	if got := line.StartColumn + line.ColumnOffset; got != len("original") {
		t.Fatalf("cancel restored cursor column %d, want %d", got, len("original"))
	}
	if model.width != 120 || model.composer.Input.Width() != resizedInputWidth || model.currentLayout().Width != 120 {
		t.Fatalf("cancel restored stale size: model=%d input=%d (want %d) layout=%d", model.width, model.composer.Input.Width(), resizedInputWidth, model.currentLayout().Width)
	}
	if got := model.composer.Input.Placeholder; got != "current resized placeholder" {
		t.Fatalf("cancel restored stale composer config: placeholder=%q", got)
	}
}

var reverseHistoryFooterSink string

func TestReverseHistorySearchWarmFooterAllocationsAreBounded(t *testing.T) {
	footerAllocs := func(history []string) float64 {
		t.Helper()
		model := NewModel(Config{}).SetSize(120, 30)
		model.composer.History = history
		model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
		model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("needle")})
		width := model.currentLayout().TimelineW
		reverseHistoryFooterSink = model.reverseHistorySearchFooter(width)
		return testing.AllocsPerRun(100, func() {
			reverseHistoryFooterSink = model.reverseHistorySearchFooter(width)
		})
	}

	single := footerAllocs([]string{"PROMPT 000 NEEDLE"})
	history := make([]string, maxPromptHistoryEntries)
	for i := range history {
		history[i] = fmt.Sprintf("PROMPT %03d NEEDLE", i)
	}
	maximum := footerAllocs(history)
	t.Logf("warm footer allocations: one=%.2f max-history=%.2f", single, maximum)

	if maximum > single+2 {
		t.Fatalf("warm footer allocations grew with history: one=%.2f max=%d=%.2f", single, maxPromptHistoryEntries, maximum)
	}
	if maximum > 20 {
		t.Fatalf("warm footer allocated %.2f times with max history, want <= 20", maximum)
	}
}

func BenchmarkReverseHistorySearchWarmFooterMaxHistory(b *testing.B) {
	history := make([]string, maxPromptHistoryEntries)
	for i := range history {
		history[i] = fmt.Sprintf("PROMPT %03d NEEDLE", i)
	}
	model := NewModel(Config{}).SetSize(120, 30)
	model.composer.History = history
	model = model.startReverseHistorySearch()
	model.reverseHistory.query = "needle"
	model = model.rebuildReverseHistoryMatches()
	width := model.currentLayout().TimelineW
	reverseHistoryFooterSink = model.reverseHistorySearchFooter(width)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reverseHistoryFooterSink = model.reverseHistorySearchFooter(width)
	}
}

func TestReverseHistorySearchDoesNotBypassModal(t *testing.T) {
	tests := []struct {
		name  string
		model Model
		route keyRoute
	}{
		{
			name: "confirmation",
			model: func() Model {
				m := NewModel(Config{})
				m.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file"}
				return m
			}(),
			route: keyRouteConfirmation,
		},
		{
			name: "question",
			model: func() Model {
				m := NewModel(Config{})
				m.question = &ask.Question{Prompt: "choose", Options: []ask.Option{{Label: "one"}}}
				return m
			}(),
			route: keyRouteQuestion,
		},
		{
			name:  "overlay",
			model: NewModel(Config{}).openAlert("notice", "keep open", toastInfo),
			route: keyRouteOverlay,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.model.composer.History = []string{"history prompt"}
			next, cmd := updateWithKey(t, tt.model, tea.KeyMsg{Type: tea.KeyCtrlR})
			if cmd != nil || next.search.Active || next.activeKeyRoute() != tt.route {
				t.Fatalf("Ctrl+R bypassed %s modal: route=%v activeSearch=%v cmd=%v", tt.name, next.activeKeyRoute(), next.search.Active, cmd != nil)
			}
		})
	}
}

func TestReverseHistorySearchEmptyHistoryIsNoOp(t *testing.T) {
	model := NewModel(Config{}).SetInput("keep me")
	beforeLine := model.composer.Input.LineInfo()

	next, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd != nil || next.search.Active || next.inputValue() != "keep me" || next.composer.Input.LineInfo() != beforeLine {
		t.Fatalf("Ctrl+R with empty history should be a clean no-op: active=%v input=%q cmd=%v", next.search.Active, next.inputValue(), cmd != nil)
	}
}

func TestCtrlLPreservesStateDuringActiveStreaming(t *testing.T) {
	model := modelWithLiveStream(BlockAssistant, "complete line\nunfinished tail", len("complete line\n"))
	model.live.SessionID = "session-redraw"
	model.agent.LiveGeneration = 41
	model.agent.LiveStream.generation = 41
	model.agent.RunGeneration = 17
	model.agent.LiveDetail = "streaming response"
	model.agent.QueuedPrompts = []string{"queued one", "queued two"}
	stream := make(chan tea.Msg)
	cancelled := false
	model.agent.Stream = stream
	model.agent.Cancel = func() { cancelled = true }
	model = model.SetInput("draft to preserve")
	model.composer.Input.SetCursor(len("draft"))
	model.scroll = 7
	model.scrollPaused = true
	model.newOutputBelow = true
	model.newOutputCount = 3
	model.mode = ModePlan
	model.leaderPending = true
	model.whichKeyVisible = true
	model.focus = FocusComposer
	model.agentBarSelected = "child-1"
	model.focusedTaskID = "child-1"
	model.coordinatorDraft = "coordinator parked draft"
	model.subagentTraces["child-1"] = &AgentTrace{
		TaskID:     "child-1",
		Status:     "running",
		Draft:      "child draft",
		Unread:     true,
		Generation: 9,
		Blocks:     []Block{{ID: "child-block", Kind: BlockAssistant, Body: "child conversation"}},
		LiveStream: &liveStreamState{kind: BlockReasoning, body: "child live", visibleLen: len("child live"), generation: 9},
	}

	beforeTimeline := model.timeline
	beforeLive := model.agent.LiveStream
	beforeLiveValue := *beforeLive
	beforeDraft := model.inputValue()
	beforeLine := model.composer.Input.LineInfo()
	beforeQueue := append([]string(nil), model.agent.QueuedPrompts...)
	beforeCancel := reflect.ValueOf(model.agent.Cancel).Pointer()
	beforeTrace := *model.subagentTraces["child-1"]
	beforeTrace.Blocks = append([]Block(nil), beforeTrace.Blocks...)
	beforeTraceLive := *beforeTrace.LiveStream

	next, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlL})
	if cmd == nil {
		t.Fatal("Ctrl+L should return a terminal clear/redraw command")
	}
	if got, want := reflect.TypeOf(cmd()), reflect.TypeOf(tea.ClearScreen()); got != want {
		t.Fatalf("Ctrl+L command returned %v, want Bubble Tea clear-screen message %v", got, want)
	}
	if next.live.SessionID != "session-redraw" || !reflect.DeepEqual(next.timeline, beforeTimeline) || next.timeline.Version != beforeTimeline.Version {
		t.Fatal("Ctrl+L changed session or committed timeline state")
	}
	if next.agent.LiveStream != beforeLive || *next.agent.LiveStream != beforeLiveValue || next.agent.LiveGeneration != 41 || next.agent.RunGeneration != 17 {
		t.Fatal("Ctrl+L changed live stream identity, body, kind, or generation")
	}
	if !next.agent.Busy || next.agent.LiveDetail != "streaming response" || !reflect.DeepEqual(next.agent.QueuedPrompts, beforeQueue) {
		t.Fatal("Ctrl+L changed active run, busy detail, or queued prompts")
	}
	if next.agent.Stream != stream || next.agent.Cancel == nil || reflect.ValueOf(next.agent.Cancel).Pointer() != beforeCancel || cancelled {
		t.Fatal("Ctrl+L changed or invoked the active stream/cancel handles")
	}
	if next.inputValue() != beforeDraft || next.composer.Input.LineInfo() != beforeLine || next.scroll != 7 || !next.scrollPaused {
		t.Fatal("Ctrl+L changed draft/cursor or transcript scroll state")
	}
	if next.mode != ModePlan || !next.leaderPending || !next.whichKeyVisible || !next.newOutputBelow || next.newOutputCount != 3 {
		t.Fatal("Ctrl+L changed mode or unrelated UI state")
	}
	if next.agentBarSelected != "child-1" || next.focusedTaskID != "child-1" || next.coordinatorDraft != "coordinator parked draft" {
		t.Fatal("Ctrl+L changed agent focus or coordinator draft state")
	}
	trace := next.subagentTraces["child-1"]
	if trace == nil || !reflect.DeepEqual(*trace, beforeTrace) || *trace.LiveStream != beforeTraceLive {
		t.Fatal("Ctrl+L changed child-agent conversation, draft, unread, or live state")
	}
}

func TestCtrlLPreservesStateWhenModalOwnsInput(t *testing.T) {
	tests := []struct {
		name  string
		model Model
		route keyRoute
	}{
		{
			name: "confirmation",
			model: func() Model {
				m := NewModel(Config{})
				m.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file"}
				return m
			}(),
			route: keyRouteConfirmation,
		},
		{
			name: "question",
			model: func() Model {
				m := NewModel(Config{})
				m.question = &ask.Question{Prompt: "choose", Options: []ask.Option{{Label: "one"}}}
				return m
			}(),
			route: keyRouteQuestion,
		},
		{
			name:  "overlay",
			model: NewModel(Config{}).openMenu("modal", "", []menuItem{{label: "one"}, {label: "two"}}),
			route: keyRouteOverlay,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, cmd := updateWithKey(t, tt.model, tea.KeyMsg{Type: tea.KeyCtrlL})
			if cmd != nil || next.activeKeyRoute() != tt.route {
				t.Fatalf("Ctrl+L bypassed %s modal: route=%v cmd=%v", tt.name, next.activeKeyRoute(), cmd != nil)
			}
		})
	}
}
