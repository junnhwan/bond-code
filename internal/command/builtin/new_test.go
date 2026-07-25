package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

func TestNewCommandStartsFreshSession(t *testing.T) {
	called := false
	env := command.Env{
		NewSession: func() (string, error) {
			called = true
			return "session-fresh", nil
		},
	}

	result, err := NewSessionCommand().Run(context.Background(), env, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if !called {
		t.Fatal("expected NewSession callback to be called")
	}
	if result.SessionSwitched == nil || *result.SessionSwitched != "session-fresh" {
		t.Fatalf("expected SessionSwitched=session-fresh, got %#v", result.SessionSwitched)
	}
	if !strings.Contains(result.Output, "session-fresh") {
		t.Fatalf("expected output to mention fresh session, got %q", result.Output)
	}
}

func TestNewCommandWithoutCallbackReportsUnavailable(t *testing.T) {
	result, err := NewSessionCommand().Run(context.Background(), command.Env{}, nil)
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

func TestClearCommandStartsFreshSession(t *testing.T) {
	called := false
	env := command.Env{
		NewSession: func() (string, error) {
			called = true
			return "session-fresh", nil
		},
	}

	result, err := ClearSessionCommand().Run(context.Background(), env, nil)
	if err != nil {
		t.Fatalf("clear session: %v", err)
	}
	if !called {
		t.Fatal("expected NewSession callback to be called")
	}
	if result.SessionSwitched == nil || *result.SessionSwitched != "session-fresh" {
		t.Fatalf("expected SessionSwitched=session-fresh, got %#v", result.SessionSwitched)
	}
	if !strings.Contains(result.Output, "session-fresh") {
		t.Fatalf("expected output to mention fresh session, got %q", result.Output)
	}
}

func TestClearCommandWithoutCallbackReportsUnavailable(t *testing.T) {
	result, err := ClearSessionCommand().Run(context.Background(), command.Env{}, nil)
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

func TestRegisterAllIncludesClearCommand(t *testing.T) {
	registry := command.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("clear"); !ok {
		t.Fatal("expected RegisterAll to include /clear")
	}
}
