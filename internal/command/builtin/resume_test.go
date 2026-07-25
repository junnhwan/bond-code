package builtin

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/session"
)

func TestResumeCommandNoArgsListsSessionsWithAge(t *testing.T) {
	store := session.NewJSONLStore(t.TempDir())
	older := "session-20260101-000000.000000000"
	newer := "session-20260102-000000.000000000"
	mustAppend := func(id, content string) {
		if err := store.Append(session.Event{
			SessionID: id,
			Type:      "message",
			Message:   &session.Message{Role: session.RoleUser, Content: content, CreatedAt: time.Now()},
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	mustAppend(older, "older preview text")
	mustAppend(newer, "newer preview text")

	result, err := ResumeCommand().Run(context.Background(), command.Env{Sessions: store, SessionID: newer}, nil)
	if err != nil {
		t.Fatalf("resume list: %v", err)
	}
	out := result.Output
	if !strings.Contains(out, newer) || !strings.Contains(out, "newer preview text") {
		t.Fatalf("expected newest session and its preview, got:\n%s", out)
	}
	if !strings.Contains(out, "resume with: /resume <id>") {
		t.Fatalf("expected in-app resume footer, got:\n%s", out)
	}
	// Relative age label is present (sessions just appended → "just now").
	if !strings.Contains(out, "just now") {
		t.Fatalf("expected relative age label, got:\n%s", out)
	}
	// Newest first.
	if strings.Index(out, newer) >= strings.Index(out, older) {
		t.Fatalf("expected newest session listed first, got:\n%s", out)
	}
	if result.SessionSwitched != nil {
		t.Fatalf("list must not signal a switch, got %q", *result.SessionSwitched)
	}
}

func TestResumeCommandHidesEmptyAndIgnoresDebugJSONL(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	withMsgs := "session-20260103-000000.000000000"
	empty := "session-20260104-000000.000000000"
	if err := store.Append(session.Event{
		SessionID: withMsgs,
		Type:      "message",
		Message:   &session.Message{Role: session.RoleUser, Content: "keep me", CreatedAt: time.Now()},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// Empty audit log (would previously show as "(no messages)").
	if err := store.Append(session.Event{SessionID: empty, Type: "message"}); err != nil {
		t.Fatal(err)
	}
	// Debug sidecar must not appear as its own session row.
	if err := os.WriteFile(store.DebugPath(withMsgs), []byte(`{"type":"llm_req"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ResumeCommand().Run(context.Background(), command.Env{
		Sessions:  store,
		SessionID: withMsgs,
	}, nil)
	if err != nil {
		t.Fatalf("resume list: %v", err)
	}
	out := result.Output
	if !strings.Contains(out, withMsgs) || !strings.Contains(out, "keep me") {
		t.Fatalf("expected real session in list, got:\n%s", out)
	}
	if strings.Contains(out, empty) {
		t.Fatalf("empty abandoned session should be hidden, got:\n%s", out)
	}
	if strings.Contains(out, ".debug") {
		t.Fatalf("debug sidecar must not list as a session, got:\n%s", out)
	}
}

func TestResumeCommandWithIDSetsSessionSwitched(t *testing.T) {
	store := session.NewJSONLStore(t.TempDir())
	target := "session-target"

	called := ""
	env := command.Env{
		Sessions: store,
		SwitchSession: func(id string) error {
			called = id
			return nil
		},
	}
	result, err := ResumeCommand().Run(context.Background(), env, []string{target})
	if err != nil {
		t.Fatalf("resume <id>: %v", err)
	}
	if called != target {
		t.Fatalf("expected SwitchSession called with %q, got %q", target, called)
	}
	if result.SessionSwitched == nil || *result.SessionSwitched != target {
		t.Fatalf("expected SessionSwitched=%q, got %#v", target, result.SessionSwitched)
	}
	if !strings.Contains(result.Output, target) {
		t.Fatalf("expected output to mention target id, got %q", result.Output)
	}
}

func TestResumeCommandWithoutCallbackReportsUnavailable(t *testing.T) {
	store := session.NewJSONLStore(t.TempDir())
	result, err := ResumeCommand().Run(context.Background(), command.Env{Sessions: store}, []string{"some-id"})
	if err != nil {
		t.Fatalf("expected graceful message, got error: %v", err)
	}
	if !strings.Contains(result.Output, "not available") {
		t.Fatalf("expected unavailable message, got %q", result.Output)
	}
	if result.SessionSwitched != nil {
		t.Fatalf("must not signal a switch when unavailable, got %q", *result.SessionSwitched)
	}
}

func TestResumeCommandSwitchErrorPropagates(t *testing.T) {
	store := session.NewJSONLStore(t.TempDir())
	want := errors.New("boom")
	env := command.Env{
		Sessions:      store,
		SwitchSession: func(id string) error { return want },
	}
	_, err := ResumeCommand().Run(context.Background(), env, []string{"x"})
	if !errors.Is(err, want) {
		t.Fatalf("expected switch error to propagate, got %v", err)
	}
}
