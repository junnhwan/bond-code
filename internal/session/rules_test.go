package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeRulesRoundTrip(t *testing.T) {
	store := NewJSONLStore(t.TempDir())

	// Missing file => nil, no error (fresh session has no grants yet).
	got, err := store.LoadRuntimeRules("s1")
	require.NoError(t, err)
	assert.Nil(t, got)

	rules := []safety.PermissionRule{
		{Tools: []string{"run_command"}, Pattern: `^go `, Decision: "allow"},
		{Tools: []string{"write_file"}, Pattern: `^internal/tui/`, Decision: "allow"},
	}
	require.NoError(t, store.SaveRuntimeRules("s1", rules))

	got, err = store.LoadRuntimeRules("s1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, rules, got)
}

func TestRuntimeRulesAddDedup(t *testing.T) {
	store := NewJSONLStore(t.TempDir())

	r1 := safety.PermissionRule{Tools: []string{"run_command"}, Pattern: `^go `, Decision: "allow"}
	r2 := safety.PermissionRule{Tools: []string{"write_file"}, Pattern: `^internal/`, Decision: "allow"}

	updated, err := store.AddRuntimeRule("s1", r1)
	require.NoError(t, err)
	assert.Len(t, updated, 1)

	// Re-granting the same rule is a no-op so the list stays bounded.
	updated, err = store.AddRuntimeRule("s1", r1)
	require.NoError(t, err)
	assert.Len(t, updated, 1)

	// A different rule appends.
	updated, err = store.AddRuntimeRule("s1", r2)
	require.NoError(t, err)
	assert.Len(t, updated, 2)
}

func TestRuntimeRulesEmptyClearsSidecar(t *testing.T) {
	store := NewJSONLStore(t.TempDir())

	require.NoError(t, store.SaveRuntimeRules("s1", []safety.PermissionRule{
		{Tools: []string{"read_file"}, Decision: "allow"},
	}))
	_, err := os.Stat(store.RulesPath("s1"))
	require.NoError(t, err)

	// Saving an empty set removes the sidecar so the session reverts to defaults.
	require.NoError(t, store.SaveRuntimeRules("s1", nil))
	_, err = os.Stat(store.RulesPath("s1"))
	assert.True(t, os.IsNotExist(err))
}

func TestRuntimeRulesRejectsBadSessionID(t *testing.T) {
	store := NewJSONLStore(t.TempDir())

	// Path traversal is rejected at the sidecar layer (same guard as meta).
	_, err := store.LoadRuntimeRules("../escape")
	require.Error(t, err)
	require.Error(t, store.SaveRuntimeRules("../escape", nil))
}

func TestRuntimeRulesJSONKeysLowercase(t *testing.T) {
	// The sidecar must use lowercase json keys matching the yaml config schema
	// so a rule round-trips identically and stays human-readable.
	dir := t.TempDir()
	store := NewJSONLStore(dir)

	require.NoError(t, store.SaveRuntimeRules("s1", []safety.PermissionRule{
		{Tools: []string{"run_command"}, Pattern: `^go `, Decision: "allow"},
	}))
	data, err := os.ReadFile(filepath.Join(dir, "s1.rules.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"tools"`)
	assert.Contains(t, string(data), `"pattern"`)
	assert.Contains(t, string(data), `"decision"`)
}
