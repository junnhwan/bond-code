package app

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/todo"
)

func TestBootstrapResumeRestoresHistory(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)

	app1, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true})
	if err != nil {
		t.Fatalf("bootstrap app1: %v", err)
	}
	if _, err := app1.Chat(context.Background(), "remember the secret word: zebra"); err != nil {
		t.Fatalf("app1 chat: %v", err)
	}
	wantHistory := app1.History()
	if len(wantHistory) < 2 {
		t.Fatalf("expected app1 to have history after a turn, got %d messages", len(wantHistory))
	}

	// A second process resuming the same session must reload that history.
	app2, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true, ResumeSessionID: app1.SessionID})
	if err != nil {
		t.Fatalf("bootstrap app2 with resume: %v", err)
	}
	gotHistory := app2.History()
	if len(gotHistory) != len(wantHistory) {
		t.Fatalf("expected resumed history length %d, got %d", len(wantHistory), len(gotHistory))
	}
	// The resumed conversation keeps the user prompt and assistant reply.
	joined := ""
	for _, msg := range gotHistory {
		joined += msg.Content + "\n"
	}
	if !strings.Contains(joined, "remember the secret word: zebra") {
		t.Fatalf("resumed history missing user prompt:\n%s", joined)
	}

	// Continuation appends to the resumed session's snapshot, not a new one.
	if _, err := app2.Chat(context.Background(), "follow up"); err != nil {
		t.Fatalf("app2 follow-up chat: %v", err)
	}
	if len(app2.History()) <= len(wantHistory) {
		t.Fatalf("expected follow-up to extend resumed history, got %d", len(app2.History()))
	}
}

func TestBootstrapScopesTodosToSessionAndResumeRestoresThem(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)

	app1, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true})
	if err != nil {
		t.Fatalf("bootstrap app1: %v", err)
	}
	if _, err := app1.Chat(context.Background(), "seed first session"); err != nil {
		t.Fatalf("seed app1 session: %v", err)
	}
	if err := app1.TaskStore.ReplaceAll([]todo.Task{{
		ID: "todo_001", Subject: "first session task", Status: todo.TaskStatusInProgress,
	}}); err != nil {
		t.Fatalf("write app1 todo: %v", err)
	}

	app2, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true})
	if err != nil {
		t.Fatalf("bootstrap app2: %v", err)
	}
	if app2.SessionID == app1.SessionID {
		t.Fatalf("expected distinct session ids, both were %q", app1.SessionID)
	}
	app2Tasks, err := app2.TaskStore.List()
	if err != nil {
		t.Fatalf("list app2 todo: %v", err)
	}
	if len(app2Tasks) != 0 {
		t.Fatalf("new session inherited another session todo: %#v", app2Tasks)
	}

	resumed, err := Bootstrap(Options{
		ConfigPath: configPath, UseFakeLLM: true, ResumeSessionID: app1.SessionID,
	})
	if err != nil {
		t.Fatalf("resume app1: %v", err)
	}
	resumedTasks, err := resumed.TaskStore.List()
	if err != nil {
		t.Fatalf("list resumed todo: %v", err)
	}
	if len(resumedTasks) != 1 || resumedTasks[0].Subject != "first session task" {
		t.Fatalf("resumed session did not restore its todo: %#v", resumedTasks)
	}
}

func TestBootstrapResumeUnknownIDErrors(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)

	_, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true, ResumeSessionID: "nope-not-a-session"})
	if err == nil {
		t.Fatal("expected error resuming an unknown session id")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestBootstrapResumeCorruptHistoryFallsBackToEmptyHistory(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)

	app1, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true})
	if err != nil {
		t.Fatalf("bootstrap app1: %v", err)
	}
	if _, err := app1.Chat(context.Background(), "create a resumable session"); err != nil {
		t.Fatalf("app1 chat: %v", err)
	}
	if err := app1.Sessions.WriteHistory(app1.SessionID, []byte(`{"not":"a message list"`)); err != nil {
		t.Fatalf("write corrupt history: %v", err)
	}

	app2, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true, ResumeSessionID: app1.SessionID})
	if err != nil {
		t.Fatalf("bootstrap should tolerate corrupt resume history: %v", err)
	}
	if got := app2.History(); len(got) != 0 {
		t.Fatalf("expected empty history after corrupt snapshot fallback, got %#v", got)
	}
	if warning := app2.StatusSnapshot().Context.Stats; !strings.Contains(warning, "history snapshot warning") || !strings.Contains(warning, app1.SessionID) {
		t.Fatalf("expected corrupt snapshot warning in status, got %q", warning)
	}
}

func TestSaveLoadHistorySnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	app := &App{
		Sessions:  store,
		SessionID: "session-snap",
		history: []llm.Message{
			{Role: llm.RoleSystem, Content: "sys"},
			{Role: llm.RoleUser, Content: "hello"},
			{Role: llm.RoleAssistant, Content: "world"},
		},
	}
	if err := app.saveHistorySnapshot(); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded := &App{Sessions: store, SessionID: "session-snap"}
	if err := loaded.loadHistorySnapshot("session-snap"); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.history) != len(app.history) {
		t.Fatalf("expected %d messages, got %d", len(app.history), len(loaded.history))
	}
	if loaded.history[1].Content != "hello" {
		t.Fatalf("expected user message preserved, got %#v", loaded.history[1])
	}
}
