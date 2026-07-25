package subagent

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

// TestRunTaskFinalizesAfterBudget reproduces the failure mode seen in the wild:
// a research subagent keeps calling tools until its step budget is spent. Before
// the fix it returned status "failed" with an empty answer, which cascaded into
// every dependent node being skipped. After the fix it gets a bounded no-tools
// finalization (plus at most one targeted repair) and returns a real summary as
// "completed".
func TestRunTaskFinalizesAfterBudget(t *testing.T) {
	// Research profile default budget is 6 steps. Simulate the model requesting
	// a successful tool on each of those 6 steps, then summarizing on the
	// 7th, tool-less finalization call.
	toolCall := llm.Chunk{ToolCall: &llm.ToolCall{ID: "c1", Name: "read_file", Arguments: "{}"}}
	responses := make([][]llm.Chunk, 0, 7)
	for i := 0; i < 6; i++ {
		responses = append(responses, []llm.Chunk{toolCall})
	}
	responses = append(responses, []llm.Chunk{{Content: "found 3 relevant files and one risk", Done: true}})

	client := llmfake.New(responses)
	registry := tool.NewRegistry()
	if err := registry.Register(&spyTool{name: "read_file", output: "fixture output"}); err != nil {
		t.Fatal(err)
	}
	manager := newTestManagerWithOptions(
		client,
		registry,
		ManagerOptions{DefaultTimeoutSeconds: 5},
	)

	result, err := manager.RunTask(context.Background(), TaskRequest{
		Description:  "explore",
		Prompt:       "look around",
		SubagentType: AgentTypeResearch,
		TaskID:       "r1",
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected completed via forced wrap-up, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.FinalAnswer, "found 3 relevant files") {
		t.Fatalf("expected the forced summary as final answer, got %q", result.FinalAnswer)
	}
	if result.Iterations != 7 {
		t.Fatalf("expected 7 iterations (6 tool + 1 wrap-up), got %d", result.Iterations)
	}
}

// TestRunTaskMaxStepsOverrideLetsCallerRaiseBudget confirms the orchestrator's
// path: a caller can request more steps than the profile default.
func TestRunTaskMaxStepsOverrideLetsCallerRaiseBudget(t *testing.T) {
	toolCall := llm.Chunk{ToolCall: &llm.ToolCall{ID: "c1", Name: "read_file", Arguments: "{}"}}
	// 12 tool calls (override) + 1 wrap-up.
	responses := make([][]llm.Chunk, 0, 13)
	for i := 0; i < 12; i++ {
		responses = append(responses, []llm.Chunk{toolCall})
	}
	responses = append(responses, []llm.Chunk{{Content: "summary after extended budget", Done: true}})

	client := llmfake.New(responses)
	registry := tool.NewRegistry()
	if err := registry.Register(&spyTool{name: "read_file", output: "fixture output"}); err != nil {
		t.Fatal(err)
	}
	manager := newTestManagerWithOptions(
		client,
		registry,
		ManagerOptions{DefaultTimeoutSeconds: 5},
	)

	result, err := manager.RunTask(context.Background(), TaskRequest{
		Prompt:       "deep dive",
		SubagentType: AgentTypeResearch,
		TaskID:       "r1",
		MaxSteps:     12, // override the profile default of 6
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected completed, got %q (%s)", result.Status, result.Error)
	}
	if result.Iterations != 13 {
		t.Fatalf("expected 13 iterations with the raised budget, got %d", result.Iterations)
	}
}

func TestRunTaskFailsWhenBudgetFinalizationRemainsUnusable(t *testing.T) {
	toolCall := llm.Chunk{ToolCall: &llm.ToolCall{ID: "c1", Name: "read_file", Arguments: "{}"}}
	responses := make([][]llm.Chunk, 0, 8)
	for range 6 {
		responses = append(responses, []llm.Chunk{toolCall})
	}
	invalid := "<tool_call>search_text<arg_key>path</arg_key>"
	responses = append(responses,
		[]llm.Chunk{{Content: invalid, Done: true}},
		[]llm.Chunk{{Content: invalid, Done: true}},
	)

	client := llmfake.New(responses)
	registry := tool.NewRegistry()
	if err := registry.Register(&spyTool{name: "read_file", output: "package rawsource"}); err != nil {
		t.Fatal(err)
	}
	manager := newTestManagerWithOptions(client, registry, ManagerOptions{DefaultTimeoutSeconds: 5})

	result, err := manager.RunTask(context.Background(), TaskRequest{
		Prompt:       "review the implementation",
		SubagentType: AgentTypeResearch,
		TaskID:       "invalid-final",
	})
	if err != nil {
		t.Fatalf("task failures should be represented in the result: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("unusable finalization must fail, got status=%q answer=%q", result.Status, result.FinalAnswer)
	}
	if result.Error == "" {
		t.Fatal("failed finalization should retain a diagnostic error")
	}
	if result.FinalAnswer != "" {
		t.Fatalf("tool protocol text must not be exposed as a final answer: %q", result.FinalAnswer)
	}
	if got := client.Calls(); got != 8 {
		t.Fatalf("expected 6 work calls plus 2 bounded finalization calls, got %d", got)
	}
	if _, ok := manager.loadResumable("invalid-final"); ok {
		t.Fatal("failed finalization history must not become resumable")
	}
}

func TestReadonlyProfileBudgetsRemainBounded(t *testing.T) {
	if got := DefaultAgentProfile(AgentTypeResearch).MaxSteps; got != 6 {
		t.Fatalf("research MaxSteps = %d, want 6", got)
	}
	if got := DefaultAgentProfile(AgentTypeReviewer).MaxSteps; got != 8 {
		t.Fatalf("reviewer MaxSteps = %d, want 8", got)
	}
}
