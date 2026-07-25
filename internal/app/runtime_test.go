package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/memory"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/todo"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/stretchr/testify/require"
)

func TestAppChatPreservesConversationHistoryAcrossTurns(t *testing.T) {
	client := llmfake.New([][]llm.Chunk{
		{{Content: "first answer", Done: true}},
		{{Content: "second answer", Done: true}},
	})
	application := &App{
		Agent: agent.NewLoop(agent.LoopConfig{MaxSteps: 2}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true)),
	}

	if _, err := application.Chat(context.Background(), "first question"); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Chat(context.Background(), "second question"); err != nil {
		t.Fatal(err)
	}

	messages := client.LastMessages()
	if len(messages) < 4 {
		t.Fatalf("expected second turn to include prior user and assistant history, got %#v", messages)
	}
	want := []llm.Message{
		{Role: llm.RoleSystem},
		{Role: llm.RoleUser, Content: "first question"},
		{Role: llm.RoleAssistant, Content: "first answer"},
		{Role: llm.RoleUser, Content: "second question"},
	}
	for i, expected := range want {
		if messages[i].Role != expected.Role {
			t.Fatalf("message %d: expected role %s, got %#v", i, expected.Role, messages[i])
		}
		if expected.Content != "" && messages[i].Content != expected.Content {
			t.Fatalf("message %d: expected content %q, got %#v", i, expected.Content, messages[i])
		}
	}
}

func TestAppMessagesForPromptUsesRuntimePromptContext(t *testing.T) {
	application := &App{
		RuntimePromptContext: agent.RuntimePromptContext{
			Memory: "User prefers Chinese answers.",
			Tasks:  "- in_progress: Implement prompt context.",
		},
	}

	messages := application.messagesForPrompt(context.Background(), "first prompt")
	if len(messages) != 2 {
		t.Fatalf("expected initial system and user messages, got %#v", messages)
	}
	// Volatile context rides on the user turn as a <system-reminder>, NOT the
	// system prompt, so the system prefix stays cache-stable.
	last := messages[len(messages)-1]
	if !strings.Contains(last.Content, "User prefers Chinese answers.") {
		t.Fatalf("expected memory context in user reminder, got %#v", last)
	}
	if !strings.Contains(last.Content, "Implement prompt context.") {
		t.Fatalf("expected task context in user reminder, got %#v", last)
	}
	if strings.Contains(messages[0].Content, "User prefers Chinese answers.") {
		t.Fatalf("memory must not leak into system prompt, got %#v", messages[0])
	}

	application.history = []llm.Message{
		{Role: llm.RoleSystem, Content: "old system prompt"},
		{Role: llm.RoleUser, Content: "first prompt"},
		{Role: llm.RoleAssistant, Content: "first answer"},
	}
	application.RuntimePromptContext.Memory = "Updated memory context."
	messages = application.messagesForPrompt(context.Background(), "second prompt")
	last = messages[len(messages)-1]
	if !strings.Contains(last.Content, "Updated memory context.") {
		t.Fatalf("expected updated memory in user reminder, got %#v", last)
	}
	if strings.Contains(messages[0].Content, "old system prompt") {
		t.Fatalf("expected old system prompt to be replaced, got %#v", messages[0])
	}
	if got := messages[len(messages)-1]; got.Role != llm.RoleUser || !strings.Contains(got.Content, "second prompt") {
		t.Fatalf("expected new user message at the end, got %#v", got)
	}
}

func TestRuntimePromptUsesRelevantStructuredMemory(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := memory.NewMemoryStore(tmpDir)
	require.NoError(t, err)
	require.NoError(t, store.Save(memory.MemoryFile{
		Type:        memory.TypeProject,
		Name:        "Go tests",
		Description: "Project uses Go tests for runtime validation",
		Body:        "Project uses Go tests for runtime validation.",
	}))
	require.NoError(t, store.Save(memory.MemoryFile{
		Type:        memory.TypeUser,
		Name:        "Resume wording",
		Description: "Resume wording should avoid unsupported metrics",
		Body:        "Resume wording should avoid unsupported metrics.",
	}))
	application := &App{
		Config:               &config.Config{Memory: config.MemoryConfig{Enabled: true, MaxRelevant: 5}},
		MemoryStore:          store,
		MemoryMaxChars:       2000,
		RuntimePromptContext: agent.RuntimePromptContext{ProjectRoot: "repo"},
		history:              []llm.Message{{Role: llm.RoleUser, Content: "fix Go tests"}},
	}

	ctx := application.runtimePromptContext(context.Background(), "fix Go tests")
	require.NotEmpty(t, ctx.MemoryGuidance)
	// MEMORY.md index may list all topics; relevant bodies should prefer the Go one.
	require.Contains(t, ctx.Memory, "Go tests")
	require.Contains(t, ctx.Memory, "### Relevant memories")
	require.Contains(t, ctx.Memory, "project_go_tests.md")
	require.NotContains(t, ctx.Memory, "### user_resume_wording")
}

