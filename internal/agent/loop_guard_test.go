package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

type repeatingToolCallClient struct {
	name      string
	argument  string
	calls     int
	lastTools []llm.ToolSpec
	last      []llm.Message
}

func newRepeatingToolCallClient(name, argument string) *repeatingToolCallClient {
	return &repeatingToolCallClient{name: name, argument: argument}
}

func (f *repeatingToolCallClient) Stream(ctx context.Context, messages []llm.Message, tools []llm.ToolSpec) (<-chan llm.Chunk, <-chan error) {
	chunks := make(chan llm.Chunk)
	errs := make(chan error, 1)
	f.calls++
	call := llm.ToolCall{
		ID:        fmt.Sprintf("repeat-call-%d", f.calls),
		Name:      f.name,
		Arguments: f.argument,
	}
	f.last = append([]llm.Message(nil), messages...)
	f.lastTools = append([]llm.ToolSpec(nil), tools...)
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

func (f *repeatingToolCallClient) Calls() int {
	return f.calls
}

type countingTool struct {
	name       string
	risk       tool.RiskLevel
	executions int
}

func (t *countingTool) Name() string        { return t.name }
func (t *countingTool) Description() string { return t.name + " test tool" }
func (t *countingTool) Schema() any         { return map[string]any{"type": "object"} }
func (t *countingTool) Risk(json.RawMessage) tool.RiskLevel {
	return t.risk
}
func (t *countingTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	t.executions++
	return &tool.Result{ToolName: t.name, Output: "ok", OK: true}, nil
}

func TestLoopGuardStopsRepeatedReadFileBeforeMaxSteps(t *testing.T) {
	registry := tool.NewRegistry()
	readTool := &fakeReadTool{}
	if err := registry.Register(readTool); err != nil {
		t.Fatal(err)
	}
	client := newRepeatingToolCallClient("read_file", `{"path":"README.md"}`)
	loop := NewLoop(LoopConfig{MaxSteps: 6}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	var events []Event
	_, err := loop.RunMessagesWithEvents(context.Background(), NewMessages("read README"), func(event Event) {
		events = append(events, event)
	})

	if !hasLoopGuardTestEvent(events, EventToolRequested, "read_file") {
		t.Fatalf("expected structured tool request events, got %#v", events)
	}
	if !hasLoopGuardTestEvent(events, EventToolResult, "read_file") {
		t.Fatalf("expected structured tool result events, got %#v", events)
	}
	if isRawMaxStepsError(err) {
		t.Fatalf("expected repeated read_file calls to be guarded before MaxSteps; got %v after %d model streams and %d real executions", err, client.Calls(), readTool.executions)
	}
	if !hasLoopGuardTestEvent(events, EventLoopGuard, "read_file") {
		t.Fatalf("expected loop guard event for repeated read_file, got %#v", events)
	}
	if readTool.executions > 3 {
		t.Fatalf("expected repeated read_file to stop executing after a small guard threshold, got %d executions; err=%v", readTool.executions, err)
	}
}

func TestLoopGuardStopsRepeatedAuxiliaryToolBeforeMaxSteps(t *testing.T) {
	registry := tool.NewRegistry()
	memoryTool := &countingTool{name: "save_memory", risk: tool.RiskMedium}
	if err := registry.Register(memoryTool); err != nil {
		t.Fatal(err)
	}
	client := newRepeatingToolCallClient("save_memory", `{"content":"remember this exact fact"}`)
	loop := NewLoop(LoopConfig{MaxSteps: 6}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	var events []Event
	_, err := loop.RunMessagesWithEvents(context.Background(), NewMessages("remember this"), func(event Event) {
		events = append(events, event)
	})

	if !hasLoopGuardTestEvent(events, EventToolRequested, "save_memory") {
		t.Fatalf("expected structured auxiliary tool request events, got %#v", events)
	}
	if !hasLoopGuardTestEvent(events, EventToolResult, "save_memory") {
		t.Fatalf("expected structured auxiliary tool result events, got %#v", events)
	}
	if isRawMaxStepsError(err) {
		t.Fatalf("expected repeated save_memory calls to be guarded before MaxSteps; got %v after %d model streams and %d real executions", err, client.Calls(), memoryTool.executions)
	}
	if !hasLoopGuardTestEvent(events, EventLoopGuard, "save_memory") {
		t.Fatalf("expected loop guard event for repeated save_memory, got %#v", events)
	}
	if memoryTool.executions > 3 {
		t.Fatalf("expected repeated save_memory to stop executing after a small guard threshold, got %d executions; err=%v", memoryTool.executions, err)
	}
}

func TestLoopGuardStopPairsRemainingBatchAndFinalizesWithoutTools(t *testing.T) {
	before := &countingTool{name: "before_tool", risk: tool.RiskLow}
	repeated := &countingTool{name: "repeat_tool", risk: tool.RiskLow}
	after := &countingTool{name: "after_tool", risk: tool.RiskLow}
	registry := tool.NewRegistry()
	for _, candidate := range []tool.Tool{before, repeated, after} {
		if err := registry.Register(candidate); err != nil {
			t.Fatal(err)
		}
	}

	repeatCall := func(id string) llm.Chunk {
		return llm.Chunk{ToolCall: &llm.ToolCall{ID: id, Name: repeated.Name(), Arguments: `{"value":"same"}`}, Done: true}
	}
	client := llmfake.New([][]llm.Chunk{
		{repeatCall("repeat-1")},
		{repeatCall("repeat-2")},
		{repeatCall("repeat-3")},
		{{ToolCall: &llm.ToolCall{ID: "call-before", Name: before.Name(), Arguments: `{}`}}, {
			ToolCall: &llm.ToolCall{ID: "repeat-4", Name: repeated.Name(), Arguments: `{"value":"same"}`},
		}, {
			ToolCall: &llm.ToolCall{ID: "call-after", Name: after.Name(), Arguments: `{}`}, Done: true,
		}},
		{{Content: "finalized from existing results", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 5, MaxRepeatedToolCalls: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	var events []Event
	result, err := loop.RunWithEvents(context.Background(), "finish safely after no progress", func(event Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("expected no-progress guard to finalize gracefully, got %v", err)
	}
	if result.FinalAnswer != "finalized from existing results" {
		t.Fatalf("unexpected final answer %q", result.FinalAnswer)
	}
	if repeated.executions != 3 {
		t.Fatalf("expected three real repeated executions before the guard, got %d", repeated.executions)
	}
	if before.executions != 1 {
		t.Fatalf("expected call before the guard to execute, got %d", before.executions)
	}
	if after.executions != 0 {
		t.Fatalf("expected call after the stop guard to be skipped, got %d executions", after.executions)
	}

	batchAssistant := -1
	for i, msg := range result.Messages {
		if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) == 3 && msg.ToolCalls[1].ID == "repeat-4" {
			batchAssistant = i
			break
		}
	}
	if batchAssistant < 0 || batchAssistant+3 >= len(result.Messages) {
		t.Fatalf("expected guarded assistant batch plus three tool results, got %#v", result.Messages)
	}
	wantIDs := []string{"call-before", "repeat-4", "call-after"}
	for i, wantID := range wantIDs {
		msg := result.Messages[batchAssistant+1+i]
		if msg.Role != llm.RoleTool || msg.ToolCallID != wantID {
			t.Fatalf("tool result %d did not preserve pairing/order: %#v", i, msg)
		}
	}
	if !strings.Contains(result.Messages[batchAssistant+3].Content, "loop_guard") {
		t.Fatalf("expected skipped trailing call to receive a guarded result, got %#v", result.Messages[batchAssistant+3])
	}
}

func TestLoopGuardFingerprintsCanonicalJSONArguments(t *testing.T) {
	first := toolCallFingerprint(llm.ToolCall{Name: "read_file", Arguments: `{"path":"README.md","depth":1}`})
	second := toolCallFingerprint(llm.ToolCall{Name: "read_file", Arguments: `{"depth":1,"path":"README.md"}`})
	if first != second {
		t.Fatalf("expected object arguments to be canonicalized, got %q and %q", first, second)
	}

	raw := toolCallFingerprint(llm.ToolCall{Name: "read_file", Arguments: `{not-json`})
	if !strings.Contains(raw, `{not-json`) {
		t.Fatalf("expected invalid JSON to use raw arguments, got %q", raw)
	}
}

func TestLoopGuardCapsToolCallsPerStepWithSyntheticResults(t *testing.T) {
	first := &recordingTool{name: "first_tool", output: "first output"}
	second := &recordingTool{name: "second_tool", output: "second output"}
	third := &recordingTool{name: "third_tool", output: "third output"}
	registry := tool.NewRegistry()
	for _, candidate := range []tool.Tool{first, second, third} {
		if err := registry.Register(candidate); err != nil {
			t.Fatal(err)
		}
	}
	client := llmfake.New([][]llm.Chunk{
		{{
			ToolCall: &llm.ToolCall{ID: "call-first", Name: "first_tool", Arguments: `{"value":"one"}`},
		}, {
			ToolCall: &llm.ToolCall{ID: "call-second", Name: "second_tool", Arguments: `{"value":"two"}`},
		}, {
			ToolCall: &llm.ToolCall{ID: "call-third", Name: "third_tool", Arguments: `{"value":"three"}`},
			Done:     true,
		}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3, MaxToolCallsPerStep: 2}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	var events []Event
	result, err := loop.RunWithEvents(context.Background(), "use too many tools", func(event Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("unexpected final answer %q", result.FinalAnswer)
	}
	if len(first.inputs) != 1 || len(second.inputs) != 1 {
		t.Fatalf("expected first two tools to execute, got first=%#v second=%#v", first.inputs, second.inputs)
	}
	if len(third.inputs) != 0 {
		t.Fatalf("expected third tool to be guarded instead of executed, got inputs %#v", third.inputs)
	}
	if !hasLoopGuardTestEvent(events, EventLoopGuard, "third_tool") {
		t.Fatalf("expected loop guard event for capped tool call, got %#v", events)
	}
	messages := client.LastMessages()
	if len(messages) < 5 {
		t.Fatalf("expected assistant plus three tool results in model context, got %#v", messages)
	}
	results := messages[len(messages)-3:]
	if results[0].ToolCallID != "call-first" || results[1].ToolCallID != "call-second" || results[2].ToolCallID != "call-third" {
		t.Fatalf("expected tool results to preserve original order, got %#v", results)
	}
	if !strings.Contains(results[2].Content, "loop_guard: too many tool calls") {
		t.Fatalf("expected third result to contain synthetic guard output, got %#v", results[2])
	}
}

func hasLoopGuardTestEvent(events []Event, eventType EventType, toolName string) bool {
	for _, event := range events {
		if event.Type == eventType && event.ToolName == toolName {
			return true
		}
	}
	return false
}

func isRawMaxStepsError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "agent stopped after max steps")
}

// TestLoopGuardAllowsRepeatedVerificationAfterProgress reproduces the real
// test-edit-test workflow that the old run-lifetime counter misclassified as a
// loop. Each edit is model-visible progress, so the same verification command
// must be allowed again after it.
func TestLoopGuardAllowsRepeatedVerificationAfterProgress(t *testing.T) {
	registry := tool.NewRegistry()
	verify := &countingTool{name: "go_test", risk: tool.RiskLow}
	edit := &countingTool{name: "edit_file", risk: tool.RiskLow}
	for _, candidate := range []tool.Tool{verify, edit} {
		if err := registry.Register(candidate); err != nil {
			t.Fatal(err)
		}
	}

	toolResponse := func(id, name, arguments string) []llm.Chunk {
		return []llm.Chunk{{
			ToolCall:   &llm.ToolCall{ID: id, Name: name, Arguments: arguments},
			Done:       true,
			StopReason: "tool_use",
		}}
	}
	client := llmfake.New([][]llm.Chunk{
		toolResponse("test-1", "go_test", `{"dir":"workspace"}`),
		toolResponse("edit-1", "edit_file", `{"path":"a.go","content":"one"}`),
		toolResponse("test-2", "go_test", `{"dir":"workspace"}`),
		toolResponse("edit-2", "edit_file", `{"path":"a.go","content":"two"}`),
		toolResponse("test-3", "go_test", `{"dir":"workspace"}`),
		toolResponse("edit-3", "edit_file", `{"path":"a.go","content":"three"}`),
		toolResponse("test-4", "go_test", `{"dir":"workspace"}`),
		{{Content: "verified after the final edit", Done: true, StopReason: "end_turn"}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 8}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	var sawGuard bool
	result, err := loop.RunWithEvents(context.Background(), "implement and verify", func(event Event) {
		if event.Type == EventLoopGuard {
			sawGuard = true
		}
	})
	if err != nil {
		t.Fatalf("expected progress-separated verification to complete, got %v", err)
	}
	if sawGuard {
		t.Fatal("progress-separated identical verification calls must not trip the no-progress guard")
	}
	if verify.executions != 4 {
		t.Fatalf("expected all four verification calls to execute, got %d", verify.executions)
	}
	if result.FinalAnswer != "verified after the final edit" {
		t.Fatalf("unexpected final answer %q", result.FinalAnswer)
	}
}

// TestLoopGuardFinalizationDegenerationIsBounded verifies that the emergency
// no-tools turn is protected by the same repetition breaker as the main turn.
func TestLoopGuardFinalizationDegenerationIsBounded(t *testing.T) {
	registry := tool.NewRegistry()
	repeated := &countingTool{name: "read_file", risk: tool.RiskLow}
	if err := registry.Register(repeated); err != nil {
		t.Fatal(err)
	}
	responses := make([][]llm.Chunk, 0, 5)
	for i := 1; i <= 4; i++ {
		responses = append(responses, []llm.Chunk{{
			ToolCall: &llm.ToolCall{ID: fmt.Sprintf("read-%d", i), Name: "read_file", Arguments: `{"path":"README.md"}`},
			Done:     true, StopReason: "tool_use",
		}})
	}
	degenerateFinalization := make([]llm.Chunk, 50)
	for i := range degenerateFinalization {
		degenerateFinalization[i] = llm.Chunk{Content: "I will now finish. "}
	}
	responses = append(responses, degenerateFinalization)
	client := llmfake.New(responses)
	loop := NewLoop(LoopConfig{MaxSteps: 6}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	var sawDegeneration bool
	result, err := loop.RunWithEvents(context.Background(), "read once", func(event Event) {
		if event.Type == EventTextDegeneration {
			sawDegeneration = true
		}
	})
	if err == nil {
		t.Fatalf("expected degenerate finalization to fail boundedly, got final answer %q", result.FinalAnswer)
	}
	if !sawDegeneration {
		t.Fatal("expected finalization repetition to emit EventTextDegeneration")
	}
	if !strings.Contains(err.Error(), "finalization") || !strings.Contains(err.Error(), "degenerated") {
		t.Fatalf("expected an explicit finalization degeneration error, got %v", err)
	}
}

// TestLoopGuardSurfacesFinalizationStreamError ensures a timeout in the
// emergency no-tools turn is not hidden behind the earlier loop_guard message.
func TestLoopGuardSurfacesFinalizationStreamError(t *testing.T) {
	registry := tool.NewRegistry()
	repeated := &countingTool{name: "read_file", risk: tool.RiskLow}
	if err := registry.Register(repeated); err != nil {
		t.Fatal(err)
	}
	responses := make([][]llm.Chunk, 5)
	for i := 0; i < 4; i++ {
		responses[i] = []llm.Chunk{{
			ToolCall: &llm.ToolCall{ID: fmt.Sprintf("read-%d", i+1), Name: "read_file", Arguments: `{"path":"README.md"}`},
			Done:     true, StopReason: "tool_use",
		}}
	}
	idleErr := fmt.Errorf("stream idle timeout: no chunks received")
	client := llmfake.NewWithErrors(responses, []error{nil, nil, nil, nil, idleErr})
	loop := NewLoop(LoopConfig{MaxSteps: 6}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	_, err := loop.RunWithEvents(context.Background(), "read once", nil)
	if err == nil {
		t.Fatal("expected finalization stream error")
	}
	if !strings.Contains(err.Error(), idleErr.Error()) {
		t.Fatalf("expected the real finalization stream error, got %v", err)
	}
}

func TestLoopGuardAllowsIdenticalCallWhenResultChanges(t *testing.T) {
	cfg := LoopConfig{MaxRepeatedToolCalls: 3, MaxToolCallsPerStep: 8}
	guard := newLoopGuard(cfg)
	call := llm.ToolCall{Name: "go_test", Arguments: `{"dir":"workspace"}`}

	for i, result := range []string{"three tests failed", "one test failed", "all tests passed"} {
		if decision := guard.Check(call, 0); decision.Guarded {
			t.Fatalf("call %d was guarded before three identical no-progress results: %#v", i+1, decision)
		}
		guard.RecordResult(call, result)
	}
	if decision := guard.Check(call, 0); decision.Guarded {
		t.Fatalf("changed verification results demonstrate progress; fourth call must be allowed: %#v", decision)
	}
}
