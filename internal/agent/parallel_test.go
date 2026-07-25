package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

// slowReadTool 是一个 RiskLow 的 read_file，Execute 固定 sleep，用于观测是否并发。
type slowReadTool struct {
	delay time.Duration
}

func (s *slowReadTool) Name() string        { return "read_file" }
func (s *slowReadTool) Description() string { return "slow read" }
func (s *slowReadTool) Schema() any         { return map[string]any{"type": "object"} }
func (s *slowReadTool) Risk(json.RawMessage) tool.RiskLevel {
	return tool.RiskLow
}
func (s *slowReadTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	time.Sleep(s.delay)
	var in struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &in)
	return &tool.Result{ToolName: "read_file", Output: "content of " + in.Path, OK: true}, nil
}

// 3 个 parallel-safe 只读工具并发执行（墙钟 ≈ 单次而非 3 倍），结果按 tool_call_id 原序回填。
func TestLoopParallelReadOnlyToolsRunConcurrently(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(&slowReadTool{delay: 120 * time.Millisecond})

	client := llmfake.New([][]llm.Chunk{
		{
			{ToolCall: &llm.ToolCall{ID: "1", Name: "read_file", Arguments: `{"path":"a"}`}},
			{ToolCall: &llm.ToolCall{ID: "2", Name: "read_file", Arguments: `{"path":"b"}`}},
			{ToolCall: &llm.ToolCall{ID: "3", Name: "read_file", Arguments: `{"path":"c"}`}},
		},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	start := time.Now()
	result, err := loop.Run(context.Background(), "read")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	// 3 个 120ms 并发应远小于串行 360ms；留宽裕阈值避开 race/调度抖动。
	if elapsed >= 330*time.Millisecond {
		t.Fatalf("expected concurrent execution (<330ms), took %v — tools may have run serially", elapsed)
	}

	// 每个 tool_call_id 都有对应 tool result，且按原序 [1 2 3]。
	var ids []string
	for _, msg := range result.Messages {
		if msg.Role == llm.RoleTool {
			ids = append(ids, msg.ToolCallID)
		}
	}
	if len(ids) != 3 || ids[0] != "1" || ids[1] != "2" || ids[2] != "3" {
		t.Fatalf("expected 3 ordered tool results [1 2 3], got %v", ids)
	}
}

// 单个工具调用不触发并发路径（len==1），行为与串行一致。
func TestLoopSingleToolCallNotParallelized(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(&fakeReadTool{})

	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{Name: "read_file", Arguments: `{}`}, Done: true}},
		{{Content: "final", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 2}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	result, err := loop.Run(context.Background(), "read")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "final" {
		t.Fatalf("expected final answer, got %q", result.FinalAnswer)
	}
}
