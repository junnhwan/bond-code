package safety

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/tool"
)

func TestPolicyRulesAllowOverrides(t *testing.T) {
	p := Policy{Rules: []PermissionRule{
		{Tools: []string{"read_file"}, Decision: "allow"},
	}}
	// read_file with allow rule overrides a high-risk default.
	if d := p.Decide("read_file", tool.RiskMedium, `{"path":"x"}`); d != Allow {
		t.Fatalf("allow rule should override, got %s", d)
	}
}

func TestPolicyRulesDenyBeatsAllow(t *testing.T) {
	p := Policy{Rules: []PermissionRule{
		{Tools: []string{"run_command"}, Decision: "allow"},
		{Tools: []string{"run_command"}, Pattern: "rm", Decision: "deny"},
	}}
	if d := p.Decide("run_command", tool.RiskLow, `{"command":"rm -rf x"}`); d != Block {
		t.Fatalf("deny should beat allow, got %s", d)
	}
	if d := p.Decide("run_command", tool.RiskLow, `{"command":"ls"}`); d != Allow {
		t.Fatalf("non-matching deny should allow, got %s", d)
	}
}

func TestPolicyRulesAskForcesConfirm(t *testing.T) {
	p := Policy{Rules: []PermissionRule{
		{Tools: []string{"edit_file"}, Decision: "ask"},
	}}
	// edit_file normally Allow (medium risk), but ask rule forces Confirm.
	if d := p.Decide("edit_file", tool.RiskMedium, `{"path":"x"}`); d != Confirm {
		t.Fatalf("ask rule should force confirm, got %s", d)
	}
}

func TestPolicyRulesNoMatchFallsBackToRisk(t *testing.T) {
	p := Policy{Rules: []PermissionRule{
		{Tools: []string{"read_file"}, Decision: "allow"},
	}}
	// write_file not in rules -> default risk-based (medium -> Allow).
	if d := p.Decide("write_file", tool.RiskMedium, `{"path":"x"}`); d != Allow {
		t.Fatalf("no rule match should fall back to risk default, got %s", d)
	}
}

func TestPolicyRulesCannotOverrideDangerousCommand(t *testing.T) {
	p := Policy{Rules: []PermissionRule{
		{Tools: []string{"run_command"}, Decision: "allow"},
	}}
	// sudo is dangerous -> hard Block; an allow rule cannot override it.
	if d := p.Decide("run_command", tool.RiskLow, `{"command":"sudo rm x"}`); d != Block {
		t.Fatalf("dangerous command must block despite allow rule, got %s", d)
	}
}
