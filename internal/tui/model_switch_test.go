package tui

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

// When a slash command signals a model switch (Result.ModelSwitched), the TUI
// updates the header's model name (and the env handed to later commands) without
// rebuilding the timeline — the "switched to X" output still renders normally.
func TestSlashModelSwitchedUpdatesHeaderModel(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:       "model",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			m := "glm-4.6"
			return command.Result{Output: "switched model to glm-4.6", ModelSwitched: &m}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(Config{
		Status:   Status{Model: "glm-5.1"},
		Commands: registry,
	})

	m, _ = m.runCommand(context.Background(), "/model glm-4.6")

	if m.cfg.Status.Model != "glm-4.6" {
		t.Fatalf("expected header model glm-4.6, got %q", m.cfg.Status.Model)
	}
	if m.cfg.CommandEnv.Model != "glm-4.6" {
		t.Fatalf("expected command env model glm-4.6, got %q", m.cfg.CommandEnv.Model)
	}
}
