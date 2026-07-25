package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTempFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.txt")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func runEdit(t *testing.T, args map[string]any) (*tool.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	return NewEditFileTool().Execute(context.Background(), raw)
}

func TestEditToolReplacesUniqueString(t *testing.T) {
	path := writeTempFile(t, "hello world")
	res, err := runEdit(t, map[string]any{"path": path, "old_string": "hello", "new_string": "hi"})
	require.NoError(t, err)
	assert.True(t, res.OK)
	got, _ := os.ReadFile(path)
	assert.Equal(t, "hi world", string(got))
}

func TestEditToolErrorsWhenOldStringMissing(t *testing.T) {
	path := writeTempFile(t, "hello world")
	_, err := runEdit(t, map[string]any{"path": path, "old_string": "nope", "new_string": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestEditToolErrorsOnMultipleMatchesWithoutReplaceAll(t *testing.T) {
	path := writeTempFile(t, "dup dup dup")
	_, err := runEdit(t, map[string]any{"path": path, "old_string": "dup", "new_string": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "3 times")
}

func TestEditToolReplaceAll(t *testing.T) {
	path := writeTempFile(t, "dup dup dup")
	res, err := runEdit(t, map[string]any{"path": path, "old_string": "dup", "new_string": "x", "replace_all": true})
	require.NoError(t, err)
	assert.True(t, res.OK)
	got, _ := os.ReadFile(path)
	assert.Equal(t, "x x x", string(got))
}
