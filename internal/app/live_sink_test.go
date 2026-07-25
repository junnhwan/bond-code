package app

import (
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
)

// TestLiveSinkForwardsSubagentEventsImmediately reproduces the TUI feedback bug:
// while a long tool (task batch) blocks the main loop, subagent events were
// buffered and only flushed on the next main-loop event, so the UI showed no
// progress. With a live sink attached, recordSubagentEvent must forward now.
func TestLiveSinkForwardsSubagentEventsImmediately(t *testing.T) {
	a := &App{SessionID: "session-1"}

	var forwarded []agent.Event
	live := func(e agent.Event) { forwarded = append(forwarded, e) }
	a.setLiveSink(live)
	defer a.setLiveSink(nil)

	a.recordSubagentEvent(agent.Event{
		Type:       agent.EventSubagentStarted,
		ToolCallID: "node-1",
		Message:    "research node started",
		CreatedAt:  time.Now(),
	})

	if len(forwarded) != 1 {
		t.Fatalf("expected live sink to receive 1 event immediately, got %d", len(forwarded))
	}
	if forwarded[0].ToolCallID != "node-1" {
		t.Fatalf("expected node-1 event, got %#v", forwarded[0])
	}

	// flushSubagentEvents persists buffered events but must NOT re-forward the
	// ones already pushed live (otherwise the TUI renders each node twice).
	var flushed []agent.Event
	flushSink := func(e agent.Event) { flushed = append(flushed, e) }
	a.flushSubagentEvents(nil, flushSink, nil)
	if len(flushed) != 0 {
		t.Fatalf("expected no double-forward on flush, got %d", len(flushed))
	}
}

// TestNoLiveSinkFallsBackToDeferredFlush confirms the legacy path still works:
// without a live sink, events are only delivered when flushSubagentEvents runs.
func TestNoLiveSinkFallsBackToDeferredFlush(t *testing.T) {
	a := &App{SessionID: "session-1"}

	a.recordSubagentEvent(agent.Event{
		Type:       agent.EventSubagentStarted,
		ToolCallID: "node-1",
		Message:    "started",
		CreatedAt:  time.Now(),
	})

	var got []agent.Event
	sink := func(e agent.Event) { got = append(got, e) }
	a.flushSubagentEvents(nil, sink, nil)
	if len(got) != 1 {
		t.Fatalf("expected deferred flush to deliver 1 event, got %d", len(got))
	}
}
