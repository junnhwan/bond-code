package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

// collectLogger is a test observe.Logger that records every Log call so a test
// can assert on the model-decision trace emitted by the loop.
type collectLogger struct {
	mu      sync.Mutex
	records []observe.Record
	verbose observe.Verbose
}

func (c *collectLogger) Log(r observe.Record) {
	c.mu.Lock()
	c.records = append(c.records, r)
	c.mu.Unlock()
}
func (c *collectLogger) Close() error             { return nil }
func (c *collectLogger) Verbose() observe.Verbose { return c.verbose }

func TestLoopDebugLoggerRecordsReqRespWithCacheBreakdown(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{Usage: &llm.Usage{InputTokens: 100, OutputTokens: 5, CacheReadInputTokens: 80, CacheCreationInputTokens: 0}, ToolCall: &llm.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{Usage: &llm.Usage{InputTokens: 120, OutputTokens: 4}, Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))
	logger := &collectLogger{verbose: observe.VerboseDefault}
	loop.SetDebugLogger(logger)

	if _, err := loop.Run(context.Background(), "read then stop"); err != nil {
		t.Fatal(err)
	}

	var reqs, resps []observe.Record
	for _, r := range logger.records {
		switch r.T {
		case "llm_req":
			reqs = append(reqs, r)
		case "llm_resp":
			resps = append(resps, r)
		}
	}
	if len(reqs) < 2 {
		t.Fatalf("expected >=2 llm_req records (one per step), got %d: %#v", len(reqs), logger.records)
	}
	if len(resps) < 2 {
		t.Fatalf("expected >=2 llm_resp records, got %d: %#v", len(resps), logger.records)
	}
	// First response carries the requested tool call + the prompt-cache breakdown.
	if len(resps[0].ToolCalls) != 1 || resps[0].ToolCalls[0].Name != "read_file" {
		t.Fatalf("expected read_file tool call in first llm_resp: %#v", resps[0])
	}
	if resps[0].Usage == nil || resps[0].Usage.CacheRead != 80 || resps[0].Usage.In != 100 {
		t.Fatalf("expected cache breakdown preserved on first llm_resp: %#v", resps[0].Usage)
	}
	// Request records carry governed-payload stats.
	if reqs[0].MsgCount == 0 || reqs[0].Payload == "" || reqs[0].Tools == 0 {
		t.Fatalf("expected llm_req to carry msg count, payload and tool count: %#v", reqs[0])
	}
}

func TestLoopDebugLoggerNilIsZeroOverhead(t *testing.T) {
	// A nil debugLogger (the default, when --debug is off) must not change loop
	// behavior or panic.
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llm.NewFakeClient([]llm.Chunk{{Content: "ok", Done: true}})
	loop := NewLoop(LoopConfig{MaxSteps: 1}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))
	// l.debugLogger stays nil.
	res, err := loop.Run(context.Background(), "stop immediately")
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalAnswer != "ok" {
		t.Fatalf("expected normal answer with nil debug logger, got %q", res.FinalAnswer)
	}
}

func TestLoopDebugLoggerRecordsToolAndDecisions(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "1", Name: "read_file", Arguments: `{"path":"x"}`}, Done: true}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))
	// A real context manager (as bootstrap wires) so the context-governance
	// decide record is exercised, not just the safety one.
	loop.SetContextManager(contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{
		MicroCompactKeepRecent: 10,
		ToolResultBudget:       8000,
	})), 100000)
	logger := &collectLogger{verbose: observe.VerboseDefault}
	loop.SetDebugLogger(logger)

	if _, err := loop.Run(context.Background(), "read"); err != nil {
		t.Fatal(err)
	}

	var toolRecs []observe.Record
	var safetyDecides, ctxDecides []observe.Record
	for _, r := range logger.records {
		switch {
		case r.T == "tool":
			toolRecs = append(toolRecs, r)
		case r.T == "decide" && r.Kind == "safety":
			safetyDecides = append(safetyDecides, r)
		case r.T == "decide" && r.Kind == "context":
			ctxDecides = append(ctxDecides, r)
		}
	}
	if len(toolRecs) != 1 || toolRecs[0].Name != "read_file" || !toolRecs[0].Approved || toolRecs[0].Decision != "allow" {
		t.Fatalf("expected one allowed read_file tool record: %#v", toolRecs)
	}
	if len(safetyDecides) == 0 || safetyDecides[0].Name != "read_file" {
		t.Fatalf("expected a safety decide record for read_file: %#v", safetyDecides)
	}
	if len(ctxDecides) == 0 {
		t.Fatalf("expected context decide records when a context manager is wired: %#v", logger.records)
	}
}
