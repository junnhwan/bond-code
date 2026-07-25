package agent

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

// summarizeHistoryWithLLM 调一次模型返回五段摘要；这是 L4 的核心调用。
func TestLoopSummarizeHistoryWithLLM(t *testing.T) {
	client := llm.NewFakeClient([]llm.Chunk{{Content: "compacted: goal set, 2 files modified", Done: true}})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))

	out, err := loop.summarizeHistoryWithLLM(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "do something"},
	}, "")
	if err != nil {
		t.Fatalf("summarizeHistoryWithLLM failed: %v", err)
	}
	if out != "compacted: goal set, 2 files modified" {
		t.Fatalf("expected LLM-generated summary, got %q", out)
	}
}

// llmSummaryEnabled 默认 false：不调 LLM 摘要，走规则截断，client 的 Response 序列不被摘要消费。
func TestLoopLLMSummaryDisabledDoesNotConsumeClient(t *testing.T) {
	client := llmfake.New([][]llm.Chunk{
		{{Content: "final", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))
	// 不调 SetLLMSummaryEnabled（默认 false）。

	result, err := loop.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "final" {
		t.Fatalf("expected final, got %q", result.FinalAnswer)
	}
}
