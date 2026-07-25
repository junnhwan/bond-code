package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

// prompt_too_long 一次 → reactive compaction → 第二次成功。
func TestLoopReactiveCompactionOnPromptTooLong(t *testing.T) {
	client := llmfake.NewWithErrors(
		[][]llm.Chunk{nil, {{Content: "recovered answer", Done: true}}},
		[]error{&llm.APIError{StatusCode: 400, Body: "prompt is too long: 1000000 > 200000"}, nil},
	)
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))
	loop.SetContextManager(contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{})), 80)

	initial := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "question"},
	}
	var sawStarted, sawFinished bool
	result, err := loop.RunMessagesWithEvents(context.Background(), initial, func(event Event) {
		if event.Type == EventCompactionStarted && strings.Contains(event.Message, "reactive") {
			sawStarted = true
		}
		if event.Type == EventCompactionFinished && strings.Contains(event.Message, "reactive") {
			sawFinished = true
		}
	})
	if err != nil {
		t.Fatalf("expected recovery from prompt_too_long, got error: %v", err)
	}
	if result.FinalAnswer != "recovered answer" {
		t.Fatalf("expected recovered answer, got %q", result.FinalAnswer)
	}
	if !sawStarted {
		t.Fatal("expected a reactive compaction_started event")
	}
	if !sawFinished {
		t.Fatal("expected a reactive compaction_finished event for TUI divider")
	}
}

// reactive compaction 只做一次：第二次仍 prompt_too_long 则透传终止。
func TestLoopReactiveCompactionOnlyOnceThenPropagates(t *testing.T) {
	client := llmfake.NewWithErrors(
		[][]llm.Chunk{nil, nil},
		[]error{
			&llm.APIError{StatusCode: 400, Body: "prompt is too long"},
			&llm.APIError{StatusCode: 400, Body: "prompt is too long"},
		},
	)
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))
	loop.SetContextManager(contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{})), 80)

	_, err := loop.RunMessagesWithEvents(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "q"},
	}, nil)
	if err == nil {
		t.Fatal("expected error: reactive compact happens once, second prompt_too_long propagates")
	}
}

// 无 context manager 时无法 reactive compact，prompt_too_long 直接透传。
func TestLoopPromptTooLongPropagatesWithoutContextManager(t *testing.T) {
	client := llmfake.NewWithErrors(
		[][]llm.Chunk{nil},
		[]error{&llm.APIError{StatusCode: 400, Body: "prompt is too long"}},
	)
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	_, err := loop.RunMessagesWithEvents(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "q"},
	}, nil)
	if err == nil {
		t.Fatal("expected prompt_too_long to propagate when context manager is absent")
	}
}
