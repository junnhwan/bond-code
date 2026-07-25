package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

type concurrencyProbe struct {
	current int32
	maximum int32
}

type probedTool struct {
	name  string
	probe *concurrencyProbe
}

func (t *probedTool) Name() string        { return t.name }
func (t *probedTool) Description() string { return "concurrency probe" }
func (t *probedTool) Schema() any         { return map[string]any{"type": "object"} }
func (t *probedTool) Risk(json.RawMessage) tool.RiskLevel {
	return tool.RiskLow
}
func (t *probedTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	current := atomic.AddInt32(&t.probe.current, 1)
	for {
		maximum := atomic.LoadInt32(&t.probe.maximum)
		if current <= maximum || atomic.CompareAndSwapInt32(&t.probe.maximum, maximum, current) {
			break
		}
	}
	time.Sleep(30 * time.Millisecond)
	atomic.AddInt32(&t.probe.current, -1)
	return &tool.Result{ToolName: t.name, Output: t.name + " done", OK: true}, nil
}

func TestExecuteAndReplayToolsKeepsMixedBatchSerialAndOrdered(t *testing.T) {
	probe := &concurrencyProbe{}
	registry := tool.NewRegistry()
	for _, name := range []string{"read_file", "write_file"} {
		if err := registry.Register(&probedTool{name: name, probe: probe}); err != nil {
			t.Fatal(err)
		}
	}
	loop := NewLoop(LoopConfig{}, llm.NewFakeClient(nil), registry, safety.Policy{}, safety.StaticConfirmer(true))
	calls := []llm.ToolCall{
		{ID: "read-1", Name: "read_file", Arguments: `{}`},
		{ID: "write-1", Name: "write_file", Arguments: `{}`},
	}

	outcome, err := loop.executeAndReplayTools(context.Background(), nil, calls, newLoopGuard(loop.cfg), func(Event) {}, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&probe.maximum); got != 1 {
		t.Fatalf("maximum concurrent executions = %d, want 1 for a mixed safe/unsafe batch", got)
	}
	if len(outcome.messages) != 2 {
		t.Fatalf("tool result message count = %d, want 2", len(outcome.messages))
	}
	for i, wantID := range []string{"read-1", "write-1"} {
		if outcome.messages[i].Role != llm.RoleTool || outcome.messages[i].ToolCallID != wantID {
			t.Fatalf("result %d = %#v, want ordered tool result for %q", i, outcome.messages[i], wantID)
		}
	}
}
