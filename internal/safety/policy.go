package safety

import (
	"regexp"
	"strings"

	"github.com/junnhwan/bond-code/internal/tool"
)

type Decision string

const (
	Allow       Decision = "allow"
	Confirm     Decision = "confirm"
	ConfirmHigh Decision = "confirm_high"
	Block       Decision = "block"
)

type Policy struct {
	// Mode is the explicit user-visible permission posture. Empty means default.
	Mode PermissionMode
	// BypassEnabled records the trusted top-level acknowledgement required before
	// bypass can skip eligible low/medium-risk confirmations.
	BypassEnabled       bool
	RuntimeModeSource   *PermissionModeSource
	RequireConfirmation bool
	BlockedSubstrings   []string
	// Rules are optional configurable permission rules (roadmap C2): allow /
	// deny / ask per tool + input pattern, evaluated before the default
	// risk-based decision, with deny > ask > allow priority among matches.
	// They never override a dangerous-command hard block.
	Rules []PermissionRule
	// RuntimeRuleSource, when non-nil, supplies session-scoped allow rules
	// granted via the TUI's "Allow always" choice (Phase 5A). It is an
	// interface (not a slice) so a Policy value can read live grants without
	// being mutated: the source caches rules in memory and persists to a
	// session sidecar (session.RuleSource). Runtime rules share evaluation with
	// Rules — the same deny>ask>allow priority — so a configured deny still
	// beats a runtime allow, and neither overrides a dangerous-command hard
	// block (Block is decided before matchRule runs).
	RuntimeRuleSource RuntimeRuleSource
}

// PermissionRule is a configurable allow/deny/ask rule. Tools empty = all tools;
// Pattern empty = match any input (regex otherwise); Decision is allow|deny|ask.
// The json tags mirror the yaml schema so a rule round-trips identically
// whether it is loaded from config yaml or persisted to a session's runtime
// rules sidecar (Phase 5A Allow-always).
type PermissionRule struct {
	Tools    []string `yaml:"tools" json:"tools"`
	Pattern  string   `yaml:"pattern" json:"pattern"`
	Decision string   `yaml:"decision" json:"decision"`
	Reason   string   `yaml:"reason" json:"reason,omitempty"`
}

// RuntimeRuleSource is the read side of Phase 5A Allow-always: it returns the
// current session-scoped allow rules. The write side lives in the TUI/app layer
// (session.RuleSource.Add). Policy.Decide consults it on every call so a
// freshly granted rule takes effect immediately for the next tool call.
type RuntimeRuleSource interface {
	RuntimeAllowRules() []PermissionRule
}

func (p Policy) Decide(toolName string, risk tool.RiskLevel, rawInput string) Decision {
	if dangerousCommandFromRawInput(toolName, rawInput) != "" {
		return Block
	}
	for _, blocked := range p.BlockedSubstrings {
		if blocked != "" && strings.Contains(rawInput, blocked) {
			return Block
		}
	}
	mode, err := ParsePermissionMode(string(p.EffectiveMode()))
	if err != nil {
		return Block
	}
	if mode == ModePlan && isPlanMutation(toolName) {
		return Block
	}
	// High-risk confirmation is a hard boundary: configured/runtime allows,
	// --yes, bypass, and child inheritance cannot auto-approve it.
	if risk == tool.RiskHigh {
		return ConfirmHigh
	}
	// C2: configurable permission rules override the default risk-based decision.
	if d, ok := p.matchRule(toolName, rawInput); ok {
		if d == Confirm && ((mode == ModeBypass && p.EffectiveBypassEnabled()) ||
			(mode == ModeAcceptEdits && (toolName == tool.WriteFile || toolName == tool.EditFile))) {
			return Allow
		}
		return d
	}
	if !p.RequireConfirmation && risk == tool.RiskLow {
		return Allow
	}
	switch risk {
	case tool.RiskLow, tool.RiskMedium:
		// Low and medium risk (file edits, memory, todos, ordinary commands)
		// run automatically; only high-risk destructive actions ask.
		return Allow
	case tool.RiskHigh:
		return ConfirmHigh
	default:
		return Confirm
	}
}

// allRules returns the combined configured + runtime rule set. Configured Rules
// (static yaml) and runtime rules from RuntimeRuleSource (session Allow-always
// grants) share one evaluation pass; deny>ask>allow priority applies across
// both, so a deny in either set always wins and an allow in either
// auto-approves. Neither can override a dangerous-command hard block, which is
// decided before matchRule.
func (p Policy) allRules() []PermissionRule {
	out := make([]PermissionRule, 0, len(p.Rules))
	out = append(out, p.Rules...)
	if p.RuntimeRuleSource != nil {
		out = append(out, p.RuntimeRuleSource.RuntimeAllowRules()...)
	}
	return out
}

// matchRule evaluates configured and runtime rules and returns the winning
// decision by deny (Block) > ask (Confirm) > allow (Allow) priority. Returns
// false if no rule matches.
func (p Policy) matchRule(toolName, rawInput string) (Decision, bool) {
	var matched []Decision
	for _, r := range p.allRules() {
		if !r.matchesTool(toolName) || !r.matchesPattern(rawInput) {
			continue
		}
		if d := r.decision(); d != "" {
			matched = append(matched, d)
		}
	}
	for _, prio := range []Decision{Block, Confirm, Allow} {
		for _, d := range matched {
			if d == prio {
				return prio, true
			}
		}
	}
	return "", false
}

func (r PermissionRule) matchesTool(toolName string) bool {
	if len(r.Tools) == 0 {
		return true
	}
	for _, t := range r.Tools {
		if t == toolName {
			return true
		}
	}
	return false
}

func (r PermissionRule) matchesPattern(rawInput string) bool {
	if strings.TrimSpace(r.Pattern) == "" {
		return true
	}
	matched, err := regexp.MatchString(r.Pattern, rawInput)
	if err != nil {
		return false
	}
	return matched
}

func (r PermissionRule) decision() Decision {
	switch strings.ToLower(strings.TrimSpace(r.Decision)) {
	case "allow":
		return Allow
	case "deny":
		return Block
	case "ask":
		return Confirm
	default:
		return ""
	}
}

func (p Policy) EffectiveMode() PermissionMode {
	if p.RuntimeModeSource != nil {
		return p.RuntimeModeSource.Mode()
	}
	mode, err := ParsePermissionMode(string(p.Mode))
	if err != nil {
		return ""
	}
	return mode
}
func (p Policy) EffectiveBypassEnabled() bool {
	if p.RuntimeModeSource != nil {
		return p.RuntimeModeSource.BypassEnabled()
	}
	return p.BypassEnabled
}
