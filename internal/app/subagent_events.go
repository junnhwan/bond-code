package app

import (
	"strings"

	"github.com/junnhwan/bond-code/internal/agent"
)

type subagentRuntimeState struct {
	active bool
	latest string
}

// bufferedSubagentEvent is a subagent event waiting to be persisted to the
// session audit at the next main-loop event. Forwarded records were already
// pushed to the live TUI sink when recorded, so flushSubagentEvents must not
// forward them again (only persist).
type bufferedSubagentEvent struct {
	event     agent.Event
	forwarded bool
}

func (a *App) recordSubagentEvent(event agent.Event) {
	a.subagentMu.Lock()
	a.updateSubagentRuntimeStateLocked(event)
	live := a.liveSink
	// Buffer for session-audit persistence (flushed on the next main-loop
	// event, since JSONLStore.Append is not concurrency-safe and must stay on
	// the main goroutine). When a live TUI sink is attached, also forward now
	// so progress is visible even while a long tool (e.g. task batch) blocks
	// the main loop inside executeTool; mark it forwarded so the flush does not
	// send it twice.
	a.subagentEvents = append(a.subagentEvents, bufferedSubagentEvent{event: event, forwarded: live != nil})
	a.subagentMu.Unlock()
	if live != nil {
		live(event)
	}
}

// setLiveSink attaches (or detaches, with nil) the current turn's live event
// sink so recordSubagentEvent can forward subagent events to the UI in real
// time. Guarded by subagentMu so concurrent node goroutines read a consistent
// value.
func (a *App) setLiveSink(sink agent.EventSink) {
	a.subagentMu.Lock()
	a.liveSink = sink
	a.subagentMu.Unlock()
}

func (a *App) updateSubagentRuntimeStateLocked(event agent.Event) {
	latest := strings.TrimSpace(firstNonEmpty(event.Message, event.Error, event.Output))
	if latest != "" {
		a.subagentLatest = latest
	}
	taskID := strings.TrimSpace(event.ToolCallID)
	if taskID == "" {
		return
	}
	if a.subagentTasks == nil {
		a.subagentTasks = map[string]subagentRuntimeState{}
	}
	state := a.subagentTasks[taskID]
	state.latest = latest
	switch event.Type {
	case agent.EventSubagentStarted, agent.EventSubagentProgress:
		state.active = true
	case agent.EventSubagentFinished, agent.EventSubagentFailed, agent.EventSubagentCancelled:
		state.active = false
	}
	a.subagentTasks[taskID] = state
}

func (a *App) flushSubagentEvents(before *agent.Event, sink agent.EventSink, sessionErr *error) {
	a.subagentMu.Lock()
	if len(a.subagentEvents) == 0 {
		a.subagentMu.Unlock()
		return
	}
	entries := append([]bufferedSubagentEvent(nil), a.subagentEvents...)
	a.subagentEvents = nil
	a.subagentMu.Unlock()
	for _, entry := range entries {
		event := entry.event
		if before != nil && event.CreatedAt.After(before.CreatedAt) && !before.CreatedAt.IsZero() {
			event.CreatedAt = before.CreatedAt
		}
		// Only forward to the turn sink if it was not already pushed live when
		// recorded; otherwise the TUI would render each subagent event twice.
		if !entry.forwarded && sink != nil {
			sink(event)
		}
		if sessionErr != nil && *sessionErr == nil {
			*sessionErr = a.appendSessionAgentEvent(event)
		}
	}
}
