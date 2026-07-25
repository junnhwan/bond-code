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

// steering channel 有值时，step 循环每轮非阻塞读并注入为 user message。
func TestLoopSteeringInjectsMidRun(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(&fakeReadTool{})
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{Name: "read_file", Arguments: `{}`}, Done: true}},
		{{Content: "final", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))
	steering := make(chan string, 1)
	steering <- "focus on tests"
	loop.SetSteering(steering)

	result, err := loop.Run(context.Background(), "do work")
	if err != nil {
		t.Fatal(err)
	}
	var sawSteering bool
	for _, msg := range result.Messages {
		if msg.Role == llm.RoleUser && strings.Contains(msg.Content, "[steering]") {
			sawSteering = true
		}
	}
	if !sawSteering {
		t.Fatal("expected steering message injected into the message stream")
	}
}

// steering channel 为空时，非阻塞读不阻塞 loop。
func TestLoopSteeringEmptyChannelDoesNotBlock(t *testing.T) {
	client := llmfake.New([][]llm.Chunk{
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))
	loop.SetSteering(make(chan string)) // 无缓冲、无值：select default 不阻塞

	if _, err := loop.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
}
