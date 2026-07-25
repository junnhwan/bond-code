package subagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/tool/builtin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunTaskCoderWritesFileViaEdit is the end-to-end proof that the coder
// subagent can really mutate the workspace: a fake model delegates an edit in
// its first turn and returns a final answer in the second; the real builtin
// edit tool must change the file and the task must complete.
func TestRunTaskCoderWritesFileViaEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0o600))

	registry := tool.NewRegistry()
	require.NoError(t, registry.Register(builtin.NewReadFileTool()))
	require.NoError(t, registry.Register(builtin.NewEditFileTool()))

	editArgs, err := json.Marshal(map[string]string{
		"path":       path,
		"old_string": "hello",
		"new_string": "hi",
	})
	require.NoError(t, err)

	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "edit_file", Arguments: string(editArgs)}}},
		{{Content: "changed hello to hi in " + path, Done: true}},
	})
	manager := newTestManager(client, registry)

	result, err := manager.RunTask(context.Background(), TaskRequest{
		Description:  "greet",
		Prompt:       "Change hello to hi in " + path,
		SubagentType: AgentTypeCoder,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "completed", result.Status)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hi world", string(got))
}

// TestCoderProfileExposesWriteTools confirms the coder profile surfaces
// edit/write_file plus run_command (for test/build verification) to the child
// registry. High-risk commands remain gated by safety.Policy + Confirmer.
func TestCoderProfileExposesWriteTools(t *testing.T) {
	registry := tool.NewRegistry()
	require.NoError(t, registry.Register(builtin.NewReadFileTool()))
	require.NoError(t, registry.Register(builtin.NewEditFileTool()))
	require.NoError(t, registry.Register(builtin.NewWriteFileTool()))
	require.NoError(t, registry.Register(builtin.NewRunCommandTool()))

	manager := newUnconfiguredTestManager(nil, registry)
	names := manager.createSubagentToolsForProfile(DefaultAgentProfile(AgentTypeCoder)).Names()

	assert.Contains(t, names, "read_file")
	assert.Contains(t, names, "edit_file")
	assert.Contains(t, names, "write_file")
	assert.Contains(t, names, "run_command")
}
