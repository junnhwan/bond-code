package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

func TestMediumRiskToolAutoExecutesWithoutConfirmation(t *testing.T) {
	// Medium risk (file edits, memory, todos, ordinary commands) runs
	// automatically; the confirmer is not consulted, so even a rejecting
	// confirmer must not block it.
	toolUnderTest := &riskTool{risk: tool.RiskMedium}
	result, err := runRiskToolLoop(toolUnderTest, safety.StaticConfirmer(false))
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("expected loop to continue, got %q", result.FinalAnswer)
	}
	if !toolUnderTest.executed {
		t.Fatal("expected medium risk tool to auto-execute without confirmation")
	}
}

func TestAutoApproveConfirmerDoesNotApproveHighRiskTools(t *testing.T) {
	confirmer := safety.AutoApproveConfirmer{MaxRisk: string(tool.RiskMedium)}
	toolUnderTest := &riskTool{risk: tool.RiskHigh}
	result, err := runRiskToolLoop(toolUnderTest, confirmer)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("expected loop to continue after high risk rejection, got %q", result.FinalAnswer)
	}
	if toolUnderTest.executed {
		t.Fatal("expected high risk tool not to execute")
	}
	if !traceHasToolError(result.Trace.Events, "risk_tool", "rejected") {
		t.Fatalf("expected rejected high risk tool result, got %#v", result.Trace.Events)
	}
}

func TestAutoApproveConfirmerFallsBackForHighRiskTools(t *testing.T) {
	fallback := &recordingConfirmer{approve: true}
	confirmer := safety.AutoApproveConfirmer{MaxRisk: string(tool.RiskMedium), Fallback: fallback}
	toolUnderTest := &riskTool{risk: tool.RiskHigh}
	_, err := runRiskToolLoop(toolUnderTest, confirmer)
	if err != nil {
		t.Fatal(err)
	}
	if fallback.last.Risk != string(tool.RiskHigh) {
		t.Fatalf("expected high risk confirmation to use fallback, got %#v", fallback.last)
	}
	if !toolUnderTest.executed {
		t.Fatal("expected fallback-approved high risk tool to execute")
	}
}

func TestAutoApproveConfirmerDoesNotBypassBlockedTools(t *testing.T) {
	toolUnderTest := &blockedRiskTool{riskTool: riskTool{risk: tool.RiskHigh}}
	registry := tool.NewRegistry()
	if err := registry.Register(toolUnderTest); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-blocked", Name: toolUnderTest.Name(), Arguments: `{"command":"rm -rf /"}`}, Done: true}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(
		LoopConfig{MaxSteps: 3},
		client,
		registry,
		safety.Policy{RequireConfirmation: true, BlockedSubstrings: []string{"rm -rf /"}},
		safety.AutoApproveConfirmer{MaxRisk: string(tool.RiskMedium), Fallback: safety.StaticConfirmer(true)},
	)

	result, err := loop.Run(context.Background(), "blocked")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("expected loop to continue after blocked tool result, got %q", result.FinalAnswer)
	}
	if toolUnderTest.executed {
		t.Fatal("expected blocked tool not to execute")
	}
	if !traceHasToolError(result.Trace.Events, "risk_tool", "blocked") {
		t.Fatalf("expected blocked tool error result, got %#v", result.Trace.Events)
	}
}

func TestHighRiskToolRequiresHighConfirmation(t *testing.T) {
	confirmer := &recordingConfirmer{approve: true}
	toolUnderTest := &riskTool{risk: tool.RiskHigh}
	_, err := runRiskToolLoop(toolUnderTest, confirmer)
	if err != nil {
		t.Fatal(err)
	}
	if confirmer.last.Risk != string(tool.RiskHigh) {
		t.Fatalf("expected high risk confirmation, got %#v", confirmer.last)
	}
}

func runRiskToolLoop(t tool.Tool, confirmer safety.Confirmer) (*RunResult, error) {
	registry := tool.NewRegistry()
	if err := registry.Register(t); err != nil {
		return nil, err
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-risk", Name: t.Name(), Arguments: `{}`}, Done: true}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{RequireConfirmation: true}, confirmer)
	return loop.Run(context.Background(), "risk")
}

type recordingConfirmer struct {
	approve bool
	last    safety.ConfirmationRequest
}

type blockedRiskTool struct {
	riskTool
}

func (r *recordingConfirmer) Confirm(ctx context.Context, req safety.ConfirmationRequest) (bool, error) {
	r.last = req
	return r.approve, nil
}

func traceHasToolError(events []Event, toolName string, want string) bool {
	for _, event := range events {
		if event.Type == EventToolResult && event.ToolName == toolName && strings.Contains(event.Error, want) {
			return true
		}
	}
	return false
}

// TestDisabledToolsAreBlockedBeforeExecution verifies plan-mode tool disabling:
// a disabled tool is rejected before it runs (even at low risk / auto-approve),
// surfaces a "disabled in plan mode" error, and never executes.
func TestDisabledToolsAreBlockedBeforeExecution(t *testing.T) {
	toolUnderTest := &riskTool{risk: tool.RiskLow}
	registry := tool.NewRegistry()
	if err := registry.Register(toolUnderTest); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-disabled", Name: toolUnderTest.Name(), Arguments: `{}`}, Done: true}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))
	loop.SetDisabledTools([]string{toolUnderTest.Name()})

	result, err := loop.Run(context.Background(), "disabled")
	if err != nil {
		t.Fatal(err)
	}
	if toolUnderTest.executed {
		t.Fatal("expected disabled tool not to execute")
	}
	if !traceHasToolError(result.Trace.Events, toolUnderTest.Name(), "disabled in plan mode") {
		t.Fatalf("expected disabled-in-plan-mode tool error, got %#v", result.Trace.Events)
	}
}

// TestSetDisabledToolsClearsOnEmpty confirms that passing nil/empty re-enables
// tools, so leaving plan mode restores normal execution.
func TestSetDisabledToolsClearsOnEmpty(t *testing.T) {
	toolUnderTest := &riskTool{risk: tool.RiskLow}
	registry := tool.NewRegistry()
	if err := registry.Register(toolUnderTest); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-1", Name: toolUnderTest.Name(), Arguments: `{}`}, Done: true}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))
	loop.SetDisabledTools([]string{toolUnderTest.Name()})
	loop.SetDisabledTools(nil)

	if _, err := loop.Run(context.Background(), "re-enable"); err != nil {
		t.Fatal(err)
	}
	if !toolUnderTest.executed {
		t.Fatal("expected tool to run again after clearing disabled set")
	}
}
