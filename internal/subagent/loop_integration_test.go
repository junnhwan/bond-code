package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

// These tests prove Phase 1's core claim: when a LoopFactory is injected, a
// child subagent runs on a real agent.Loop and every one of its tool calls flows
// through the shared safety.Policy + Confirmer — the CLAUDE.md invariant the
// legacy runTaskLoop violated by calling t.Execute directly.

// spyTool is a RiskLow tool (so it survives the profile allowlist filter) that
// records whether it was actually executed. executed may be nil when the test
// only cares about the event stream, not the execution gate.
type spyTool struct {
	name     string
	executed *atomic.Bool
	output   string
}

func (s *spyTool) Name() string        { return s.name }
func (s *spyTool) Description() string { return "spy" }
func (s *spyTool) Schema() any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
		"required": []string{"path"},
	}
}
func (s *spyTool) Risk(json.RawMessage) tool.RiskLevel { return tool.RiskLow }
func (s *spyTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	if s.executed != nil {
		s.executed.Store(true)
	}
	out := s.output
	if out == "" {
		out = "spy output"
	}
	return &tool.Result{ToolName: s.name, Output: out, OK: true}, nil
}

// recordingConfirmer records that Confirm was called and returns a fixed
// decision, so a test can assert the child loop reached the confirmer.
type recordingConfirmer struct {
	called atomic.Bool
	allow  bool
}

func (r *recordingConfirmer) Confirm(context.Context, safety.ConfirmationRequest) (bool, error) {
	r.called.Store(true)
	return r.allow, nil
}

func blockedPolicyFactory(client llm.Client) LoopFactory {
	policy := safety.Policy{RequireConfirmation: true, BlockedSubstrings: []string{"BLOCKED-CMD"}}
	return func(req LoopRequest) *agent.Loop {
		return agent.NewLoop(agent.LoopConfig{MaxSteps: 4}, client, req.Tools, policy, safety.StaticConfirmer(false))
	}
}