func TestAppChatInjectsSavedMemoryIntoNextPrompt(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	t.Setenv("BONDCODE_TEST_API_KEY", "prompt-leak-sentinel")
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-memory", Name: "memory_save", Arguments: `{"type":"user","name":"Language","description":"Prefers concise Chinese answers","content":"Remember: User prefers concise Chinese answers."}`}, Done: true}},
		{{Content: "memory saved", Done: true}},
		{{Content: "memory visible", Done: true}},
	})
	application, err := Bootstrap(Options{
		ConfigPath: configPath,
		Client:     client,
		AutoYes:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := application.Chat(context.Background(), "remember this"); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Chat(context.Background(), "what do you remember?"); err != nil {
		t.Fatal(err)
	}

	messages := client.LastMessages()
	if len(messages) == 0 {
		t.Fatalf("expected messages, got none")
	}
	// Memory rides on the user turn as a <system-reminder>, not the system prompt.
	last := messages[len(messages)-1]
	if !strings.Contains(last.Content, "User prefers concise Chinese answers.") {
		t.Fatalf("expected memory to be injected into the user reminder, got %#v", last)
	}
	for _, m := range messages {
		if strings.Contains(m.Content, "prompt-leak-sentinel") {
			t.Fatalf("expected no API key env value in any message, got %#v", m)
		}
	}
}

func TestAppChatInjectsTaskSummaryIntoPrompt(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	client := llm.NewFakeClient([]llm.Chunk{{Content: "tasks visible", Done: true}})
	application, err := Bootstrap(Options{
		ConfigPath: configPath,
		Client:     client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.TaskStore.ReplaceAll([]todo.Task{
		{ID: "1", Subject: "Implement todo_write", Status: todo.TaskStatusInProgress},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := application.Chat(context.Background(), "what is next?"); err != nil {
		t.Fatal(err)
	}

	messages := client.LastMessages()
	if len(messages) == 0 {
		t.Fatalf("expected messages, got none")
	}
	last := messages[len(messages)-1]
	if !strings.Contains(last.Content, "Implement todo_write") {
		t.Fatalf("expected task summary to be injected into the user reminder, got %#v", last)
	}
}

func TestAppChatInjectsSavedContextSummaryIntoPrompt(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	client := llm.NewFakeClient([]llm.Chunk{{Content: "summary visible", Done: true}})
	application, err := Bootstrap(Options{
		ConfigPath: configPath,
		Client:     client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if application.ContextSummary == nil {
		t.Fatal("expected context summary store")
	}
	if err := application.ContextSummary.Save(contextx.SummaryArtifact{
		Version: 1,
		Summary: "Earlier: baseline verified.",
		ReadFiles: []contextx.FileObservation{
			{Path: "README.md", ToolName: "read_file"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := application.Chat(context.Background(), "continue from summary"); err != nil {
		t.Fatal(err)
	}

	messages := client.LastMessages()
	if len(messages) == 0 {
		t.Fatalf("expected messages, got none")
	}
	last := messages[len(messages)-1]
	if !strings.Contains(last.Content, "## Context Summary") {
		t.Fatalf("expected context summary section in the user reminder, got %#v", last)
	}
	if !strings.Contains(last.Content, "Earlier: baseline verified.") || !strings.Contains(last.Content, "README.md") {
		t.Fatalf("expected saved summary artifact in the user reminder, got %#v", last)
	}
}

func TestAppChatEmitsSubagentLifecycleEvents(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-task", Name: "task", Arguments: `{"description":"inspect docs","prompt":"inspect docs","subagent_type":"research"}`}, Done: true}},
		{{Content: "child answer", Done: true}},
		{{Content: "parent done", Done: true}},
	})
	application, err := Bootstrap(Options{
		ConfigPath: configPath,
		Client:     client,
		AutoYes:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	var events []agent.Event
	if _, err := application.RunWithEvents(context.Background(), "delegate docs", func(event agent.Event) {
		events = append(events, event)
	}); err != nil {
		t.Fatal(err)
	}

	if !hasAgentRuntimeEvent(events, agent.EventSubagentStarted, "research") {
		t.Fatalf("expected subagent start event, got %#v", events)
	}
	if !hasAgentRuntimeEvent(events, agent.EventSubagentFinished, "research") {
		t.Fatalf("expected subagent finish event, got %#v", events)
	}
}

func TestAppChatPersistsAgentEventsToSessionJSONL(t *testing.T) {
	store := session.NewJSONLStore(t.TempDir())
	registry := tool.NewRegistry()
	if err := registry.Register(&sessionReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-read", Name: "read_file", Arguments: `{"path":"README.md"}`}, Done: true}},
		{{Content: "summary", Done: true}},
	})
	application := &App{
		SessionID: "session-test",
		Sessions:  store,
		Agent:     agent.NewLoop(agent.LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true)),
	}

	if _, err := application.Chat(context.Background(), "read README"); err != nil {
		t.Fatal(err)
	}

	events, err := store.Load("session-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected persisted session events")
	}
	if !hasSessionMessage(events, session.RoleUser, "read README") {
		t.Fatalf("expected persisted user message, got %#v", events)
	}
	if !hasSessionMessage(events, session.RoleAssistant, "summary") {
		t.Fatalf("expected persisted assistant message, got %#v", events)
	}
	if !hasAgentSessionEvent(events, agent.EventToolRequested, "read_file") {
		t.Fatalf("expected persisted raw agent tool request event, got %#v", events)
	}
	if !hasToolSessionEvent(events, "call-read", "read_file", `{"path":"README.md"}`, "file content") {
		t.Fatalf("expected persisted read_file tool event, got %#v", events)
	}
}

func TestAppRuntimeRepeatedToolCallsEmitEventsBeforeGuardStopsLoop(t *testing.T) {
	sessionDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("session:\n  dir: %q\nsafety:\n  require_confirmation: false\n", sessionDir)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	readPath := filepath.Join(t.TempDir(), "runtime-loop-input.txt")
	if err := os.WriteFile(readPath, []byte("small runtime fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	readArgs, err := json.Marshal(map[string]string{"path": readPath})
	if err != nil {
		t.Fatal(err)
	}

	client := newAppRepeatingToolCallClient("read_file", string(readArgs))
	application, err := Bootstrap(Options{
		ConfigPath: cfgPath,
		Client:     client,
		AutoYes:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	application.Agent = agent.NewLoop(agent.LoopConfig{MaxSteps: 6}, client, application.Tools, application.Policy, application.Confirmer)

	var events []agent.Event
	_, err = application.RunWithEvents(context.Background(), "read README", func(event agent.Event) {
		events = append(events, event)
	})

	if !hasAgentRuntimeEvent(events, agent.EventToolRequested, "read_file") {
		t.Fatalf("expected app runtime to emit structured tool request events, got %#v", events)
	}
	if !hasAgentRuntimeEvent(events, agent.EventToolResult, "read_file") {
		t.Fatalf("expected app runtime to emit structured tool result events, got %#v", events)
	}
	if err != nil && strings.Contains(err.Error(), "agent stopped after max steps") {
		t.Fatalf("expected app runtime guard to stop or redirect repeated tool calls before raw MaxSteps; got %v after %d model streams", err, client.Calls())
	}
}

type sessionReadTool struct{}

func (sessionReadTool) Name() string        { return "read_file" }
func (sessionReadTool) Description() string { return "read file" }
func (sessionReadTool) Schema() any         { return map[string]any{"type": "object"} }
func (sessionReadTool) Risk(json.RawMessage) tool.RiskLevel {
	return tool.RiskLow
}
func (sessionReadTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	return &tool.Result{ToolName: "read_file", Output: "file content", OK: true}, nil
}

func hasSessionMessage(events []session.Event, role session.Role, content string) bool {
	for _, event := range events {
		if event.Message != nil && event.Message.Role == role && event.Message.Content == content {
			return true
		}
	}
	return false
}

func hasToolSessionEvent(events []session.Event, id, name, input, output string) bool {
	for _, event := range events {
		if event.ToolCall == nil {
			continue
		}
		call := event.ToolCall
		if call.ID == id && call.Name == name && call.Input == input && strings.Contains(call.Output, output) {
			return true
		}
	}
	return false
}

func hasAgentSessionEvent(events []session.Event, eventType agent.EventType, toolName string) bool {
	for _, event := range events {
		if event.AgentEvent != nil && event.AgentEvent.Type == string(eventType) && event.AgentEvent.ToolName == toolName {
			return true
		}
	}
	return false
}

func hasAgentRuntimeEvent(events []agent.Event, eventType agent.EventType, toolName string) bool {
	for _, event := range events {
		if event.Type == eventType && event.ToolName == toolName {
			return true
		}
	}
	return false
}

type appRepeatingToolCallClient struct {
	name     string
	argument string
	calls    int
	last     []llm.Message
}

func newAppRepeatingToolCallClient(name, argument string) *appRepeatingToolCallClient {
	return &appRepeatingToolCallClient{name: name, argument: argument}
}

func (f *appRepeatingToolCallClient) Stream(ctx context.Context, messages []llm.Message, tools []llm.ToolSpec) (<-chan llm.Chunk, <-chan error) {
	chunks := make(chan llm.Chunk)
	errs := make(chan error, 1)
	f.calls++
	call := llm.ToolCall{
		ID:        fmt.Sprintf("repeat-call-%d", f.calls),
		Name:      f.name,
		Arguments: f.argument,
	}
	f.last = append([]llm.Message(nil), messages...)
	go func() {
		defer close(chunks)
		defer close(errs)
		select {
		case <-ctx.Done():
			errs <- ctx.Err()
		case chunks <- llm.Chunk{ToolCall: &call, Done: true}:
			errs <- nil
		}
	}()
	return chunks, errs
}

func (f *appRepeatingToolCallClient) Calls() int {
	return f.calls
}

func (f *appRepeatingToolCallClient) LastMessages() []llm.Message {
	return append([]llm.Message(nil), f.last...)
}
