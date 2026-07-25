package hook

import (
	"context"
	"testing"
)

func TestPreToolUseBlockStopsChain(t *testing.T) {
	r := &Registry{}
	secondCalled := false
	r.RegisterPreToolUse(func(ctx context.Context, in PreToolUseInput) PreToolUseDecision {
		if in.ToolName == "write_file" {
			return PreToolUseDecision{Action: ActionBlock, Reason: "no writes"}
		}
		return PreToolUseDecision{Action: ActionAllow}
	})
	r.RegisterPreToolUse(func(context.Context, PreToolUseInput) PreToolUseDecision {
		secondCalled = true
		return PreToolUseDecision{Action: ActionAllow}
	})
	d, _ := r.RunPreToolUse(context.Background(), PreToolUseInput{ToolName: "write_file", Input: "{}"})
	if !d.IsBlocking() {
		t.Fatal("expected block decision")
	}
	if secondCalled {
		t.Fatal("hooks after a block must not run")
	}
}

func TestPreToolUseModifyRewritesInput(t *testing.T) {
	r := &Registry{}
	r.RegisterPreToolUse(func(ctx context.Context, in PreToolUseInput) PreToolUseDecision {
		return PreToolUseDecision{Action: ActionModify, ModifiedInput: `{"path":"redacted"}`}
	})
	d, input := r.RunPreToolUse(context.Background(), PreToolUseInput{ToolName: "read_file", Input: `{"path":"secret"}`})
	if d.Action != ActionAllow {
		t.Fatalf("expected allow after modify, got %s", d.Action)
	}
	if input != `{"path":"redacted"}` {
		t.Fatalf("expected modified input, got %s", input)
	}
}

func TestPostToolUseRewritesOutput(t *testing.T) {
	r := &Registry{}
	r.RegisterPostToolUse(func(ctx context.Context, in PostToolUseInput) PostToolUseDecision {
		return PostToolUseDecision{ModifiedOutput: "[redacted] " + in.Output}
	})
	out := r.RunPostToolUse(context.Background(), PostToolUseInput{ToolName: "read_file", Output: "secret"})
	if out.Output != "[redacted] secret" {
		t.Fatalf("expected redacted output, got %s", out.Output)
	}
}

func TestUserPromptSubmitRewrites(t *testing.T) {
	r := &Registry{}
	r.RegisterUserPromptSubmit(func(ctx context.Context, in UserPromptSubmitInput) UserPromptSubmitDecision {
		return UserPromptSubmitDecision{ModifiedPrompt: in.Prompt + " (ctx)"}
	})
	out := r.RunUserPromptSubmit(context.Background(), UserPromptSubmitInput{Prompt: "hi"})
	if out.Prompt != "hi (ctx)" {
		t.Fatalf("expected modified prompt, got %s", out.Prompt)
	}
}

func TestStopRunsAll(t *testing.T) {
	r := &Registry{}
	count := 0
	r.RegisterStop(func(context.Context) { count++ })
	r.RegisterStop(func(context.Context) { count++ })
	r.RunStop(context.Background())
	if count != 2 {
		t.Fatalf("expected 2 stop hooks, got %d", count)
	}
}

func TestNilRegistryIsNoOp(t *testing.T) {
	var r *Registry
	r.RegisterPreToolUse(nil) // must not panic
	d, input := r.RunPreToolUse(context.Background(), PreToolUseInput{Input: "{}"})
	if d.Action != ActionAllow || input != "{}" {
		t.Fatal("nil registry RunPreToolUse should be allow passthrough")
	}
	r.RunStop(context.Background())       // must not panic
	r.RunPostToolUse(context.Background(), PostToolUseInput{})
	r.RunUserPromptSubmit(context.Background(), UserPromptSubmitInput{})
}
