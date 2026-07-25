package subagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/tool/builtin"
	"github.com/junnhwan/bond-code/internal/undo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockLLMClient for testing
type MockLLMClient struct {
	Chunks []llm.Chunk
	Error  error
}

func (m *MockLLMClient) Stream(ctx context.Context, messages []llm.Message, tools []llm.ToolSpec) (<-chan llm.Chunk, <-chan error) {
	chunks := make(chan llm.Chunk, len(m.Chunks)+1)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		if m.Error != nil {
			errs <- m.Error
			return
		}

		for _, chunk := range m.Chunks {
			chunks <- chunk
		}
	}()

	return chunks, errs
}


func TestSubagentManager_CancelBySession(t *testing.T) {
	// This test verifies CancelBySession can be called without error
	// The actual cancellation logic is tested implicitly
	registry := tool.NewRegistry()
	manager := newUnconfiguredTestManager(nil, registry)

	// Test canceling non-existent session (should return 0)
	count := manager.CancelBySession("non-existent")
	assert.Equal(t, 0, count)

	// Test canceling empty session
	manager.getOrCreateTaskSet("session1") // create empty taskSet
	count = manager.CancelBySession("session1")
	assert.Equal(t, 0, count)
}

func TestSubagentManager_CreateSubagentTools(t *testing.T) {
	registry := tool.NewRegistry()
	// Mock tools
	registry.Register(&MockTool{name: "read_file"})
	registry.Register(&MockTool{name: "task"})
	registry.Register(&MockTool{name: "memory_save"})
	registry.Register(&MockTool{name: "mcp__filesystem__read_file"})

	manager := newUnconfiguredTestManager(nil, registry)
	subRegistry := manager.createSubagentToolsForProfile(DefaultAgentProfile(AgentTypeResearch))

	// Check which tools are included
	subNames := subRegistry.Names()

	assert.Contains(t, subNames, "read_file")
	assert.NotContains(t, subNames, "task")
	assert.NotContains(t, subNames, "memory_save")
	assert.NotContains(t, subNames, "mcp__filesystem__read_file")
}

func TestSubagentManager_RunTaskReturnsFinalAnswerWithMetadata(t *testing.T) {
	mockClient := &MockLLMClient{
		Chunks: []llm.Chunk{
			{Content: "I inspected README.md and found the agent loop summary.", Done: true},
		},
	}
	registry := tool.NewRegistry()
	manager := newTestManager(mockClient, registry)

	result, err := manager.RunTask(context.Background(), TaskRequest{
		Description:  "review agent loop",
		Prompt:       "summarize the Agent Loop section",
		SubagentType: AgentTypeResearch,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.TaskID, "sub-")
	assert.Equal(t, "completed", result.Status)
	assert.Equal(t, AgentTypeResearch, result.AgentType)
	assert.Equal(t, "I inspected README.md and found the agent loop summary.", result.FinalAnswer)
	assert.Equal(t, 1, result.Iterations)
}

func TestSubagentManager_ProfileRestrictsResearchTools(t *testing.T) {
	registry := tool.NewRegistry()
	for _, name := range []string{"read_file", "search_text", "write_file", "run_command", "task", "memory_save"} {
		require.NoError(t, registry.Register(&MockTool{name: name}))
	}
	manager := newUnconfiguredTestManager(nil, registry)

	subRegistry := manager.createSubagentToolsForProfile(DefaultAgentProfile(AgentTypeResearch))
	names := subRegistry.Names()

	assert.Contains(t, names, "read_file")
	assert.Contains(t, names, "search_text")
	assert.NotContains(t, names, "write_file")
	assert.NotContains(t, names, "run_command")
	assert.NotContains(t, names, "task")
}


// MockTool for testing
type MockTool struct {
	name string
}

func (m *MockTool) Name() string {
	return m.name
}

func (m *MockTool) Description() string {
	return "mock tool"
}

func (m *MockTool) Schema() any {
	return map[string]interface{}{}
}

func (m *MockTool) Risk(args json.RawMessage) tool.RiskLevel {
	return tool.RiskLow
}

func (m *MockTool) Execute(ctx context.Context, args json.RawMessage) (*tool.Result, error) {
	return &tool.Result{
		ToolName: m.name,
		Output:   "mock result",
		OK:       true,
	}, nil
}

func TestTaskSet_Operations(t *testing.T) {
	ts := &taskSet{}

	// Add tasks
	ts.add("task1")
	ts.add("task2")
	ts.add("task3")

	// Snapshot
	snapshot := ts.snapshot()
	assert.Equal(t, 3, len(snapshot))
	assert.Contains(t, snapshot, "task1")

	// Remove
	ts.remove("task2")
	snapshot = ts.snapshot()
	assert.Equal(t, 2, len(snapshot))
	assert.NotContains(t, snapshot, "task2")
}

func TestDefaultManagerOptionsAllowLongAgentTasks(t *testing.T) {
	got := DefaultManagerOptions().DefaultTimeoutSeconds
	if got != 600 {
		t.Fatalf("default subagent timeout = %ds, want 600s for multi-step agent tasks", got)
	}
}

// SpawnTool is long-lived in the runtime registry. After /resume it must use
// the newly active session for task ownership so cancellation and cleanup do
// not continue targeting the bootstrap session.



func TestSubagentRegistryReusesGuardedFileToolInstances(t *testing.T) {
	observations := builtin.NewObservationStore()
	history := undo.NewStore(4)
	read, err := builtin.NewReadFileToolWithObservations(observations)
	if err != nil {
		t.Fatal(err)
	}
	write, err := builtin.NewWriteFileToolWithObservations(observations, history)
	if err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	if err := registry.Register(read); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(write); err != nil {
		t.Fatal(err)
	}
	manager := newUnconfiguredTestManager(nil, registry)
	child := manager.createSubagentToolsForProfile(DefaultAgentProfile(AgentTypeCoder))
	childRead, ok := child.Get(tool.ReadFile)
	if !ok {
		t.Fatal("child read missing")
	}
	childWrite, ok := child.Get(tool.WriteFile)
	if !ok {
		t.Fatal("child write missing")
	}
	if childRead != read || childWrite != write {
		t.Fatal("child registry cloned guarded file tools")
	}
}
