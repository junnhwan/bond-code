package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/session"
)

// fakeSessionHistory is a test double for SessionHistoryController: it serves a
// fixed event tree and records fork-resume calls instead of touching disk.
type fakeSessionHistory struct {
	events       []session.Event
	seed         []SeedMessage
	newSessionID string
	loadErr      error
	forkCalls    int
	lastForkAt   string
}

func (f *fakeSessionHistory) LoadEvents(string) ([]session.Event, error) {
	return f.events, f.loadErr
}

func (f *fakeSessionHistory) ResumeFromEvent(sessionID, eventID string) (string, []SeedMessage, error) {
	f.forkCalls++
	f.lastForkAt = eventID
	return f.newSessionID, f.seed, nil
}

// noopChatRunner satisfies ChatRunner without doing any work; tests only need a
// non-nil Chat so Submit produces a runAgentMsg instead of an error.
type noopChatRunner struct{}

func (noopChatRunner) RunWithEvents(context.Context, string, agent.EventSink) (*agent.RunResult, error) {
	return nil, nil
}
func (noopChatRunner) Compact(context.Context, agent.EventSink) error { return nil }

func pressKey(m Model, t tea.KeyType) Model {
	u, _ := m.Update(tea.KeyMsg{Type: t})
	return u.(Model)
}

func newHistoryModel(fake *fakeSessionHistory) Model {
	return NewModel(Config{
		CommandEnv:     command.Env{SessionID: "s"},
		Status:         Status{SessionID: "s"},
		SessionHistory: fake,
	})
}

func openHistoryFromSlash(t *testing.T, model Model) Model {
	t.Helper()
	for attempts := 0; model.focus != FocusComposer && attempts < 2; attempts++ {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
		model = updated.(Model)
	}
	if model.focus != FocusComposer {
		t.Fatalf("Esc should return focus to the composer before /history, got %q", model.focus)
	}
	model = model.SetInput("/history")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !model.history.visible || model.mode != ModeHistory {
		t.Fatalf("/history should open history overlay, got mode=%q visible=%v", model.mode, model.history.visible)
	}
	return model
}

