package safety

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/tool"
)

func TestPermissionModeValidation(t *testing.T) {
	for _, mode := range []PermissionMode{ModeDefault, ModeAcceptEdits, ModePlan, ModeBypass} {
		if got, err := ParsePermissionMode(string(mode)); err != nil || got != mode {
			t.Fatalf("ParsePermissionMode(%q)=(%q,%v)", mode, got, err)
		}
	}
	if _, err := ParsePermissionMode("unsafe"); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestPlanModeBlocksMutatingTools(t *testing.T) {
	p := Policy{Mode: ModePlan}
	for _, name := range []string{tool.WriteFile, tool.EditFile, tool.RunCommand} {
		if got := p.Decide(name, tool.RiskLow, `{}`); got != Block {
			t.Fatalf("%s=%s, want block", name, got)
		}
	}
	if got := p.Decide(tool.ReadFile, tool.RiskLow, `{}`); got == Block {
		t.Fatalf("read_file unexpectedly blocked")
	}
}

func TestBypassRequiresTrustedEnableAndNeverSkipsHighRisk(t *testing.T) {
	ask := PermissionRule{Tools: []string{"demo"}, Decision: "ask"}
	if got := (Policy{Mode: ModeBypass, Rules: []PermissionRule{ask}}).Decide("demo", tool.RiskMedium, `{}`); got != Confirm {
		t.Fatalf("disabled bypass=%s", got)
	}
	p := Policy{Mode: ModeBypass, BypassEnabled: true, Rules: []PermissionRule{ask}}
	if got := p.Decide("demo", tool.RiskMedium, `{}`); got != Allow {
		t.Fatalf("enabled bypass=%s", got)
	}
	if got := p.Decide("demo", tool.RiskHigh, `{}`); got != ConfirmHigh {
		t.Fatalf("high risk=%s", got)
	}
	p.BlockedSubstrings = []string{"forbidden"}
	if got := p.Decide("demo", tool.RiskLow, `forbidden`); got != Block {
		t.Fatalf("blocked=%s", got)
	}
}

func TestChildPermissionCapabilitiesCannotEscalate(t *testing.T) {
	parent := CapabilitiesForMode(ModePlan, false)
	child := EffectiveCapabilities(parent, CapabilitiesForMode(ModeBypass, true))
	if child.Has(CapabilityMutate) || child.Has(CapabilityAutoApproveMedium) {
		t.Fatalf("child escalated: %#v", child)
	}
	if !child.Has(CapabilityRead) {
		t.Fatalf("read capability lost: %#v", child)
	}
}

func TestAcceptEditsSkipsEligibleEditConfirmationOnly(t *testing.T) {
	ask := PermissionRule{Decision: "ask"}
	p := Policy{Mode: ModeAcceptEdits, Rules: []PermissionRule{ask}}
	if got := p.Decide(tool.WriteFile, tool.RiskMedium, `{}`); got != Allow {
		t.Fatalf("write=%s", got)
	}
	if got := p.Decide(tool.RunCommand, tool.RiskMedium, `{}`); got != Confirm {
		t.Fatalf("command=%s", got)
	}
}

func TestPolicyReadsRuntimePermissionModeSource(t *testing.T) {
	source, err := NewPermissionModeSource(ModeDefault, true)
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{Mode: ModeDefault, BypassEnabled: true, RuntimeModeSource: source}
	if got := p.Decide(tool.WriteFile, tool.RiskLow, `{}`); got != Allow {
		t.Fatalf("default=%s", got)
	}
	if err := source.Set(ModePlan); err != nil {
		t.Fatal(err)
	}
	if got := p.Decide(tool.WriteFile, tool.RiskLow, `{}`); got != Block {
		t.Fatalf("plan=%s", got)
	}
	if err := source.Set(ModeBypass); err != nil {
		t.Fatal(err)
	}
	if got := p.Decide(tool.RunCommand, tool.RiskHigh, `{}`); got != ConfirmHigh {
		t.Fatalf("high risk=%s", got)
	}
}

func TestPermissionModeSourceRejectsBypassWithoutTrustedEnablement(t *testing.T) {
	source, err := NewPermissionModeSource(ModeDefault, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Set(ModeBypass); err == nil {
		t.Fatal("expected bypass rejection")
	}
	if mode := source.Mode(); mode != ModeDefault {
		t.Fatalf("mode=%s", mode)
	}
}
