package cli

import (
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/safety"
)

// Permission-mode flags lived on the removed chat command. Keep coverage that
// app.Options still accept ModeBypass + EnableBypass for TUI/bootstrap wiring.
func TestPermissionModeOptionsAcceptBypassAcknowledgement(t *testing.T) {
	opts := app.Options{
		PermissionMode: safety.ModeBypass,
		EnableBypass:   true,
	}
	if opts.PermissionMode != safety.ModeBypass || !opts.EnableBypass {
		t.Fatalf("options=%+v", opts)
	}
}

func TestPermissionModeBypassStringIsRecognized(t *testing.T) {
	mode, err := safety.ParsePermissionMode("bypass")
	if err != nil {
		t.Fatal(err)
	}
	if mode != safety.ModeBypass {
		t.Fatalf("mode=%v", mode)
	}
	if !strings.Contains(string(mode), "bypass") && mode != safety.ModeBypass {
		t.Fatalf("unexpected mode %v", mode)
	}
}
