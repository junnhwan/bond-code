package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/hook"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

// PreToolUse 钩子 block 时，工具不执行、loop 继续用 blocked 结果推理。
func TestLoopPreToolUseHookBlocksTool(t *testing.T) {
	registry := tool.NewRegistry()
	rt := &fakeReadTool{}
	registry.Register(rt)

	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{Name: "read_file", Arguments: `{"path":"a"}`}, Done: true}},
		{{Content: "final answer", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))
	hooks := &hook.Registry{}
	hooks.RegisterPreToolUse(func(context.Context, hook.PreToolUseInput) hook.PreToolUseDecision {
		return hook.PreToolUseDecision{Action: hook.ActionBlock, Reason: "blocked by test hook"}
	})
	loop.SetHooks(hooks)

	result, err := loop.Run(context.Background(), "read a")
	if err != nil {
		t.Fatalf("block should not fail the run: %v", err)
	}
	if rt.executed {
		t.Fatal("tool must NOT execute when PreToolUse hook blocks")
	}
	if result.FinalAnswer != "final answer" {
		t.Fatalf("expected final answer after blocked tool, got %q", result.FinalAnswer)
	}
}

// PreToolUse 钩子 modify 时，改写后的 input 传给工具，工具照常执行。
func TestLoopPreToolUseHookModifiesInput(t *testing.T) {
	registry := tool.NewRegistry()
	rt := &fakeReadTool{}
	registry.Register(rt)

	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{Name: "read_file", Arguments: `{"path":"original"}`}, Done: true}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))
	hooks := &hook.Registry{}
	var seenInput string
	hooks.RegisterPreToolUse(func(ctx context.Context, in hook.PreToolUseInput) hook.PreToolUseDecision {
		seenInput = in.Input
		return hook.PreToolUseDecision{Action: hook.ActionModify, ModifiedInput: `{"path":"modified-by-hook"}`}
	})
	loop.SetHooks(hooks)

	if _, err := loop.Run(context.Background(), "read"); err != nil {
		t.Fatal(err)
	}
	if seenInput != `{"path":"original"}` {
		t.Fatalf("hook should see original input, got %s", seenInput)
	}
	if !rt.executed {
		t.Fatal("tool should execute after modify")
	}
}

// PostToolUse 钩子改写工具输出，回填给模型的是改写后的版本。
func TestLoopPostToolUseHookRewritesOutput(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(&fakeReadTool{})

	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{Name: "read_file", Arguments: `{}`}, Done: true}},
		{{Content: "synthesis", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))
	hooks := &hook.Registry{}
	hooks.RegisterPostToolUse(func(ctx context.Context, in hook.PostToolUseInput) hook.PostToolUseDecision {
		return hook.PostToolUseDecision{ModifiedOutput: "[HOOK] " + in.Output}
	})
	loop.SetHooks(hooks)

	var toolResultOutput string
	if _, err := loop.RunWithEvents(context.Background(), "read", func(e Event) {
		if e.Type == EventToolResult {
			toolResultOutput = e.Output
		}
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolResultOutput, "[HOOK]") {
		t.Fatalf("expected post-tool-use hook to rewrite output, got %q", toolResultOutput)
	}
}
