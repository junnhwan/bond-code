package collaboration

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/safety"
)

func TestResolveChildPermissionModePreventsEscalation(t *testing.T) {
	if _, err := resolveChildPermissionMode("bypass", safety.ModeDefault, false); err == nil {
		t.Fatal("expected bypass escalation rejection")
	}
	if got, err := resolveChildPermissionMode("plan", safety.ModeDefault, false); err != nil || got != safety.ModePlan {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if got, err := resolveChildPermissionMode("", safety.ModeAcceptEdits, false); err != nil || got != safety.ModeAcceptEdits {
		t.Fatalf("inherit got=%q err=%v", got, err)
	}
}
