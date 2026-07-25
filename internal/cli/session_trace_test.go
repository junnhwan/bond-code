package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/session"
)

func TestRunSessionTraceSummarizesTurnsAndAnomalies(t *testing.T) {
	events := []session.Event{
		{AgentEvent: &session.AgentEvent{Type: "agent_started"}},
		{AgentEvent: &session.AgentEvent{Type: "tool_requested", ToolName: "read_file"}},
		{AgentEvent: &session.AgentEvent{Type: "tool_requested", ToolName: "search_text"}},
		{AgentEvent: &session.AgentEvent{Type: "tool_rejected", ToolName: "run_command", Message: "disabled in plan mode"}},
		{AgentEvent: &session.AgentEvent{Type: "agent_finished", Message: "here is the plan"}},
		// second turn never finishes and trips the guard
		{AgentEvent: &session.AgentEvent{Type: "agent_started"}},
		{AgentEvent: &session.AgentEvent{Type: "tool_requested", ToolName: "read_file"}},
		{AgentEvent: &session.AgentEvent{Type: "loop_guard", ToolName: "read_file", Message: "repeated identical tool call blocked"}},
	}
	var buf bytes.Buffer
	runSessionTrace(&buf, "sess-1", events, nil)
	out := buf.String()

	for _, want := range []string{
		"sess-1 · 2 turns",
		"turn 1", "done",
		"turn 2", "no-finish",
		"read_file×1  search_text×1",
		"rejected: run_command — disabled in plan mode",
		"loop_guard: read_file",
		"tool stats: read_file 2  search_text 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("trace output missing %q:\n%s", want, out)
		}
	}
}

// TestRunSessionTraceAugmentsTurnsWithDebug: when --debug records are present and
// their segments line up 1:1 with audit turns, each turn gets an inline
// model-decision line showing the cache hit rate, token totals, and tool
// risk/decision — the model-decision layer folded into the turn view.
func TestRunSessionTraceAugmentsTurnsWithDebug(t *testing.T) {
	events := []session.Event{
		{AgentEvent: &session.AgentEvent{Type: "agent_started"}},
		{AgentEvent: &session.AgentEvent{Type: "tool_requested", ToolName: "read_file"}},
		{AgentEvent: &session.AgentEvent{Type: "agent_finished", Message: "done"}},
	}
	// One debug segment (one model call) for the one turn.
	debug := []observe.Record{
		{T: "llm_req", Step: 0, Model: "glm-5.1", MsgCount: 4, TotalBytes: 9000, Tools: 14},
		{T: "decide", Step: 0, Kind: "context", Detail: "kept 10 recent, snipped 2"},
		{T: "llm_resp", Step: 0, TextBytes: 120, Usage: &observe.UsageRec{In: 12000, Out: 120, CacheRead: 9000, CacheCreate: 0},
			ToolCalls: []observe.ToolCallRec{{Name: "read_file", ArgsBytes: 40}}},
		{T: "tool", Step: 0, Name: "read_file", Risk: "low", Decision: "allow", Approved: true, DurMs: 12, OutBytes: 500},
	}
	var buf bytes.Buffer
	runSessionTrace(&buf, "sess-debug", events, debug)
	out := buf.String()

	for _, want := range []string{
		"sess-debug · 1 turns",
		"debug: 1 model call(s)", // the inline augmentation
		"9.0k/12.0k in (75%)",    // cache hit rate
		"tools: read_file(low:allow ok)",
		"decide[context]: kept 10 recent",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("debug-augmented trace missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "model decisions (--debug)") {
		t.Fatalf("1:1 alignment should inline, not print a fallback section:\n%s", out)
	}
}

// TestRunSessionTraceFallbackSectionWhenUnaligned: when the debug segment count
// doesn't match the turn count, the trace must NOT inline-misalign; it prints a
// separate ordered "model decisions" section instead.
func TestRunSessionTraceFallbackSectionWhenUnaligned(t *testing.T) {
	events := []session.Event{
		{AgentEvent: &session.AgentEvent{Type: "agent_started"}},
		{AgentEvent: &session.AgentEvent{Type: "agent_finished", Message: "done"}},
	}
	// Two segments but one turn -> unaligned -> fallback section.
	debug := []observe.Record{
		{T: "llm_req", Step: 0},
		{T: "llm_resp", Step: 0, Usage: &observe.UsageRec{In: 100, Out: 10}},
		{T: "llm_req", Step: 0},
		{T: "llm_resp", Step: 0, Usage: &observe.UsageRec{In: 200, Out: 20}},
	}
	var buf bytes.Buffer
	runSessionTrace(&buf, "sess-x", events, debug)
	out := buf.String()

	if !strings.Contains(out, "model decisions (--debug)") {
		t.Fatalf("expected fallback section for unaligned debug records:\n%s", out)
	}
	if !strings.Contains(out, "[call 1]") || !strings.Contains(out, "[call 2]") {
		t.Fatalf("expected both model calls in the fallback section:\n%s", out)
	}
}

func TestLatestSessionIDErrorsWhenEmpty(t *testing.T) {
	if _, err := latestSessionID(t.TempDir()); err == nil {
		t.Fatal("expected error when no sessions exist")
	}
}
