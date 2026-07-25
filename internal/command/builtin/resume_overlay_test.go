package builtin

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

// TestResumeCommandNoArgsSignalsOverlay verifies /resume with no args asks the
// TUI to open the interactive session-manager overlay (OpenSessionManager=true)
// — the Claude-Code-style "list sessions you can pick from" flow — while still
// returning the text list in Output as a headless fallback.
func TestResumeCommandNoArgsSignalsOverlay(t *testing.T) {
	result, err := ResumeCommand().Run(context.Background(), command.Env{}, nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !result.OpenSessionManager {
		t.Fatal("expected /resume with no args to set OpenSessionManager so the TUI opens the session overlay")
	}
	if result.Output == "" {
		t.Fatal("expected the text session list in Output as a headless fallback")
	}
}
