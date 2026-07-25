package builtin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/session"
)

func TestCompatibilityCommandSessionsDelegatesToResume(t *testing.T) {
	registry := command.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		t.Fatal(err)
	}

	sessions, ok := registry.Get("sessions")
	if !ok {
		t.Fatal("expected hidden /sessions compatibility command")
	}
	resume, ok := registry.Get("resume")
	if !ok {
		t.Fatal("expected /resume command")
	}

	env := command.Env{}
	got, err := sessions.Run(context.Background(), env, nil)
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	want, err := resume.Run(context.Background(), env, nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got.Output != want.Output || got.OpenSessionManager != want.OpenSessionManager {
		t.Fatalf("/sessions result = %#v, want /resume result %#v", got, want)
	}
}

func TestCompatibilityCommandSessionShowsCurrentSessionDetails(t *testing.T) {
	registry := command.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	cmd, ok := registry.Get("session")
	if !ok {
		t.Fatal("expected hidden /session compatibility command")
	}

	store := session.NewJSONLStore(t.TempDir())
	sessionID := "session-current"
	now := time.Now()
	for _, event := range []session.Event{
		{SessionID: sessionID, Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "question", CreatedAt: now}, CreatedAt: now},
		{SessionID: sessionID, Type: "message", Message: &session.Message{Role: session.RoleAssistant, Content: "answer", CreatedAt: now}, CreatedAt: now},
		{SessionID: sessionID, Type: "tool_result", ToolCall: &session.ToolCall{ID: "tool-1", Name: "read", CreatedAt: now}, CreatedAt: now},
	} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}

	result, err := cmd.Run(context.Background(), command.Env{Sessions: store, SessionID: sessionID}, nil)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	for _, want := range []string{"id: " + sessionID, "messages: 2 (1 user, 1 assistant)", "tool calls: 1"} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("expected %q in current-session details:\n%s", want, result.Output)
		}
	}
	if result.Panel == nil || result.Panel.Title != "session" {
		t.Fatalf("expected current-session panel, got %#v", result.Panel)
	}
	if result.OpenSessionManager {
		t.Fatal("/session must show current-session details, not the session list overlay")
	}
}

func TestCompatibilityCommandSessionStatsRemainsUnknown(t *testing.T) {
	registry := command.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("session-stats"); ok {
		t.Fatal("/session-stats must remain unknown")
	}
}
