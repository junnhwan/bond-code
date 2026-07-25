package safety

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/tool"
)

// testRuleSource is a minimal RuntimeRuleSource for policy tests: it returns a
// fixed slice of allow rules, standing in for the session.RuleSource the TUI
// populates at runtime.
type testRuleSource []PermissionRule

func (s testRuleSource) RuntimeAllowRules() []PermissionRule { return s }

// TestPolicyRuntimeRulesAllow confirms a session-scoped allow grant (Phase 5A
// "Allow always") auto-approves a tool that the risk default would otherwise
// only confirm.
func TestPolicyRuntimeRulesAllow(t *testing.T) {
	p := Policy{RuntimeRuleSource: testRuleSource{
		{Tools: []string{"run_command"}, Pattern: `"command":"go `, Decision: "allow"},
	}}
	if d := p.Decide("run_command", tool.RiskMedium, `{"command":"go test ./..."}`); d != Allow {
		t.Fatalf("runtime allow rule should auto-approve, got %s", d)
	}
}

// TestPolicyRuntimeRulesCannotOverrideDangerousCommand confirms the hard Block
// on dangerous commands is decided before runtime rules, so a user cannot
// Allow-always their way past `sudo rm` and friends.
func TestPolicyRuntimeRulesCannotOverrideDangerousCommand(t *testing.T) {
	p := Policy{RuntimeRuleSource: testRuleSource{
		{Tools: []string{"run_command"}, Decision: "allow"},
	}}
	if d := p.Decide("run_command", tool.RiskLow, `{"command":"sudo rm x"}`); d != Block {
		t.Fatalf("dangerous command must block despite runtime allow, got %s", d)
	}
}

// TestPolicyConfiguredDenyBeatsRuntimeAllow confirms a static config deny rule
// still wins over a session runtime allow (deny>ask>allow across both sets).
func TestPolicyConfiguredDenyBeatsRuntimeAllow(t *testing.T) {
	p := Policy{
		Rules: []PermissionRule{
			{Tools: []string{"run_command"}, Pattern: "rm", Decision: "deny"},
		},
		RuntimeRuleSource: testRuleSource{
			{Tools: []string{"run_command"}, Decision: "allow"},
		},
	}
	if d := p.Decide("run_command", tool.RiskLow, `{"command":"rm -rf x"}`); d != Block {
		t.Fatalf("configured deny should beat runtime allow, got %s", d)
	}
}

// TestPolicyRuntimeRulesEmptyToolsMatchesAll confirms a runtime rule with no
// Tools entry matches every tool (same semantics as configured rules).
func TestPolicyRuntimeRulesEmptyToolsMatchesAll(t *testing.T) {
	p := Policy{RuntimeRuleSource: testRuleSource{
		{Decision: "ask"},
	}}
	if d := p.Decide("edit_file", tool.RiskMedium, `{"path":"x"}`); d != Confirm {
		t.Fatalf("empty-tools runtime rule should match all, got %s", d)
	}
}

// TestPolicyRuntimeAndConfiguredRulesBothEvaluated confirms both rule sets feed
// the same priority resolution: a config allow and a runtime allow each fire
// for their own tool.
func TestPolicyRuntimeAndConfiguredRulesBothEvaluated(t *testing.T) {
	p := Policy{
		Rules: []PermissionRule{
			{Tools: []string{"read_file"}, Decision: "allow"},
		},
		RuntimeRuleSource: testRuleSource{
			{Tools: []string{"write_file"}, Decision: "allow"},
		},
	}
	if d := p.Decide("read_file", tool.RiskMedium, `{}`); d != Allow {
		t.Fatalf("configured allow should fire, got %s", d)
	}
	if d := p.Decide("write_file", tool.RiskMedium, `{}`); d != Allow {
		t.Fatalf("runtime allow should fire, got %s", d)
	}
}
