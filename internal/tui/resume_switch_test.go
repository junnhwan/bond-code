package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/command"
	commandbuiltin "github.com/junnhwan/bond-code/internal/command/builtin"
)

// When a slash command signals a session switch (Result.SessionSwitched), the
// TUI rebuilds its timeline from the app's freshly-switched history via
// ReloadSessionSeed, tracks the new id in header + live, and appends no
// command block — the switched-to conversation is what the user should see
// (design §4.4, §6).
func TestSlashSessionSwitchedReseedsTimelineAndStatus(t *testing.T) {
	switchedTo := "session-switched"
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:       "resume",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			s := switchedTo
			return command.Result{Output: "switched", SessionSwitched: &s}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{
		Commands:   registry,
		CommandEnv: command.Env{SessionID: "session-old", ProjectRoot: "."},
		ReloadSessionSeed: func(sessionID string) []SeedMessage {
			if sessionID != switchedTo {
				t.Fatalf("expected seed requested for %q, got %q", switchedTo, sessionID)
			}
			return []SeedMessage{
				{Role: "user", Content: "resumed question"},
				{Role: "assistant", Content: "resumed answer"},
			}
		},
	})
	model = model.SetInput("/resume " + switchedTo)

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("session switch should resolve synchronously in the model")
	}
	// Timeline reseeded from the target session, not the slash command output.
	turns := next.timeline.Turns
	if len(turns) != 1 || turns[0].User.Body != "resumed question" {
		t.Fatalf("expected timeline reseeded from target session, got %#v", turns)
	}
	if len(turns[0].Blocks) != 1 || turns[0].Blocks[0].Kind != BlockAssistant || turns[0].Blocks[0].Body != "resumed answer" {
		t.Fatalf("expected resumed assistant block, got %#v", turns[0].Blocks)
	}
	// Session id tracked in both header (cfg.Status) and live.
	if next.cfg.Status.SessionID != switchedTo {
		t.Fatalf("expected status SessionID=%q, got %q", switchedTo, next.cfg.Status.SessionID)
	}
	if next.live.SessionID != switchedTo {
		t.Fatalf("expected live SessionID=%q, got %q", switchedTo, next.live.SessionID)
	}
	// No command block: the slash command itself is dropped on switch.
	for _, turn := range next.timeline.Turns {
		for _, block := range turn.Blocks {
			if block.Kind == BlockCommand {
				t.Fatalf("expected no command block after switch, got %#v", block)
			}
		}
	}
}

func TestSlashSessionSwitchedResetsAgentFocus(t *testing.T) {
	switchedTo := "session-switched"
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:       "resume",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{Output: "switched", SessionSwitched: &switchedTo}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{
		Commands: registry,
		Status:   Status{SessionID: "session-old"},
		ReloadSessionSeed: func(sessionID string) []SeedMessage {
			return []SeedMessage{{Role: "user", Content: "resumed question"}}
		},
	})
	model.focus = FocusAgentWindow
	model.focusedTaskID = "old-task"
	model.agentBarSelected = "old-task"
	model.subagentTraces["old-task"] = &AgentTrace{TaskID: "old-task", Status: "running"}
	model = model.SetInput("/resume " + switchedTo)

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("session switch should resolve synchronously in the model")
	}

	if next.focus != FocusComposer {
		t.Fatalf("session switch should return focus to composer, got %q", next.focus)
	}
	if next.focusedTaskID != "" || next.agentBarSelected != "" {
		t.Fatalf("session switch should clear stale agent focus, focused=%q selected=%q", next.focusedTaskID, next.agentBarSelected)
	}
	if len(next.subagentTraces) != 0 {
		t.Fatalf("session switch should clear stale subagent traces, got %#v", next.subagentTraces)
	}
}

func TestSlashSessionSwitchedUpdatesCommandEnvSessionID(t *testing.T) {
	switchedTo := "session-switched"
	var statusSession string
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:       "resume",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{Output: "switched", SessionSwitched: &switchedTo}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(command.Command{
		Name:       "status",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			statusSession = env.SessionID
			return command.Result{Output: env.SessionID}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{
		Commands:   registry,
		CommandEnv: command.Env{SessionID: "session-old"},
		Status:     Status{SessionID: "session-old"},
		ReloadSessionSeed: func(sessionID string) []SeedMessage {
			return []SeedMessage{{Role: "user", Content: "resumed question"}}
		},
	})
	model = model.SetInput("/resume " + switchedTo)

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("session switch should resolve synchronously in the model")
	}
	next = next.SetInput("/status")
	_, cmd = next.Submit(context.Background())
	if cmd != nil {
		t.Fatal("status command should resolve synchronously in the model")
	}

	if statusSession != switchedTo {
		t.Fatalf("expected follow-up commands to see SessionID=%q, got %q", switchedTo, statusSession)
	}
}

