package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
)

// TestQuestionerAskBlocksUntilRespond drives the Questioner directly: Ask blocks
// until Respond delivers the answer, mirroring how the ask_user tool uses it.
func TestQuestionerAskBlocksUntilRespond(t *testing.T) {
	q := NewQuestioner()
	got := make(chan ask.Answer, 1)
	go func() {
		ans, err := q.Ask(context.Background(), ask.Question{
			Prompt:  "pick",
			Options: []ask.Option{{Label: "A"}, {Label: "B"}},
		})
		if err != nil {
			got <- nil
			return
		}
		got <- ans
	}()
	if !waitPending(q, time.Second) {
		t.Fatal("Ask never reported a pending question")
	}
	q.Respond(ask.Answer{1})
	select {
	case ans := <-got:
		if len(ans) != 1 || ans[0] != 1 {
			t.Fatalf("expected answer [1], got %v", ans)
		}
	case <-time.After(time.Second):
		t.Fatal("Ask did not return after Respond")
	}
}

// TestModelRendersAndAnswersPendingQuestion wires a Questioner into a Model,
// observes the pending question surface via syncPendingQuestion, drives the
// option panel with down+enter, and confirms the answer flows back to Ask.
func TestModelRendersAndAnswersPendingQuestion(t *testing.T) {
	q := NewQuestioner()
	model := NewModel(Config{Questioner: q})
	model = model.SetSize(80, 24)

	got := make(chan ask.Answer, 1)
	go func() {
		ans, _ := q.Ask(context.Background(), ask.Question{
			Prompt:  "Which framework?",
			Options: []ask.Option{{Label: "React"}, {Label: "Vue"}, {Label: "Svelte"}},
		})
		got <- ans
	}()
	if !waitPending(q, time.Second) {
		t.Fatal("Ask never reported a pending question")
	}

	// Any Update triggers syncPendingQuestion.
	updated, _ := model.Update(agentTickMsg{})
	m := updated.(Model)
	if m.question == nil {
		t.Fatalf("expected question to be synced into model state")
	}
	if !strings.Contains(m.View(), "Which framework?") {
		t.Fatalf("question prompt should render:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "Vue") || !strings.Contains(m.View(), "Svelte") {
		t.Fatalf("all options should render:\n%s", m.View())
	}

	// Cursor starts at 0 (React); move down twice to Svelte, then confirm.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.questionCursor != 1 {
		t.Fatalf("expected cursor at 1 after down, got %d", m.questionCursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.questionCursor != 2 {
		t.Fatalf("expected cursor at 2 after second down, got %d", m.questionCursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.question != nil {
		t.Fatalf("question should clear after confirm")
	}

	select {
	case ans := <-got:
		if len(ans) != 1 || ans[0] != 2 {
			t.Fatalf("expected selected answer [2] (Svelte), got %v", ans)
		}
	case <-time.After(time.Second):
		t.Fatal("Ask did not return the selection")
	}
}

// TestModelQuestionCancelSendsEmptyAnswer confirms esc dismisses the panel and
// delivers an empty answer (which the tool surfaces as "no selection").
func TestModelQuestionCancelSendsEmptyAnswer(t *testing.T) {
	q := NewQuestioner()
	model := NewModel(Config{Questioner: q})
	model = model.SetSize(80, 24)

	got := make(chan ask.Answer, 1)
	go func() {
		ans, _ := q.Ask(context.Background(), ask.Question{
			Prompt:  "x",
			Options: []ask.Option{{Label: "A"}},
		})
		got <- ans
	}()
	if !waitPending(q, time.Second) {
		t.Fatal("Ask never reported a pending question")
	}
	updated, _ := model.Update(agentTickMsg{})
	m := updated.(Model)
	if m.question == nil {
		t.Fatal("expected question synced")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.question != nil {
		t.Fatal("question should clear on cancel")
	}
	select {
	case ans := <-got:
		if len(ans) != 0 {
			t.Fatalf("expected empty answer on cancel, got %v", ans)
		}
	case <-time.After(time.Second):
		t.Fatal("Ask did not return after cancel")
	}
}

func TestQuestionResponseContinuesAgentStream(t *testing.T) {
	stream := make(chan tea.Msg, 1)
	stream <- agentEventMsg{event: agent.Event{
		Type:    agent.EventModelChunk,
		Message: "after answer",
	}}
	q := NewQuestioner()
	model := NewModel(Config{Questioner: q})
	model.agent.Busy = true
	model.agent.Stream = stream
	model.question = &ask.Question{
		Prompt:  "Pick one",
		Options: []ask.Option{{Label: "A"}, {Label: "B"}},
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := updated.(Model)
	if next.question != nil {
		t.Fatal("expected question to clear after answer")
	}
	if cmd == nil {
		t.Fatal("expected question answer to continue reading the agent stream")
	}
	msg := cmd()
	if _, ok := msg.(agentEventMsg); !ok {
		t.Fatalf("expected next agent event message, got %#v", msg)
	}
}

func TestPendingQuestionConsumesOrdinaryPromptInput(t *testing.T) {
	q := NewQuestioner()
	model := NewModel(Config{Questioner: q})
	model = model.SetInput("draft")

	go func() {
		_, _ = q.Ask(context.Background(), ask.Question{
			Prompt:  "Pick one",
			Options: []ask.Option{{Label: "A"}, {Label: "B"}},
		})
	}()
	if !waitPending(q, time.Second) {
		t.Fatal("Ask never reported a pending question")
	}
	updated, _ := model.Update(agentTickMsg{})
	m := updated.(Model)
	if m.question == nil {
		t.Fatal("expected question synced")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	next := updated.(Model)
	if next.question == nil {
		t.Fatal("expected ordinary question rune to keep question pending")
	}
	if got := next.inputValue(); got != "draft" {
		t.Fatalf("expected pending question not to mutate prompt input, got %q", got)
	}
}

func TestPendingQuestionAllowsApprovedTimelineScrollKeys(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 14)
	for i := 0; i < 30; i++ {
		model.timeline = model.timeline.AppendBlock(BlockAssistant, "agent", "message")
	}
	model.question = &ask.Question{
		Prompt:  "Pick one",
		Options: []ask.Option{{Label: "A"}, {Label: "B"}},
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)

	if model.scroll == 0 {
		t.Fatal("PageUp should scroll transcript context while a question owns input")
	}
	if model.question == nil {
		t.Fatal("expected question to remain pending after PageUp")
	}
	scrollAfterPageUp := model.scroll
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(Model)
	if model.scroll >= scrollAfterPageUp {
		t.Fatalf("PageDown should move toward the transcript tail, scroll=%d after PageUp=%d", model.scroll, scrollAfterPageUp)
	}
	if model.question == nil {
		t.Fatal("expected question to remain pending after PageDown")
	}
}

func TestPendingQuestionHidesComposer(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(100, 24)
	model.question = &ask.Question{
		Prompt:  "Pick one",
		Options: []ask.Option{{Label: "A"}},
	}

	view := model.View()
	if !strings.Contains(view, "Pick one") {
		t.Fatalf("expected question panel to render:\n%s", view)
	}
	if strings.Contains(view, "> type a message") {
		t.Fatalf("question panel should hide composer while it owns input:\n%s", view)
	}
	if strings.Contains(view, "/ commands") || strings.Contains(view, "@ files") {
		t.Fatalf("question footer should not advertise composer actions:\n%s", view)
	}
}

func TestLeaderKeyIgnoredWhileQuestionPending(t *testing.T) {
	model := NewModel(Config{})
	model.question = &ask.Question{
		Prompt:  "Pick one",
		Options: []ask.Option{{Label: "A"}, {Label: "B"}},
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	model = updated.(Model)

	view := model.View()
	if !strings.Contains(view, "Pick one") || strings.Contains(view, "leader") {
		t.Fatalf("expected question panel to remain authoritative:\n%s", view)
	}
	if got := strings.TrimSpace(model.renderFooter(model.currentLayout())); strings.TrimSpace(got) == "" {
		t.Fatalf("question panel should keep a footer row, got %q", got)
	}
}

func TestQuestionRequestDoesNotArmRemovedLeaderKey(t *testing.T) {
	q := NewQuestioner()
	model := NewModel(Config{Questioner: q})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	model = updated.(Model)

	go func() {
		_, _ = q.Ask(context.Background(), ask.Question{
			Prompt:  "Pick one",
			Options: []ask.Option{{Label: "A"}, {Label: "B"}},
		})
	}()
	if !waitPending(q, time.Second) {
		t.Fatal("Ask never reported a pending question")
	}
	updated, _ = model.Update(agentTickMsg{})
	model = updated.(Model)

	if model.question == nil {
		t.Fatal("expected question request to remain pending")
	}
}

func TestQuestionRequestHidesOpenSuggestions(t *testing.T) {
	q := NewQuestioner()
	model := NewModel(Config{Questioner: q})
	model = model.SetInput("/")
	model = model.updateSuggestions()
	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatal("test setup expected visible command suggestions")
	}

	go func() {
		_, _ = q.Ask(context.Background(), ask.Question{
			Prompt:  "Pick one",
			Options: []ask.Option{{Label: "A"}, {Label: "B"}},
		})
	}()
	if !waitPending(q, time.Second) {
		t.Fatal("Ask never reported a pending question")
	}
	updated, _ := model.Update(agentTickMsg{})
	model = updated.(Model)

	if model.composer.Suggestions != nil && model.composer.Suggestions.IsVisible() {
		t.Fatalf("question request should hide open suggestions:\n%s", model.View())
	}
}

func TestPendingQuestionWaitsBehindPermissionDock(t *testing.T) {
	q := NewQuestioner()
	model := NewModel(Config{Questioner: q})
	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	}
	go func() {
		_, _ = q.Ask(context.Background(), ask.Question{
			Prompt:  "Continue?",
			Options: []ask.Option{{Label: "Yes"}, {Label: "No"}},
		})
	}()
	if !waitPending(q, time.Second) {
		t.Fatal("Ask never reported a pending question")
	}
	defer q.Respond(nil)

	updated, _ := model.Update(agentTickMsg{})
	next := updated.(Model)

	if next.question != nil {
		t.Fatal("pending ask_user question should not render while permission owns input")
	}
	if next.agent.Pending == nil {
		t.Fatal("permission dock should remain pending")
	}
	if !q.HasPending() {
		t.Fatal("question should stay pending behind the permission dock")
	}
}

func TestDeferredQuestionAppearsAfterPermissionClears(t *testing.T) {
	q := NewQuestioner()
	model := NewModel(Config{Questioner: q})
	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	}
	go func() {
		_, _ = q.Ask(context.Background(), ask.Question{
			Prompt:  "Continue?",
			Options: []ask.Option{{Label: "Yes"}, {Label: "No"}},
		})
	}()
	if !waitPending(q, time.Second) {
		t.Fatal("Ask never reported a pending question")
	}
	defer q.Respond(nil)

	updated, _ := model.Update(agentTickMsg{})
	model = updated.(Model)
	model.agent.Pending = nil
	updated, _ = model.Update(agentTickMsg{})
	model = updated.(Model)

	if model.question == nil {
		t.Fatal("deferred ask_user question should appear after permission clears")
	}
	if model.question.Prompt != "Continue?" {
		t.Fatalf("expected deferred question prompt, got %#v", model.question)
	}
}

func TestPermissionRequestDefersVisibleQuestionDock(t *testing.T) {
	model := NewModel(Config{})
	model.question = &ask.Question{
		Prompt:  "Continue?",
		Options: []ask.Option{{Label: "Yes"}, {Label: "No"}},
	}
	model.questionCursor = 1

	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	})

	if model.question != nil {
		t.Fatal("permission request should hide any visible ask_user question dock")
	}
	if model.questionCursor != 0 || model.questionSelected != nil {
		t.Fatalf("permission request should reset deferred question UI state, cursor=%d selected=%#v", model.questionCursor, model.questionSelected)
	}
	if model.agent.Pending == nil {
		t.Fatal("permission request should remain pending")
	}
}

func TestPermissionDockSuppressesQuestionRendering(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	}
	model.question = &ask.Question{
		Prompt:  "Continue?",
		Options: []ask.Option{{Label: "Yes"}, {Label: "No"}},
	}

	view := model.View()
	if !strings.Contains(view, "Permission required") {
		t.Fatalf("expected permission dock to render:\n%s", view)
	}
	if strings.Contains(view, "Continue?") || strings.Contains(view, "question") {
		t.Fatalf("question dock should not render while permission owns input:\n%s", view)
	}
}

func TestQuestionPanelKeepsCursorVisibleWhenClipped(t *testing.T) {
	options := make([]ask.Option, 0, 12)
	for i := 1; i <= 12; i++ {
		options = append(options, ask.Option{Label: "Option " + string(rune('A'+i-1))})
	}
	model := NewModel(Config{})
	model = model.SetSize(121, 16)
	model.question = &ask.Question{
		Prompt:  "Pick one",
		Options: options,
	}
	model.questionCursor = 10

	view := model.View()
	assertViewFits(t, view, model.width, model.height)
	stripped := strings.Map(func(r rune) rune {
		// keep printable; tests care about the selected option text
		return r
	}, view)
	if !strings.Contains(stripped, "Option K") {
		t.Fatalf("clipped question panel should keep selected option visible:\n%s", view)
	}
	// Selected row uses ❯ prefix (ANSI may separate the glyph from the label).
	if !strings.Contains(view, "❯") {
		t.Fatalf("clipped question panel should keep cursor glyph visible:\n%s", view)
	}
}

func TestSingleQuestionNumberShortcutAnswers(t *testing.T) {
	q := NewQuestioner()
	model := NewModel(Config{Questioner: q})
	model = model.SetInput("draft")

	got := make(chan ask.Answer, 1)
	go func() {
		ans, _ := q.Ask(context.Background(), ask.Question{
			Prompt:  "Pick one",
			Options: []ask.Option{{Label: "A"}, {Label: "B"}, {Label: "C"}},
		})
		got <- ans
	}()
	if !waitPending(q, time.Second) {
		t.Fatal("Ask never reported a pending question")
	}
	updated, _ := model.Update(agentTickMsg{})
	m := updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	next := updated.(Model)
	if next.question != nil {
		t.Fatal("expected number shortcut to answer and clear single-select question")
	}
	if gotDraft := next.inputValue(); gotDraft != "draft" {
		t.Fatalf("expected number shortcut not to mutate prompt input, got %q", gotDraft)
	}
	select {
	case ans := <-got:
		if len(ans) != 1 || ans[0] != 1 {
			t.Fatalf("expected selected answer [1], got %v", ans)
		}
	case <-time.After(time.Second):
		t.Fatal("Ask did not return after number shortcut")
	}
}

func TestMultiQuestionNumberShortcutTogglesSelection(t *testing.T) {
	model := NewModel(Config{})
	model.question = &ask.Question{
		Prompt:  "Pick many",
		Multi:   true,
		Options: []ask.Option{{Label: "A"}, {Label: "B"}, {Label: "C"}},
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	next := updated.(Model)
	if next.question == nil {
		t.Fatal("expected multi-select number shortcut to keep question pending")
	}
	if !next.questionSelected[1] {
		t.Fatalf("expected option 2 selected, got %#v", next.questionSelected)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	next = updated.(Model)
	if next.questionSelected[1] {
		t.Fatalf("expected second number shortcut to toggle option 2 off, got %#v", next.questionSelected)
	}
}

func TestQuestionNavigationWrapsAroundOptions(t *testing.T) {
	model := NewModel(Config{})
	model.question = &ask.Question{
		Prompt:  "Pick one",
		Options: []ask.Option{{Label: "A"}, {Label: "B"}, {Label: "C"}},
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.questionCursor != 2 {
		t.Fatalf("expected Up on first option to wrap to last, got cursor=%d", model.questionCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.questionCursor != 0 {
		t.Fatalf("expected Down on last option to wrap to first, got cursor=%d", model.questionCursor)
	}
}

func TestQuestionNavigationSupportsCtrlPN(t *testing.T) {
	model := NewModel(Config{})
	model.question = &ask.Question{
		Prompt:  "Pick one",
		Options: []ask.Option{{Label: "A"}, {Label: "B"}, {Label: "C"}},
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	model = updated.(Model)
	if model.questionCursor != 1 {
		t.Fatalf("expected Ctrl+N to move question cursor down to 1, got cursor=%d", model.questionCursor)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	model = updated.(Model)
	if model.questionCursor != 0 {
		t.Fatalf("expected Ctrl+P to move question cursor up to 0, got cursor=%d", model.questionCursor)
	}
}

func waitPending(q *Questioner, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if q.HasPending() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
