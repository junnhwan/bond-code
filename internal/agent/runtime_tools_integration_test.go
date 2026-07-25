package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/memory"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/subagent"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/todo"
	"github.com/junnhwan/bond-code/internal/tool"
)

// TestLoopCanCallRuntimeMemoryTodoAndSpawnTools lives in the external test
// package because it exercises real runtime tools (memory / todo / spawn) whose
// packages ultimately import agent (subagent -> agent). Keeping it out of the
// internal test package avoids an import cycle now that subagent reuses agent.Loop.

func TestLoopCanCallRuntimeMemoryTodoAndSpawnTools(t *testing.T) {
	dataDir := t.TempDir()
	memoryStore, err := memory.NewMemoryStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	taskStore, err := todo.NewSessionTaskStore(dataDir, "session-test")
	if err != nil {
		t.Fatal(err)
	}
	subagentClient := llm.NewFakeClient([]llm.Chunk{{Content: "subagent done", Done: true}})
	subagentManager := subagent.NewSubagentManagerWithOptions(subagentClient, tool.NewRegistry(), subagent.ManagerOptions{
		LoopFactory: func(req subagent.LoopRequest) *agent.Loop {
			return agent.NewLoop(agent.LoopConfig{MaxSteps: req.Profile.MaxSteps}, subagentClient, req.Tools, safety.Policy{}, safety.StaticConfirmer(true))
		},
	})

	registry := tool.NewRegistry()
	for _, candidate := range []tool.Tool{
		memory.NewMemorySaveTool(memoryStore),
		todo.NewTodoWriteTool(taskStore),
		subagent.NewSpawnTool(subagentManager, "session-test"),
	} {
		if err := registry.Register(candidate); err != nil {
			t.Fatal(err)
		}
	}
	client := llmfake.New([][]llm.Chunk{
		{{
			ToolCall: &llm.ToolCall{ID: "call-memory", Name: "memory_save", Arguments: `{"type":"project","name":"Runtime tools","description":"Runtime tools are callable","content":"Runtime tools are callable"}`},
		}, {
			ToolCall: &llm.ToolCall{ID: "call-todo", Name: "todo_write", Arguments: `{"items":[{"subject":"Runtime tools","status":"in_progress"}]}`},
		}, {
			ToolCall: &llm.ToolCall{ID: "call-spawn", Name: "spawn", Arguments: `{"prompt":"background task"}`},
			Done:     true,
		}},
		{{Content: "done", Done: true}},
	})
	loop := agent.NewLoop(agent.LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{RequireConfirmation: true}, safety.AutoApproveConfirmer{MaxRisk: "medium"})

	result, err := loop.Run(context.Background(), "use runtime tools")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("unexpected final answer %q", result.FinalAnswer)
	}
	memoryData, err := os.ReadFile(filepath.Join(dataDir, "memory", "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(memoryData), "Runtime tools") {
		t.Fatalf("expected memory tool to write MEMORY.md index, got:\n%s", string(memoryData))
	}
	todoPath := filepath.Join(dataDir, "tasks", "session-test", "todos.json")
	if _, err := os.Stat(todoPath); err != nil {
		t.Fatalf("expected todo_write to persist todos.json: %v", err)
	}
	for _, name := range []string{"memory_save", "todo_write", "spawn"} {
		if !traceIncludesSuccessfulTool(result.Trace, name) {
			t.Fatalf("expected trace to include successful %s result, got %#v", name, result.Trace.Events)
		}
	}
}

func traceIncludesSuccessfulTool(trace agent.Trace, name string) bool {
	for _, event := range trace.Events {
		if event.Type == agent.EventToolResult && event.ToolName == name && event.Error == "" {
			return true
		}
	}
	return false
}