func TestBusySessionSwitchSignalDoesNotSwapSession(t *testing.T) {
	switchedTo := "session-switched"
	reloadCalled := false
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:       "resume",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{Output: "switched", SessionSwitched: &switchedTo}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{
		Commands: registry,
		Status:   Status{SessionID: "session-old"},
		ReloadSessionSeed: func(sessionID string) []SeedMessage {
			reloadCalled = true
			return []SeedMessage{{Role: "user", Content: "new session prompt"}}
		},
	})
	model.timeline = model.timeline.StartUserTurn("running prompt")
	model.agent.Busy = true
	model = model.SetInput("/resume " + switchedTo)

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("busy session switch should resolve synchronously in the model")
	}

	if reloadCalled {
		t.Fatal("busy session switch should not reload another session while the current stream is running")
	}
	if next.cfg.Status.SessionID != "session-old" || next.live.SessionID != "session-old" {
		t.Fatalf("busy session switch should keep current session, got status=%q live=%q", next.cfg.Status.SessionID, next.live.SessionID)
	}
	if !next.agent.Busy {
		t.Fatal("busy session switch should leave the running agent active")
	}
	if len(next.timeline.Turns) != 1 || next.timeline.Turns[0].User.Body != "running prompt" {
		t.Fatalf("busy session switch should preserve the active timeline, got %#v", next.timeline.Turns)
	}
}

// When ReloadSessionSeed is nil (headless-style config), a SessionSwitched
// signal must not crash — it falls through to the normal output rendering so
// the user still sees something (defensive degradation).
func TestSlashSessionSwitchedNilSeedFallsBackToOutput(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:       "resume",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			s := "x"
			return command.Result{Output: "switched to x", SessionSwitched: &s}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Commands: registry, CommandEnv: command.Env{SessionID: "s"}})
	model = model.SetInput("/resume x")

	next, _ := model.Submit(context.Background())
	block, ok := latestTimelineBlock(next.timeline, BlockCommand)
	if !ok || !strings.Contains(block.Body, "switched to x") {
		t.Fatalf("expected fallback output rendering when no seed callback, got %#v", next.timeline)
	}
}

func TestSlashNewSessionSwitchClearsTimelineAndStatus(t *testing.T) {
	registry := command.NewRegistry()
	freshID := "session-fresh"
	if err := registry.Register(command.Command{
		Name:       "new",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{Output: "started new session " + freshID, SessionSwitched: &freshID}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{
		Commands: registry,
		Status:   Status{SessionID: "session-old"},
		ReloadSessionSeed: func(sessionID string) []SeedMessage {
			if sessionID != freshID {
				t.Fatalf("expected seed requested for %q, got %q", freshID, sessionID)
			}
			return nil
		},
	})
	model.timeline = model.timeline.StartUserTurn("old prompt")
	model = model.SetInput("/new")

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("/new should switch synchronously")
	}

	if next.cfg.Status.SessionID != freshID || next.live.SessionID != freshID {
		t.Fatalf("expected fresh session id in status/live, got status=%q live=%q", next.cfg.Status.SessionID, next.live.SessionID)
	}
	if len(next.timeline.Turns) != 0 {
		t.Fatalf("expected fresh session to clear timeline, got turns=%#v", next.timeline.Turns)
	}
}

func TestSlashClearSessionSwitchClearsTimelineAndStatus(t *testing.T) {
	registry := command.NewRegistry()
	freshID := "session-fresh"
	if err := commandbuiltin.RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{
		Commands: registry,
		CommandEnv: command.Env{
			SessionID: "session-old",
			NewSession: func() (string, error) {
				return freshID, nil
			},
		},
		Status: Status{SessionID: "session-old"},
		ReloadSessionSeed: func(sessionID string) []SeedMessage {
			if sessionID != freshID {
				t.Fatalf("expected seed requested for %q, got %q", freshID, sessionID)
			}
			return nil
		},
	})
	model.timeline = model.timeline.StartUserTurn("old prompt")
	model = model.SetInput("/clear")

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("/clear should switch synchronously")
	}

	if next.cfg.Status.SessionID != freshID || next.live.SessionID != freshID {
		t.Fatalf("expected fresh session id in status/live, got status=%q live=%q", next.cfg.Status.SessionID, next.live.SessionID)
	}
	if len(next.timeline.Turns) != 0 {
		t.Fatalf("expected fresh session to clear timeline, got turns=%#v", next.timeline.Turns)
	}
}

func TestRemovedLeaderNewSequenceTypesNormally(t *testing.T) {
	registry := command.NewRegistry()
	freshID := "session-fresh"
	called := false
	if err := registry.Register(command.Command{
		Name:       "new",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			called = true
			return command.Result{Output: "started new session " + freshID, SessionSwitched: &freshID}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{
		Commands: registry,
		Status:   Status{SessionID: "session-old"},
		ReloadSessionSeed: func(sessionID string) []SeedMessage {
			return nil
		},
	})
	model = model.SetInput("draft")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model = updated.(Model)

	if called {
		t.Fatal("removed Ctrl+X sequence must not start a new session")
	}
	if model.cfg.Status.SessionID != "session-old" || model.live.SessionID != "session-old" {
		t.Fatalf("removed Ctrl+X sequence should keep current session, got status=%q live=%q", model.cfg.Status.SessionID, model.live.SessionID)
	}
	if got := model.inputValue(); got != "draftn" {
		t.Fatalf("rune after removed Ctrl+X route should type normally, got %q", got)
	}
	if model.leaderPending {
		t.Fatal("removed Ctrl+X route must not arm leader mode")
	}
}
