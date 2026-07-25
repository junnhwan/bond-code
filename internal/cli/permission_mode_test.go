package cli

import (
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/safety"
)

func TestChatBypassRequiresExplicitAcknowledgement(t *testing.T) {
	called := false
	cmd := newChatCommandWithBootstrapAndTUI(func(app.Options) (*app.App, error) { called = true; return nil, nil }, nil)
	cmd.SetArgs([]string{"--once", "--permission-mode", "bypass", "hello"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "enable-bypass") {
		t.Fatalf("error=%v", err)
	}
	if called {
		t.Fatal("bootstrap called without bypass acknowledgement")
	}
}

func TestChatPassesPermissionModeAndBypassAcknowledgement(t *testing.T) {
	var got app.Options
	cmd := newChatCommandWithBootstrapAndTUI(func(opts app.Options) (*app.App, error) { got = opts; return nil, assertStop{} }, nil)
	cmd.SetArgs([]string{"--once", "--permission-mode", "bypass", "--enable-bypass", "hello"})
	_ = cmd.Execute()
	if got.PermissionMode != safety.ModeBypass || !got.EnableBypass {
		t.Fatalf("options=%+v", got)
	}
}

type assertStop struct{}

func (assertStop) Error() string { return "stop after bootstrap" }
