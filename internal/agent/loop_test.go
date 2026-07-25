package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/tool/builtin"
	"github.com/junnhwan/bond-code/internal/undo"
)

type fakeReadTool struct {
	executed   bool
	executions int
}

func (f *fakeReadTool) Name() string        { return "read_file" }
func (f *fakeReadTool) Description() string { return "read file" }
func (f *fakeReadTool) Schema() any         { return map[string]any{"type": "object"} }
func (f *fakeReadTool) Risk(json.RawMessage) tool.RiskLevel {
	return tool.RiskLow
}
func (f *fakeReadTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	f.executed = true
	f.executions++
	return &tool.Result{ToolName: "read_file", Output: "file content", OK: true}, nil
}

func TestLoopExecutesStructuredToolCallAndReturnsFinalAnswer(t *testing.T) {
	registry := tool.NewRegistry()
	readTool := &fakeReadTool{}
	if err := registry.Register(readTool); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{Name: "read_file", Arguments: `{"path":"README.md"}`}, Done: true}},
		{{Content: "final answer", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{RequireConfirmation: true}, safety.StaticConfirmer(false))

	result, err := loop.Run(context.Background(), "read README")
	if err != nil {
		t.Fatal(err)
	}

	if result.FinalAnswer != "final answer" {
		t.Fatalf("expected final answer, got %q", result.FinalAnswer)
	}
	if !readTool.executed {
		t.Fatal("expected tool to execute")
	}
	if len(result.Trace.Events) == 0 {
		t.Fatal("expected trace events")
	}
}

func TestLoopAppliesContextGovernanceBeforeModelStream(t *testing.T) {
	client := llmfake.New([][]llm.Chunk{
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))
	loop.SetContextManager(contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{
		AutoCompact:            true,
		MicroCompactKeepRecent: 1,
		MicroCompactMinChars:   10,
	})), 100000)

	initial := []llm.Message{
		{Role: llm.RoleSystem, Content: "system rules must stay"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "1", ToolName: "read_file", Content: strings.Repeat("old file body ", 80)},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "2", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "2", ToolName: "read_file", Content: "recent"},
		{Role: llm.RoleUser, Content: "current question"},
	}
	var events []Event
	result, err := loop.RunMessagesWithEvents(context.Background(), initial, func(event Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}

	streamed := client.LastMessages()
	if len(streamed) == 0 {
		t.Fatal("expected model stream messages")
	}
	if streamed[0].Role != llm.RoleSystem || streamed[0].Content != "system rules must stay" {
		t.Fatalf("expected governed view to preserve system prompt, got %#v", streamed)
	}
	cleared := false
	for _, msg := range streamed {
		if msg.Role == llm.RoleTool && msg.Content == "[Old tool result content cleared]" {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("expected micro-cleared old tool result in model view, got %#v", streamed)
	}
	if len(result.Messages) != len(initial)+1 {
		t.Fatalf("expected local run messages to preserve full history plus final answer, got %#v", result.Messages)
	}
	if !loopTestHasEvent(events, EventContextUpdated) {
		t.Fatalf("expected context update event, got %#v", events)
	}
}

func TestLoopInjectsStoredCompactionSummary(t *testing.T) {
	client := llmfake.New([][]llm.Chunk{
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))
	loop.SetContextManager(contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{AutoCompact: true})), 100000)
	store := contextx.NewSummaryStore(t.TempDir(), "session-1")
	_ = store.Save(contextx.SummaryArtifact{
		Version:   2,
		Summary:   "## Goal\nKeep going\n",
		ReadFiles: []contextx.FileObservation{{Path: "README.md", ToolName: "read_file"}},
	})
	loop.SetContextSummaryStore(store)

	initial := []llm.Message{
		{Role: llm.RoleSystem, Content: "system rules must stay"},
		{Role: llm.RoleUser, Content: "current question"},
	}
	result, err := loop.RunMessagesWithEvents(context.Background(), initial, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("expected final answer, got %q", result.FinalAnswer)
	}

	streamed := client.LastMessages()
	if len(streamed) < 2 {
		t.Fatalf("expected request messages, got %#v", streamed)
	}
	if streamed[1].Role != llm.RoleUser || !strings.Contains(streamed[1].Content, "## Context Summary") {
		t.Fatalf("expected context summary in a user continuation message, got %#v", streamed)
	}
	if !strings.Contains(streamed[1].Content, "Keep going") {
		t.Fatalf("expected stored summary body, got %#v", streamed[1])
	}
}

func TestLoopInjectsPlanningReminderAfterRepeatedToolUseWithoutTodo(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"a"}`}, Done: true}},
		{{ToolCall: &llm.ToolCall{ID: "call-2", Name: "read_file", Arguments: `{"path":"b"}`}, Done: true}},
		{{ToolCall: &llm.ToolCall{ID: "call-3", Name: "read_file", Arguments: `{"path":"c"}`}, Done: true}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 5}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	if _, err := loop.Run(context.Background(), "inspect several files and then implement"); err != nil {
		t.Fatal(err)
	}

	messages := client.LastMessages()
	if len(messages) == 0 || !strings.Contains(messages[0].Content, "Planning reminder") {
		t.Fatalf("expected planning reminder after repeated tool use without todo, got %#v", messages)
	}
}

func loopTestHasEvent(events []Event, eventType EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func TestLoopPairsUnobservedWriteFailureWithOriginalToolCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	write, err := builtin.NewWriteFileToolWithObservations(builtin.NewObservationStore(), undo.NewStore(4))
	if err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	if err := registry.Register(write); err != nil {
		t.Fatal(err)
	}
	arguments, err := json.Marshal(builtin.WriteFileInput{Path: path, Content: "new"})
	if err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "stale-write-call", Name: tool.WriteFile, Arguments: string(arguments)}, Done: true}},
		{{Content: "reread required", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))
	result, err := loop.Run(context.Background(), "update file")
	if err != nil {
		t.Fatal(err)
	}
	matches := 0
	for _, message := range result.Messages {
		if message.Role == llm.RoleTool && message.ToolCallID == "stale-write-call" {
			matches++
			if message.ToolName != tool.WriteFile {
				t.Fatalf("tool name=%q", message.ToolName)
			}
		}
	}
	if matches != 1 {
		t.Fatalf("paired tool results=%d, want 1; messages=%#v", matches, result.Messages)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old" {
		t.Fatalf("file mutated: %q", content)
	}
}
