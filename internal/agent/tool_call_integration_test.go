package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

func TestLoopStopsAfterMaxSteps(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{ToolCall: &llm.ToolCall{ID: "2", Name: "read_file", Arguments: `{}`}, Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 1}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	_, err := loop.Run(context.Background(), "loop")
	if err == nil || !strings.Contains(err.Error(), "agent stopped after max steps") {
		t.Fatalf("expected max steps error, got %v", err)
	}
}

func TestLoopReturnsLatestToolResultWhenStepBudgetEndsAfterProgress(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "1", Name: "read_file", Arguments: `{"path":"one"}`}, Done: true}},
		{{ToolCall: &llm.ToolCall{ID: "2", Name: "read_file", Arguments: `{"path":"two"}`}, Done: true}},
		{{Content: "forced final answer from existing tool results", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	result, err := loop.Run(context.Background(), "loop with progress")
	if err != nil {
		t.Fatalf("expected step-budget fallback instead of raw max steps error, got %v", err)
	}
	if result.FinalAnswer != "forced final answer from existing tool results" {
		t.Fatalf("expected finalization model call to produce final answer, got %q", result.FinalAnswer)
	}
}

func TestLoopRepairsNativeToolCallDuringStepBudgetFinalization(t *testing.T) {
	read := &recordingTool{name: "read_file", output: "file content"}
	registry := tool.NewRegistry()
	if err := registry.Register(read); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{ToolCall: &llm.ToolCall{ID: "2", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{ToolCall: &llm.ToolCall{ID: "must-not-run", Name: "read_file", Arguments: `{}`}, Done: true, StopReason: "tool_use"}},
		{{Content: "Two files were reviewed; the main risk is missing validation.", Done: true, StopReason: "end_turn"}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	requested := 0
	result, err := loop.RunWithEvents(context.Background(), "review", func(event Event) {
		if event.Type == EventToolRequested {
			requested++
		}
	})
	if err != nil {
		t.Fatalf("expected bounded finalization repair to succeed: %v", err)
	}
	if result.FinalAnswer != "Two files were reviewed; the main risk is missing validation." {
		t.Fatalf("unexpected repaired final answer %q", result.FinalAnswer)
	}
	if got := len(read.inputs); got != 2 {
		t.Fatalf("finalization tool call must not execute; got %d executions", got)
	}
	if requested != 2 {
		t.Fatalf("finalization tool call must not emit an unmatched tool request; got %d requests", requested)
	}
	if got := client.Calls(); got != 4 {
		t.Fatalf("expected 2 work turns plus finalization and one repair, got %d calls", got)
	}
}

func TestLoopRepairsTextToolProtocolDuringStepBudgetFinalization(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{ToolCall: &llm.ToolCall{ID: "2", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{Content: "<tool_call>search_text<arg_key>path</arg_key>", Done: true}},
		{{Content: "Review complete: one concrete concurrency risk was found.", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	result, err := loop.Run(context.Background(), "review")
	if err != nil {
		t.Fatalf("expected text-protocol repair to succeed: %v", err)
	}
	if result.FinalAnswer != "Review complete: one concrete concurrency risk was found." {
		t.Fatalf("unexpected repaired final answer %q", result.FinalAnswer)
	}
}

func TestLoopAcceptsStepBudgetSummaryThatMentionsToolProtocol(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	answer := "The review found that finalization must reject literal <tool_call> protocol artifacts."
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{ToolCall: &llm.ToolCall{ID: "2", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{Content: answer, Done: true, StopReason: "end_turn"}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	result, err := loop.Run(context.Background(), "review protocol handling")
	if err != nil {
		t.Fatalf("a prose summary that cites protocol text should remain usable: %v", err)
	}
	if result.FinalAnswer != answer {
		t.Fatalf("unexpected final answer %q", result.FinalAnswer)
	}
}

func TestLoopRepairsTruncatedStepBudgetFinalization(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{ToolCall: &llm.ToolCall{ID: "2", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{Content: "Partial rev", Done: true, StopReason: "max_tokens"}},
		{{Content: "Review complete after bounded repair.", Done: true, StopReason: "end_turn"}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	result, err := loop.Run(context.Background(), "review")
	if err != nil {
		t.Fatalf("expected truncated finalization repair to succeed: %v", err)
	}
	if result.FinalAnswer != "Review complete after bounded repair." {
		t.Fatalf("unexpected repaired final answer %q", result.FinalAnswer)
	}
}

func TestLoopRejectsUnusableStepBudgetFinalization(t *testing.T) {
	tests := []struct {
		name       string
		toolOutput string
		invalid    string
	}{
		{name: "tool protocol", toolOutput: "file content", invalid: "<tool_call>read_file<arg_key>path</arg_key>"},
		{name: "raw tool result", toolOutput: "file content", invalid: "file content"},
		{name: "raw tool result with normalized newlines", toolOutput: "package source\r\nline two", invalid: "package source\nline two"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := tool.NewRegistry()
			if err := registry.Register(&recordingTool{name: "read_file", output: tt.toolOutput}); err != nil {
				t.Fatal(err)
			}
			client := llmfake.New([][]llm.Chunk{
				{{ToolCall: &llm.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}, Done: true}},
				{{ToolCall: &llm.ToolCall{ID: "2", Name: "read_file", Arguments: `{}`}, Done: true}},
				{{Content: tt.invalid, Done: true}},
				{{Content: tt.invalid, Done: true}},
			})
			loop := NewLoop(LoopConfig{MaxSteps: 2}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

			result, err := loop.Run(context.Background(), "review")
			if err == nil {
				t.Fatalf("expected unusable finalization to fail, got answer %q", result.FinalAnswer)
			}
			if result.FinalAnswer != "" {
				t.Fatalf("unusable finalization must not become a final answer: %q", result.FinalAnswer)
			}
			if strings.Contains(strings.ToLower(err.Error()), "returning the latest available tool result") {
				t.Fatalf("raw-tool fallback must be removed, got %v", err)
			}
			if got := client.Calls(); got != 4 {
				t.Fatalf("finalization repair must be bounded to one retry, got %d calls", got)
			}
		})
	}
}

type riskTool struct {
	risk     tool.RiskLevel
	executed bool
}

func (r *riskTool) Name() string        { return "risk_tool" }
func (r *riskTool) Description() string { return "risk tool" }
func (r *riskTool) Schema() any         { return map[string]any{"type": "object"} }
func (r *riskTool) Risk(json.RawMessage) tool.RiskLevel {
	return r.risk
}
func (r *riskTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	r.executed = true
	return &tool.Result{ToolName: r.Name(), Output: "ok", OK: true}, nil
}

func TestLoopAppendsToolResultToModelContext(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-read", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	if _, err := loop.Run(context.Background(), "read"); err != nil {
		t.Fatal(err)
	}
	messages := client.LastMessages()
	if len(messages) == 0 {
		t.Fatal("expected fake client to capture messages")
	}
	previous := messages[len(messages)-2]
	if previous.Role != llm.RoleAssistant || len(previous.ToolCalls) != 1 || previous.ToolCalls[0].ID != "call-read" {
		t.Fatalf("expected assistant tool call before tool result, got %#v", previous)
	}
	last := messages[len(messages)-1]
	if last.Role != llm.RoleTool || last.ToolCallID != "call-read" {
		t.Fatalf("expected tool result in model context, got %#v", last)
	}
	var envelope tool.Result
	if err := json.Unmarshal([]byte(last.Content), &envelope); err != nil {
		t.Fatalf("expected JSON tool result envelope, got %#v: %v", last, err)
	}
	if envelope.Status != "success" || envelope.Output != "file content" {
		t.Fatalf("unexpected tool result envelope: %#v", envelope)
	}
}

func TestLoopExecutesAllToolCallsFromOneModelResponseInOrder(t *testing.T) {
	first := &recordingTool{name: "first_tool", output: "first output"}
	second := &recordingTool{name: "second_tool", output: "second output"}
	registry := tool.NewRegistry()
	for _, candidate := range []tool.Tool{first, second} {
		if err := registry.Register(candidate); err != nil {
			t.Fatal(err)
		}
	}
	client := llmfake.New([][]llm.Chunk{
		{{
			ToolCall: &llm.ToolCall{ID: "call-first", Name: "first_tool", Arguments: `{"value":"one"}`},
		}, {
			ToolCall: &llm.ToolCall{ID: "call-second", Name: "second_tool", Arguments: `{"value":"two"}`},
			Done:     true,
		}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	result, err := loop.Run(context.Background(), "use tools")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("unexpected final answer %q", result.FinalAnswer)
	}
	if !reflect.DeepEqual(first.inputs, []string{`{"value":"one"}`}) {
		t.Fatalf("unexpected first tool inputs: %#v", first.inputs)
	}
	if !reflect.DeepEqual(second.inputs, []string{`{"value":"two"}`}) {
		t.Fatalf("unexpected second tool inputs: %#v", second.inputs)
	}

	messages := client.LastMessages()
	if len(messages) < 4 {
		t.Fatalf("expected model context with assistant and tool results, got %#v", messages)
	}
	assistant := messages[len(messages)-3]
	if assistant.Role != llm.RoleAssistant || len(assistant.ToolCalls) != 2 {
		t.Fatalf("expected assistant message with both tool calls, got %#v", assistant)
	}
	if assistant.ToolCalls[0].ID != "call-first" || assistant.ToolCalls[1].ID != "call-second" {
		t.Fatalf("assistant tool calls out of order: %#v", assistant.ToolCalls)
	}
	firstResult := messages[len(messages)-2]
	secondResult := messages[len(messages)-1]
	if firstResult.Role != llm.RoleTool || firstResult.ToolCallID != "call-first" {
		t.Fatalf("unexpected first tool result message: %#v", firstResult)
	}
	if secondResult.Role != llm.RoleTool || secondResult.ToolCallID != "call-second" {
		t.Fatalf("unexpected second tool result message: %#v", secondResult)
	}
	var firstEnvelope tool.Result
	if err := json.Unmarshal([]byte(firstResult.Content), &firstEnvelope); err != nil {
		t.Fatalf("expected first JSON tool result envelope, got %#v: %v", firstResult, err)
	}
	var secondEnvelope tool.Result
	if err := json.Unmarshal([]byte(secondResult.Content), &secondEnvelope); err != nil {
		t.Fatalf("expected second JSON tool result envelope, got %#v: %v", secondResult, err)
	}
	if firstEnvelope.Output != "first output" || secondEnvelope.Output != "second output" {
		t.Fatalf("unexpected envelopes: %#v %#v", firstEnvelope, secondEnvelope)
	}
}

func TestLoopReturnsUnknownRejectedAndFailedToolResultsToModelContext(t *testing.T) {
	rejected := &riskTool{risk: tool.RiskHigh}
	failing := &fakeReadToolWithError{}
	registry := tool.NewRegistry()
	for _, candidate := range []tool.Tool{rejected, failing} {
		if err := registry.Register(candidate); err != nil {
			t.Fatal(err)
		}
	}
	client := llmfake.New([][]llm.Chunk{
		{{
			ToolCall: &llm.ToolCall{ID: "call-unknown", Name: "missing_tool", Arguments: `{}`},
		}, {
			ToolCall: &llm.ToolCall{ID: "call-rejected", Name: rejected.Name(), Arguments: `{}`},
		}, {
			ToolCall: &llm.ToolCall{ID: "call-failing", Name: failing.Name(), Arguments: `{}`},
			Done:     true,
		}},
		{{Content: "handled errors", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{RequireConfirmation: true}, safety.StaticConfirmer(false))

	result, err := loop.Run(context.Background(), "handle tool failures")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "handled errors" {
		t.Fatalf("unexpected final answer %q", result.FinalAnswer)
	}
	messages := client.LastMessages()
	if len(messages) < 5 {
		t.Fatalf("expected assistant plus three tool results, got %#v", messages)
	}
	assistant := messages[len(messages)-4]
	if assistant.Role != llm.RoleAssistant || len(assistant.ToolCalls) != 3 {
		t.Fatalf("expected assistant message with all tool calls, got %#v", assistant)
	}
	results := messages[len(messages)-3:]
	want := []struct {
		id     string
		status string
	}{
		{id: "call-unknown", status: "error"},
		{id: "call-rejected", status: "rejected"},
		{id: "call-failing", status: "error"},
	}
	for i, msg := range results {
		if msg.Role != llm.RoleTool || msg.ToolCallID != want[i].id {
			t.Fatalf("tool result %d out of order: %#v", i, msg)
		}
		var envelope tool.Result
		if err := json.Unmarshal([]byte(msg.Content), &envelope); err != nil {
			t.Fatalf("expected JSON tool result envelope, got %#v: %v", msg, err)
		}
		if envelope.Status != want[i].status || envelope.ToolName == "" || envelope.OK {
			t.Fatalf("unexpected envelope for %s: %#v", want[i].id, envelope)
		}
	}
	if rejected.executed {
		t.Fatal("expected rejected tool not to execute")
	}
}

func TestLoopReturnsToolErrorToModelContext(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadToolWithError{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-bad", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{Content: "tool arguments were invalid", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	result, err := loop.Run(context.Background(), "read")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "tool arguments were invalid" {
		t.Fatalf("unexpected final answer %q", result.FinalAnswer)
	}
	messages := client.LastMessages()
	last := messages[len(messages)-1]
	if last.Role != llm.RoleTool {
		t.Fatalf("expected tool error returned to model, got %#v", last)
	}
	var envelope tool.Result
	if err := json.Unmarshal([]byte(last.Content), &envelope); err != nil {
		t.Fatalf("expected JSON tool result envelope, got %#v: %v", last, err)
	}
	if envelope.Status != "error" || !strings.Contains(envelope.Error, "missing path") {
		t.Fatalf("unexpected tool error envelope: %#v", envelope)
	}
}

type fakeReadToolWithError struct{}

func (fakeReadToolWithError) Name() string        { return "read_file" }
func (fakeReadToolWithError) Description() string { return "read file" }
func (fakeReadToolWithError) Schema() any         { return map[string]any{"type": "object"} }
func (fakeReadToolWithError) Risk(json.RawMessage) tool.RiskLevel {
	return tool.RiskLow
}
func (fakeReadToolWithError) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	return nil, fmt.Errorf("missing path")
}

type recordingTool struct {
	name   string
	output string
	inputs []string
}

func (r *recordingTool) Name() string        { return r.name }
func (r *recordingTool) Description() string { return r.name }
func (r *recordingTool) Schema() any         { return map[string]any{"type": "object"} }
func (r *recordingTool) Risk(json.RawMessage) tool.RiskLevel {
	return tool.RiskLow
}
func (r *recordingTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	r.inputs = append(r.inputs, string(raw))
	return &tool.Result{ToolName: r.name, Output: r.output, OK: true}, nil
}