// /history enters history, ↑ moves the cursor up the tree, and Enter
// fork-resumes at the cursor and switches onto the new branch. Esc exits
// without switching
// (design test matrix §2).
func TestHistoryBrowseAndForkResume(t *testing.T) {
	events := []session.Event{
		{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
		{EventID: "b", Type: "message", ParentID: "a", Message: &session.Message{Role: session.RoleAssistant, Content: "a1"}},
		{EventID: "c", Type: "message", ParentID: "b", Message: &session.Message{Role: session.RoleUser, Content: "q2"}},
	}
	fake := &fakeSessionHistory{
		events:       events,
		newSessionID: "s-forked",
		seed:         []SeedMessage{{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"}},
	}
	model := newHistoryModel(fake)

	// /history opens with the cursor on the most recent node.
	model = openHistoryFromSlash(t, model)
	if !model.mode.IsHistory() || !model.history.visible {
		t.Fatalf("/history should enter history mode, got mode=%q visible=%v", model.mode, model.history.visible)
	}
	if got := model.history.cursorEventID(); got != "c" {
		t.Fatalf("cursor should default to last node c, got %q", got)
	}

	// ↑ walks the cursor up the tree: c -> b -> a.
	model = pressKey(model, tea.KeyUp)
	if got := model.history.cursorEventID(); got != "b" {
		t.Fatalf("cursor should move to b after ↑, got %q", got)
	}
	model = pressKey(model, tea.KeyUp)
	if got := model.history.cursorEventID(); got != "a" {
		t.Fatalf("cursor should move to a after second ↑, got %q", got)
	}

	// ↓ walks back down: a -> b.
	model = pressKey(model, tea.KeyDown)
	if got := model.history.cursorEventID(); got != "b" {
		t.Fatalf("cursor should move to b after ↓, got %q", got)
	}

	// Enter fork-resumes at the cursor (b), switching onto the new branch.
	model = pressKey(model, tea.KeyEnter)
	if fake.forkCalls != 1 || fake.lastForkAt != "b" {
		t.Fatalf("expected fork-resume at b, got calls=%d event=%q", fake.forkCalls, fake.lastForkAt)
	}
	if model.mode != ModeNormal {
		t.Fatalf("expected mode back to normal after fork, got %q", model.mode)
	}
	if model.cfg.Status.SessionID != "s-forked" {
		t.Fatalf("expected session id switched to forked branch, got %q", model.cfg.Status.SessionID)
	}
	if model.live.SessionID != "s-forked" {
		t.Fatalf("expected live session id switched to forked branch, got %q", model.live.SessionID)
	}
	if model.cfg.CommandEnv.SessionID != "s-forked" {
		t.Fatalf("expected command env session id switched to forked branch, got %q", model.cfg.CommandEnv.SessionID)
	}
	if model.history.visible {
		t.Fatal("history overlay should be hidden after fork-resume")
	}

	// Esc without Enter exits and leaves the session untouched (invariant 2).
	model = newHistoryModel(fake)
	model = openHistoryFromSlash(t, model)
	model = pressKey(model, tea.KeyEsc)
	if model.history.visible {
		t.Fatal("esc should exit history mode")
	}
	if fake.forkCalls != 1 { // still 1 from the earlier Enter, no new fork
		t.Fatalf("esc should not fork, got calls=%d", fake.forkCalls)
	}
	if model.cfg.Status.SessionID != "s" {
		t.Fatalf("esc should not switch session, got %q", model.cfg.Status.SessionID)
	}
}

func TestHistoryOverlayRestoresPreviousMode(t *testing.T) {
	fake := &fakeSessionHistory{
		events: []session.Event{
			{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
			{EventID: "b", Type: "message", ParentID: "a", Message: &session.Message{Role: session.RoleAssistant, Content: "a1"}},
		},
		newSessionID: "s-forked",
		seed:         []SeedMessage{{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"}},
	}
	model := newHistoryModel(fake)
	model.mode = ModePlan

	model = openHistoryFromSlash(t, model)
	if model.mode != ModeHistory {
		t.Fatalf("expected history mode after /history, got %q", model.mode)
	}
	model = pressKey(model, tea.KeyEsc)
	if model.mode != ModePlan {
		t.Fatalf("Esc should restore previous plan mode, got %q", model.mode)
	}

	model = newHistoryModel(fake)
	model.mode = ModePlan
	model = openHistoryFromSlash(t, model)
	model = pressKey(model, tea.KeyEnter)
	if model.mode != ModePlan {
		t.Fatalf("fork-resume should restore previous plan mode, got %q", model.mode)
	}
	if model.cfg.Status.SessionID != "s-forked" {
		t.Fatalf("expected forked session, got %q", model.cfg.Status.SessionID)
	}
}

func TestPermissionRequestExitsHistoryOverlay(t *testing.T) {
	model := newHistoryModel(&fakeSessionHistory{
		events: []session.Event{
			{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
		},
	})
	model = model.SetSize(100, 24)
	model = openHistoryFromSlash(t, model)
	if !model.history.visible {
		t.Fatal("test setup expected history overlay")
	}

	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	})

	if model.history.visible || model.mode == ModeHistory {
		t.Fatalf("permission request should exit history overlay, visible=%v mode=%q", model.history.visible, model.mode)
	}
	view := model.View()
	if !strings.Contains(view, "Permission required") || strings.Contains(view, "session timeline") {
		t.Fatalf("permission dock should be visible instead of history overlay:\n%s", view)
	}
}

func TestPermissionPendingOwnsKeysBeforeHistoryOverlay(t *testing.T) {
	model := newHistoryModel(&fakeSessionHistory{
		events: []session.Event{
			{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
		},
	})
	model.cfg.Confirmer = NewConfirmer()
	model = openHistoryFromSlash(t, model)
	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)

	if model.agent.Pending != nil {
		t.Fatal("Esc should reject permission before history handles the key")
	}
	if !model.history.visible {
		t.Fatal("history should not consume Esc while permission owns input")
	}
}

func TestPendingQuestionExitsHistoryOverlay(t *testing.T) {
	q := NewQuestioner()
	model := newHistoryModel(&fakeSessionHistory{
		events: []session.Event{
			{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
		},
	})
	model.questioner = q
	model = model.SetSize(100, 24)
	model = openHistoryFromSlash(t, model)
	if !model.history.visible {
		t.Fatal("test setup expected history overlay")
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

	if model.history.visible || model.mode == ModeHistory {
		t.Fatalf("pending question should exit history overlay, visible=%v mode=%q", model.history.visible, model.mode)
	}
	view := model.View()
	if !strings.Contains(view, "Continue?") || strings.Contains(view, "session timeline") {
		t.Fatalf("question dock should be visible instead of history overlay:\n%s", view)
	}
}

// The history view renders the tree with the cursor highlighted and off-path
// sibling branches collapsed to a rule-based BranchSummary (design test matrix
// §3).
func TestHistoryViewRendersTreeAndCollapsedBranches(t *testing.T) {
	//   a -> b -> c   (cursor path)
	//        \-> e    (off-path sibling, collapsed to BranchSummary)
	events := []session.Event{
		{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
		{EventID: "b", Type: "message", ParentID: "a", Message: &session.Message{Role: session.RoleAssistant, Content: "a1"}},
		{EventID: "c", Type: "message", ParentID: "b", Message: &session.Message{Role: session.RoleUser, Content: "q2"}},
		{EventID: "e", Type: "message", ParentID: "b", Message: &session.Message{Role: session.RoleUser, Content: "try approach B"}},
	}
	model := newHistoryModel(&fakeSessionHistory{events: events})
	model = model.SetSize(80, 20)
	model = openHistoryFromSlash(t, model)

	view := model.View()
	for _, want := range []string{"session timeline", "[user] q1", "▶", "Abandoned branch", "try approach B"} {
		if !strings.Contains(view, want) {
			t.Errorf("history view missing %q:\n%s", want, view)
		}
	}
	// The cursor node c is marked; moving up re-marks its parent b.
	model = pressKey(model, tea.KeyUp)
	if got := model.View(); !strings.Contains(got, "▶") || !strings.Contains(got, "[assistant] a1") {
		t.Errorf("expected cursor on b after ↑:\n%s", got)
	}
}

func TestHistoryViewFitsTerminalWithLongSession(t *testing.T) {
	events := make([]session.Event, 0, 16)
	parent := ""
	for i := 0; i < 16; i++ {
		id := string(rune('a' + i))
		events = append(events, session.Event{
			EventID:  id,
			Type:     "message",
			ParentID: parent,
			Message: &session.Message{
				Role:    session.RoleUser,
				Content: strings.Repeat("very long history entry ", 6),
			},
		})
		parent = id
	}
	model := newHistoryModel(&fakeSessionHistory{events: events})
	model = model.SetSize(40, 8)
	model = openHistoryFromSlash(t, model)

	assertViewFits(t, model.View(), model.width, model.height)
}

func TestHistoryViewKeepsCursorVisibleWhenClipped(t *testing.T) {
	events := make([]session.Event, 0, 16)
	parent := ""
	for i := 0; i < 16; i++ {
		id := string(rune('a' + i))
		events = append(events, session.Event{
			EventID:  id,
			Type:     "message",
			ParentID: parent,
			Message: &session.Message{
				Role:    session.RoleUser,
				Content: "entry " + id,
			},
		})
		parent = id
	}
	model := newHistoryModel(&fakeSessionHistory{events: events})
	model = model.SetSize(40, 8)
	model = openHistoryFromSlash(t, model)
	for i := 0; i < 15; i++ {
		model = pressKey(model, tea.KeyUp)
	}

	view := model.View()
	if !strings.Contains(view, "▶ [user] entry a") {
		t.Fatalf("clipped history view should keep cursor visible:\n%s", view)
	}
	assertViewFits(t, view, model.width, model.height)
}

// After fork-resume the new branch accepts the next input and starts the agent
// (design test matrix §4).
func TestHistoryForkResumeAcceptsNextInput(t *testing.T) {
	fake := &fakeSessionHistory{
		events: []session.Event{
			{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
			{EventID: "b", Type: "message", ParentID: "a", Message: &session.Message{Role: session.RoleAssistant, Content: "a1"}},
		},
		newSessionID: "s-forked",
		seed:         []SeedMessage{{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"}},
	}
	model := NewModel(Config{
		Status:         Status{SessionID: "s"},
		SessionHistory: fake,
		Chat:           noopChatRunner{},
	})
	model = openHistoryFromSlash(t, model)
	model = pressKey(model, tea.KeyEnter) // fork-resume at b
	if model.cfg.Status.SessionID != "s-forked" {
		t.Fatalf("expected forked session, got %q", model.cfg.Status.SessionID)
	}

	// The forked line of thought is typed and submitted, starting the agent.
	model = model.SetInput("now try approach two")
	model, cmd := model.Submit(context.Background())
	if cmd == nil {
		t.Fatal("expected submit to start the agent after fork-resume")
	}
	msg := cmd()
	if _, ok := msg.(runAgentMsg); !ok {
		t.Fatalf("expected runAgentMsg after submit, got %T", msg)
	}
}

func TestHistoryForkResumeClearsStaleAgentFocus(t *testing.T) {
	fake := &fakeSessionHistory{
		events: []session.Event{
			{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
			{EventID: "b", Type: "message", ParentID: "a", Message: &session.Message{Role: session.RoleAssistant, Content: "a1"}},
		},
		newSessionID: "s-forked",
		seed:         []SeedMessage{{Role: "user", Content: "q1"}},
	}
	model := newHistoryModel(fake)
	model.focus = FocusAgentWindow
	model.focusedTaskID = "old-task"
	model.agentBarSelected = "old-task"
	model.subagentTraces["old-task"] = &AgentTrace{TaskID: "old-task", Status: "running"}

	model = openHistoryFromSlash(t, model)
	model = pressKey(model, tea.KeyEnter)

	if model.focus != FocusComposer {
		t.Fatalf("fork-resume should return focus to composer, got %q", model.focus)
	}
	if model.focusedTaskID != "" || model.agentBarSelected != "" {
		t.Fatalf("fork-resume should clear stale agent focus, focused=%q selected=%q", model.focusedTaskID, model.agentBarSelected)
	}
	if len(model.subagentTraces) != 0 {
		t.Fatalf("fork-resume should clear stale subagent traces, got %#v", model.subagentTraces)
	}
}

// While the agent is running, entering history is allowed (read-only) but Enter
// is disabled so the session is never mutated mid-turn (invariant 4, test
// matrix §5).
func TestHistoryReadOnlyWhileAgentBusy(t *testing.T) {
	fake := &fakeSessionHistory{
		events: []session.Event{
			{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
		},
		newSessionID: "s-forked",
	}
	model := newHistoryModel(fake)
	model.agent.Busy = true

	model = openHistoryFromSlash(t, model)
	if !model.history.visible {
		t.Fatal("entering history while busy should be allowed (read-only browse)")
	}
	model = pressKey(model, tea.KeyEnter)
	if fake.forkCalls != 0 {
		t.Fatalf("enter must be disabled while agent busy, got %d fork calls", fake.forkCalls)
	}
	if !model.history.visible {
		t.Fatal("history should remain visible since no fork happened")
	}
}

func TestCtrlHWithDraftStaysInComposer(t *testing.T) {
	fake := &fakeSessionHistory{
		events: []session.Event{
			{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
		},
	}
	model := newHistoryModel(fake)
	model = model.SetInput("draft")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	model = updated.(Model)

	if model.history.visible {
		t.Fatal("Ctrl+H with draft input should not open history")
	}
	if got := model.inputValue(); got != "draf" {
		t.Fatalf("Ctrl+H with draft input should act as composer backspace, got %q", got)
	}
}

func TestRemovedLeaderHistorySequenceTypesNormally(t *testing.T) {
	fake := &fakeSessionHistory{
		events: []session.Event{
			{EventID: "a", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
		},
	}
	model := newHistoryModel(fake)
	model = model.SetInput("draft")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	model = updated.(Model)

	if model.history.visible {
		t.Fatal("removed Ctrl+X sequence must not open history")
	}
	if got := model.inputValue(); got != "draftg" {
		t.Fatalf("rune after removed Ctrl+X route should type normally, got %q", got)
	}
	if model.leaderPending {
		t.Fatal("removed Ctrl+X route must not arm leader mode")
	}
}
