package session

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuleSourceCachesAfterAdd confirms an Add is immediately visible to the
// next RuntimeAllowRules read without touching the filesystem again — the
// cache is what keeps Policy.Decide cheap.
func TestRuleSourceCachesAfterAdd(t *testing.T) {
	store := NewJSONLStore(t.TempDir())
	src := NewRuleSource(store, "s1")

	// Fresh session: no rules yet.
	assert.Empty(t, src.RuntimeAllowRules())

	r1 := safety.PermissionRule{Tools: []string{"run_command"}, Pattern: `"command":"go `, Decision: "allow"}
	require.NoError(t, src.Add(r1))

	// The cache reflects the add immediately.
	got := src.RuntimeAllowRules()
	require.Len(t, got, 1)
	assert.Equal(t, r1, got[0])
}

// TestRuleSourceLoadsExistingSidecar confirms a source picks up rules a previous
// run persisted (source-of-truth across restarts).
func TestRuleSourceLoadsExistingSidecar(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(dir)
	require.NoError(t, store.SaveRuntimeRules("s1", []safety.PermissionRule{
		{Tools: []string{"read_file"}, Decision: "allow"},
	}))

	src := NewRuleSource(store, "s1")
	got := src.RuntimeAllowRules()
	require.Len(t, got, 1)
	assert.Equal(t, "read_file", got[0].Tools[0])
}

// TestRuleSourceResetOnSessionSwitch confirms Reset drops the cache and rebinds
// to a new session, so grants never leak across sessions.
func TestRuleSourceResetOnSessionSwitch(t *testing.T) {
	store := NewJSONLStore(t.TempDir())
	src := NewRuleSource(store, "s1")

	require.NoError(t, src.Add(safety.PermissionRule{Tools: []string{"run_command"}, Decision: "allow"}))
	require.Len(t, src.RuntimeAllowRules(), 1)

	// Switch to a new session: the grant from s1 must not be visible.
	src.Reset("s2")
	assert.Empty(t, src.RuntimeAllowRules())
	assert.Equal(t, "s2", src.SessionID())

	// The original session's rules are still on disk and reload if we switch back.
	src.Reset("s1")
	require.Len(t, src.RuntimeAllowRules(), 1)
}

// TestRuleSourceNilSafe confirms a nil source (e.g. Allow-always disabled) does
// not panic on any operation — Policy.Decide must be able to call it blindly.
func TestRuleSourceNilSafe(t *testing.T) {
	var src *RuleSource
	assert.Nil(t, src.RuntimeAllowRules())
	assert.NoError(t, src.Add(safety.PermissionRule{}))
	src.Reset("x")
	assert.Equal(t, "", src.SessionID())
}

// TestRuleSourceAsPolicyRuntimeSource confirms the end-to-end Phase 5A loop: a
// RuleSource plugged into a safety.Policy makes Decide auto-approve right after
// an Add, with no separate load — the in-memory cache bridges the grant to the
// next tool call. This is the integration the TUI's "Allow always" relies on.
func TestRuleSourceAsPolicyRuntimeSource(t *testing.T) {
	store := NewJSONLStore(t.TempDir())
	src := NewRuleSource(store, "s1")
	policy := safety.Policy{RequireConfirmation: true, RuntimeRuleSource: src}

	// Before any grant: an unclassified run_command requires confirmation.
	if d := policy.Decide("run_command", tool.RiskLevel("unclassified"), `{"command":"go test"}`); d != safety.Confirm {
		t.Fatalf("pre-grant unclassified call should confirm, got %s", d)
	}

	// Grant the "go" command family via the same path the TUI's Always uses:
	// PatternKey derives the pattern, Add persists + refreshes the cache.
	require.NoError(t, src.Add(safety.PermissionRule{
		Tools:    []string{"run_command"},
		Pattern:  safety.PatternKey("run_command", `{"command":"go test"}`),
		Decision: "allow",
	}))

	// After grant: the same family auto-approves without consulting the user.
	if d := policy.Decide("run_command", tool.RiskLevel("unclassified"), `{"command":"go build"}`); d != safety.Allow {
		t.Fatalf("post-grant same family should allow, got %s", d)
	}
	// A different command family still confirms (the grant is scoped to "go").
	if d := policy.Decide("run_command", tool.RiskLevel("unclassified"), `{"command":"ls -la"}`); d == safety.Allow {
		t.Fatalf("different family should not match the grant")
	}
}