// TestRunTaskViaLoopBlocksPolicyBlockedTool: a tool whose input carries a blocked
// substring must come back as a blocked envelope and NOT execute. The legacy
// runTaskLoop would have run it (it called t.Execute directly), so this is the
// direct proof the child now inherits the main safety boundary.
func TestRunTaskViaLoopBlocksPolicyBlockedTool(t *testing.T) {
	var executed atomic.Bool
	rt := &spyTool{name: "read_file", executed: &executed}
	registry := tool.NewRegistry()
	if err := registry.Register(rt); err != nil {
		t.Fatal(err)
	}

	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"BLOCKED-CMD"}`}}},
		{{Content: "could not read, wrapping up", Done: true}},
	})
	manager := NewSubagentManagerWithOptions(client, registry, ManagerOptions{
		DefaultTimeoutSeconds: 5,
		LoopFactory:           blockedPolicyFactory(client),
	})

	result, err := manager.RunTask(context.Background(), TaskRequest{
		Prompt: "read it", SubagentType: AgentTypeResearch, TaskID: "t1",
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if executed.Load() {
		t.Fatal("child executed a policy-blocked tool — safety boundary bypassed")
	}
	if result.Status != "completed" {
		t.Fatalf("expected completed (blocked envelope fed back, model wraps up), got status=%q error=%q", result.Status, result.Error)
	}
}

// TestRunTaskViaLoopAsksConfirmerForRuleAskTool: a tool an "ask" permission rule
// matches must reach the Confirmer. When the confirmer denies, the tool is
// rejected and not executed — proving the full Policy+Confirmer path (not Policy
// alone) is in the child execution path.
func TestRunTaskViaLoopAsksConfirmerForRuleAskTool(t *testing.T) {
	var executed atomic.Bool
	rt := &spyTool{name: "read_file", executed: &executed}
	registry := tool.NewRegistry()
	if err := registry.Register(rt); err != nil {
		t.Fatal(err)
	}

	confirmer := &recordingConfirmer{allow: false}
	policy := safety.Policy{
		RequireConfirmation: true,
		Rules:               []safety.PermissionRule{{Tools: []string{"read_file"}, Decision: "ask"}},
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"/x"}`}}},
		{{Content: "denied, wrapping up", Done: true}},
	})
	factory := func(req LoopRequest) *agent.Loop {
		return agent.NewLoop(agent.LoopConfig{MaxSteps: 4}, client, req.Tools, policy, confirmer)
	}
	manager := NewSubagentManagerWithOptions(client, registry, ManagerOptions{
		DefaultTimeoutSeconds: 5,
		LoopFactory:           factory,
	})

	result, err := manager.RunTask(context.Background(), TaskRequest{
		Prompt: "read", SubagentType: AgentTypeResearch, TaskID: "t1",
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if !confirmer.called.Load() {
		t.Fatal("child tool call did not reach the confirmer — safety boundary bypassed")
	}
	if executed.Load() {
		t.Fatal("child executed a tool the confirmer denied")
	}
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
}

// TestRunTaskViaLoopEmitsChildToolStreamEvents: reusing agent.Loop must not
// regress the TUI's child tool-stream rendering. The adapter should still emit a
// "running" EventToolCall when the model requests a tool and a "done" one after
// it executes — the same two-event shape the legacy mini-loop produced.
func TestRunTaskViaLoopEmitsChildToolStreamEvents(t *testing.T) {
	rt := &spyTool{name: "read_file", output: "file body"}
	registry := tool.NewRegistry()
	if err := registry.Register(rt); err != nil {
		t.Fatal(err)
	}

	var events []Event
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"/x"}`}}},
		{{Content: "done", Done: true}},
	})
	policy := safety.Policy{RequireConfirmation: true}
	factory := func(req LoopRequest) *agent.Loop {
		return agent.NewLoop(agent.LoopConfig{MaxSteps: 4}, client, req.Tools, policy, safety.StaticConfirmer(false))
	}
	manager := NewSubagentManagerWithOptions(client, registry, ManagerOptions{
		DefaultTimeoutSeconds: 5,
		LoopFactory:           factory,
		EventSink:             func(e Event) { events = append(events, e) },
	})

	result, err := manager.RunTask(context.Background(), TaskRequest{
		Prompt: "read", SubagentType: AgentTypeResearch, TaskID: "t1",
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}

	var toolEvents []Event
	for _, e := range events {
		if e.Type == EventToolCall {
			toolEvents = append(toolEvents, e)
		}
	}
	if len(toolEvents) < 2 {
		t.Fatalf("expected >=2 tool-call events (running + done), got %#v", events)
	}
	if toolEvents[0].ToolStatus != "running" || toolEvents[0].ToolName != "read_file" {
		t.Fatalf("first tool event should be running read_file, got %#v", toolEvents[0])
	}
	foundDone := false
	for _, e := range toolEvents {
		if e.ToolStatus == "done" {
			foundDone = true
			if e.ToolOutput != "file body" {
				t.Fatalf("done event lost tool output, got %#v", e)
			}
		}
	}
	if !foundDone {
		t.Fatalf("no done tool event in %#v", toolEvents)
	}
}

// captureClient wraps an llm.Client and records the messages each Stream call
// received, so a test can assert what the child Loop was actually fed (used to
// prove resume replays the prior child's history into the resumed run).
type captureClient struct {
	inner llm.Client
	mu    sync.Mutex
	calls [][]llm.Message
}

func (c *captureClient) Stream(ctx context.Context, messages []llm.Message, tools []llm.ToolSpec) (<-chan llm.Chunk, <-chan error) {
	c.mu.Lock()
	c.calls = append(c.calls, append([]llm.Message(nil), messages...))
	c.mu.Unlock()
	return c.inner.Stream(ctx, messages, tools)
}

func (c *captureClient) callMessages(i int) []llm.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i < 0 || i >= len(c.calls) {
		return nil
	}
	return c.calls[i]
}

func (c *captureClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func containsMessage(msgs []llm.Message, role llm.Role, content string) bool {
	for _, m := range msgs {
		if m.Role == role && strings.Contains(m.Content, content) {
			return true
		}
	}
	return false
}

// TestRunTaskResumeContinuesPriorChildHistory (Phase 4): after a task completes,
// its child history is saved; a follow-up task with resume_task_id replays that
// history as the resumed run's opening context (same profile), so the child
// picks up the prior conversation instead of starting fresh.
func TestRunTaskResumeContinuesPriorChildHistory(t *testing.T) {
	inner := llmfake.New([][]llm.Chunk{
		{{Content: "FIRST ANSWER", Done: true}},  // first run: no tool call → done
		{{Content: "SECOND ANSWER", Done: true}}, // resumed run
	})
	client := &captureClient{inner: inner}
	policy := safety.Policy{RequireConfirmation: true}
	factory := func(req LoopRequest) *agent.Loop {
		return agent.NewLoop(agent.LoopConfig{MaxSteps: 4}, client, req.Tools, policy, safety.StaticConfirmer(false))
	}
	manager := NewSubagentManagerWithOptions(client, tool.NewRegistry(), ManagerOptions{
		DefaultTimeoutSeconds: 5,
		LoopFactory:           factory,
	})

	first, err := manager.RunTask(context.Background(), TaskRequest{
		Prompt: "explore the module", SubagentType: AgentTypeResearch, TaskID: "task-A",
	})
	if err != nil {
		t.Fatalf("first RunTask: %v", err)
	}
	if first.Status != "completed" || !strings.Contains(first.FinalAnswer, "FIRST ANSWER") {
		t.Fatalf("first run expected completed/FIRST ANSWER, got status=%q answer=%q", first.Status, first.FinalAnswer)
	}
	if _, ok := manager.loadResumable("task-A"); !ok {
		t.Fatal("first run did not save a resumable child session under task-A")
	}

	second, err := manager.RunTask(context.Background(), TaskRequest{
		Prompt: "now summarize it", SubagentType: AgentTypeResearch, TaskID: "task-B", ResumeTaskID: "task-A",
	})
	if err != nil {
		t.Fatalf("resume RunTask: %v", err)
	}
	if second.Status != "completed" {
		t.Fatalf("resume run expected completed, got status=%q error=%q", second.Status, second.Error)
	}

	// The resumed run is the 2nd Stream call. Its input must carry the prior
	// child's full history (original user turn + the FIRST ANSWER assistant
	// turn) plus the new user turn as the closing message.
	if manager != nil && client.callCount() < 2 {
		t.Fatalf("expected >=2 stream calls (first + resume), got %d", client.callCount())
	}
	resumed := client.callMessages(1)
	if !containsMessage(resumed, llm.RoleUser, "explore the module") {
		t.Fatalf("resumed run lost prior user turn, messages=%#v", resumed)
	}
	if !containsMessage(resumed, llm.RoleAssistant, "FIRST ANSWER") {
		t.Fatalf("resumed run lost prior assistant turn (history not replayed), messages=%#v", resumed)
	}
	if len(resumed) == 0 {
		t.Fatal("resumed run had no messages")
	}
	last := resumed[len(resumed)-1]
	if last.Role != llm.RoleUser || !strings.Contains(last.Content, "now summarize it") {
		t.Fatalf("resumed run's last message should be the new user turn, got %#v", last)
	}
	// The resumed run should also be saved so it can be re-resumed.
	if _, ok := manager.loadResumable("task-B"); !ok {
		t.Fatal("resumed run did not save its own child session under task-B")
	}
}

// TestRunTaskResumeUnknownIDFailsSafely (Phase 4): resuming an id with no saved
// child context must return a protocol-safe failed result (the run never
// executes, no LLM call made), not a Go error that breaks the tool-result chain.
func TestRunTaskResumeUnknownIDFailsSafely(t *testing.T) {
	inner := llmfake.New([][]llm.Chunk{{{Content: "should not run", Done: true}}})
	client := &captureClient{inner: inner}
	factory := func(req LoopRequest) *agent.Loop {
		return agent.NewLoop(agent.LoopConfig{MaxSteps: 4}, client, req.Tools, safety.Policy{}, safety.StaticConfirmer(false))
	}
	manager := NewSubagentManagerWithOptions(client, tool.NewRegistry(), ManagerOptions{
		DefaultTimeoutSeconds: 5,
		LoopFactory:           factory,
	})

	result, err := manager.RunTask(context.Background(), TaskRequest{
		Prompt: "continue", SubagentType: AgentTypeResearch, TaskID: "t-x", ResumeTaskID: "never-existed",
	})
	if err != nil {
		t.Fatalf("unknown resume id should not return a Go error, got %v", err)
	}
	if result == nil || result.Status != "failed" {
		t.Fatalf("expected failed result for unknown resume id, got %#v", result)
	}
	if !strings.Contains(result.Error, "not found") {
		t.Fatalf("expected error to explain the id was not found, got %q", result.Error)
	}
	if client.callCount() != 0 {
		t.Fatalf("unknown resume id must not start a child run (no LLM call), got %d calls", client.callCount())
	}
}

// A hot session switch must not make resume_task_id cross session boundaries.
// The TaskTool is long-lived, so rebinding it changes both newly saved child
// contexts and which saved contexts a follow-up is allowed to load.
func TestTaskToolBindSessionIsolatesResumableChildContext(t *testing.T) {
	inner := llmfake.New([][]llm.Chunk{
		{{Content: "SESSION A ANSWER", Done: true}},
		{{Content: "MUST NOT RUN", Done: true}},
	})
	client := &captureClient{inner: inner}
	factory := func(req LoopRequest) *agent.Loop {
		return agent.NewLoop(agent.LoopConfig{MaxSteps: 4}, client, req.Tools, safety.Policy{}, safety.StaticConfirmer(false))
	}
	manager := NewSubagentManagerWithOptions(client, tool.NewRegistry(), ManagerOptions{
		DefaultTimeoutSeconds: 5,
		LoopFactory:           factory,
	})
	taskTool := NewTaskTool(manager)
	taskTool.BindSession("session-A")

	first, err := taskTool.Execute(context.Background(), []byte(`{
		"prompt":"inspect session A",
		"subagent_type":"research",
		"task_id":"task-A"
	}`))
	if err != nil || !first.OK {
		t.Fatalf("first task: result=%#v err=%v", first, err)
	}

	taskTool.BindSession("session-B")
	resumed, err := taskTool.Execute(context.Background(), []byte(`{
		"prompt":"continue old child",
		"subagent_type":"research",
		"task_id":"task-B",
		"resume_task_id":"task-A"
	}`))
	if err != nil {
		t.Fatalf("cross-session resume should return a tool envelope: %v", err)
	}
	if resumed.OK || !strings.Contains(resumed.Error, "session-scoped") {
		t.Fatalf("cross-session child context was not rejected: %#v", resumed)
	}
	if got := client.callCount(); got != 1 {
		t.Fatalf("cross-session resume reached the LLM; calls=%d, want 1", got)
	}
}

// TestRunTaskRequiresLoopFactory locks the safety invariant at the manager
// boundary: child execution must never fall back to a private tool loop.
