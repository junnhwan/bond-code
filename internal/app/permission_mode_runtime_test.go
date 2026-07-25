package app

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/tool"
)

func TestSetPermissionModeUpdatesSharedPolicyAndAuditsTransition(t *testing.T) {
	source, err := safety.NewPermissionModeSource(safety.ModeDefault, true)
	if err != nil {
		t.Fatal(err)
	}
	store := session.NewJSONLStore(t.TempDir())
	a := &App{Sessions: store, SessionID: "session-a", Policy: safety.Policy{Mode: safety.ModeDefault, BypassEnabled: true, RuntimeModeSource: source}}
	if err := a.SetPermissionMode("plan"); err != nil {
		t.Fatal(err)
	}
	if got := a.PermissionMode(); got != safety.ModePlan {
		t.Fatalf("mode=%s", got)
	}
	if got := a.Policy.Decide("write_file", tool.RiskLow, `{}`); got != safety.Block {
		t.Fatalf("decision=%s", got)
	}
	events, err := store.Load("session-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "permission_mode_changed" || events[0].AgentEvent == nil || events[0].AgentEvent.Input != "default" || events[0].AgentEvent.Output != "plan" {
		t.Fatalf("events=%#v", events)
	}
}

func TestSetPermissionModeDoesNotChangeModeWhenAuditFails(t *testing.T) {
	source, _ := safety.NewPermissionModeSource(safety.ModeDefault, false)
	a := &App{Sessions: session.NewJSONLStore(t.TempDir()), SessionID: "../invalid", Policy: safety.Policy{Mode: safety.ModeDefault, RuntimeModeSource: source}}
	if err := a.SetPermissionMode("plan"); err == nil {
		t.Fatal("expected audit failure")
	}
	if got := a.PermissionMode(); got != safety.ModeDefault {
		t.Fatalf("mode=%s", got)
	}
}
