package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
)

func TestHistoricalThinkingFullyAbsentFromView(t *testing.T) {
	model := NewModel(Config{}).SetSize(100, 40)
	model.agent.Busy = true
	model.showToolDetails = true
	model.showThinking = false
	model.verbose = false
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventAgentStarted})
	model = model.beginUserTurn("please think and act")
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "SECRET_THINK_ALPHA unique marker\n"})
	model = model.ApplyAgentEvent(agent.Event{
		Type: agent.EventToolRequested, ToolName: "read_file", ToolCallID: "c1",
		Input: `{"path":"secret.go"}`,
	})
	// Mid-turn after tool: committed thinking must already be gone from the view.
	mid := ansi.Strip(model.View())
	if strings.Contains(mid, "SECRET_THINK_ALPHA") {
		t.Fatalf("committed thinking still visible mid-turn after tool:\n%s", mid)
	}
	for _, leak := range []string{"thinking ·", "∴ Thinking", "⌥ thinking"} {
		if strings.Contains(mid, leak) {
			t.Fatalf("thinking chrome leaked mid-turn via %q:\n%s", leak, mid)
		}
	}

	model = model.ApplyAgentEvent(agent.Event{
		Type: agent.EventToolResult, ToolName: "read_file", ToolCallID: "c1", Output: "ok",
	})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "SECRET_THINK_BETA another marker\n"})
	// Live second thinking may preview — but alpha (historical) must stay hidden.
	liveView := ansi.Strip(model.View())
	if strings.Contains(liveView, "SECRET_THINK_ALPHA") {
		t.Fatalf("historical thinking reappeared while live thinking streams:\n%s", liveView)
	}
	if !strings.Contains(liveView, "SECRET_THINK_BETA") && !strings.Contains(liveView, "Thinking") {
		t.Fatalf("expected live thinking preview:\n%s", liveView)
	}

	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "final visible answer"})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventAgentFinished, Message: "final visible answer"})
	model.agent.Busy = false

	view := ansi.Strip(model.View())
	for _, secret := range []string{"SECRET_THINK_ALPHA", "SECRET_THINK_BETA", "thinking ·", "∴ Thinking", "⌥ thinking"} {
		if strings.Contains(view, secret) {
			t.Fatalf("historical thinking leaked into finished View via %q:\n%s", secret, view)
		}
	}
	if !strings.Contains(view, "final visible answer") && !strings.Contains(view, "secret.go") && !strings.Contains(view, "read_file") {
		t.Fatalf("expected answer or tool still visible:\n%s", view)
	}

	// Even with Ctrl+O verbose + timestamps, historical thinking must stay gone.
	model.verbose = true
	model.showTimestamps = true
	setRenderVerbose(true)
	verboseView := ansi.Strip(model.View())
	for _, secret := range []string{"SECRET_THINK_ALPHA", "SECRET_THINK_BETA", "thinking ·", "∴ Thinking", "⌥ thinking"} {
		if strings.Contains(verboseView, secret) {
			t.Fatalf("verbose mode leaked historical thinking via %q:\n%s", secret, verboseView)
		}
	}

	var found bool
	for _, b := range model.timeline.Turns[len(model.timeline.Turns)-1].Blocks {
		if b.Kind == BlockReasoning && strings.Contains(b.Body, "SECRET_THINK_ALPHA") {
			found = true
		}
	}
	if !found {
		t.Fatal("thinking must remain in data model even when hidden")
	}
}
