package tui

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/observe"
)

func (m Model) startAgent(prompt string) (Model, tea.Cmd) {
	if m.agent.Busy {
		// Defensive: Submit queues before calling startAgent, so a busy agent
		// should never reach here. Park the prompt anyway rather than drop it.
		m.agent.QueuedPrompts = append(m.agent.QueuedPrompts, prompt)
		return m, nil
	}
	m.agent.Busy = true
	m.agent.Err = nil
	m.agent.LiveStream = nil
	m.agent.LiveDetail = ""
	m.agent.TerminalHandled = false
	m.agent.RunGeneration++
	runGeneration := m.agent.RunGeneration
	m.timeline = m.timeline.MarkAgentStarted(time.Now())
	ctx, cancel := context.WithCancel(m.cfg.Context)
	m.agent.Cancel = cancel
	stream := make(chan tea.Msg, 32)
	go func() {
		defer cancel()
		defer close(stream)
		// Recover panics from the ReAct loop (and every goroutine nested under
		// it: tools, LLM transport, subagents) and route them to the existing
		// agentDoneMsg error path so the TUI shows a normal error block instead
		// of crashing. A main-level recover cannot reach this goroutine.
		var err error
		defer func() {
			if r := recover(); r != nil {
				observe.LogPanic("agent", r, debug.Stack())
				err = fmt.Errorf("internal error: %v", r)
			}
			sendAgentMsg(ctx, stream, agentDoneMsg{err: err, runGeneration: runGeneration})
		}()
		_, err = m.cfg.Chat.RunWithEvents(ctx, prompt, func(event agent.Event) {
			sendAgentMsg(ctx, stream, agentEventMsg{event: event, runGeneration: runGeneration})
		})
	}()
	m.agent.Stream = stream
	return m, m.waitForAgent()
}

// startCompact runs /compact as an async operation: it summarizes the
// conversation history via the model, streaming EventCompactionStarted/
// Finished back through the same channel as a normal agent turn so the spinner
// animates and the result lands in the timeline.
func (m Model) startCompact() (Model, tea.Cmd) {
	if m.agent.Busy {
		body := "agent is already running"
		m.timeline = m.timeline.AppendBlock(BlockError, "compact", body)
		return m.markNewOutputBelow(), nil
	}
	if m.cfg.Chat == nil {
		body := "agent is not configured"
		m.timeline = m.timeline.AppendBlock(BlockError, "compact", body)
		return m.markNewOutputBelow(), nil
	}
	m.agent.Busy = true
	m.agent.Err = nil
	m.agent.LiveStream = nil
	m.agent.LiveDetail = ""
	m.agent.TerminalHandled = false
	m.agent.RunGeneration++
	runGeneration := m.agent.RunGeneration
	m.timeline = m.timeline.MarkAgentStarted(time.Now())
	ctx, cancel := context.WithCancel(m.cfg.Context)
	m.agent.Cancel = cancel
	stream := make(chan tea.Msg, 32)
	go func() {
		defer cancel()
		defer close(stream)
		var err error
		defer func() {
			if r := recover(); r != nil {
				observe.LogPanic("compact", r, debug.Stack())
				err = fmt.Errorf("internal error: %v", r)
			}
			sendAgentMsg(ctx, stream, agentDoneMsg{err: err, runGeneration: runGeneration})
		}()
		err = m.cfg.Chat.Compact(ctx, func(event agent.Event) {
			sendAgentMsg(ctx, stream, agentEventMsg{event: event, runGeneration: runGeneration})
		})
	}()
	m.agent.Stream = stream
	// Batch the spinner tick so the "compacting context" status animates;
	// startAgent gets this via the runAgentMsg handler, but /compact skips it.
	// Mark armed so mouse motion cannot stack a parallel Tick chain.
	m.animTickArmed = true
	return m, tea.Batch(m.waitForAgent(), m.spinner.Tick)
}

func (m Model) waitForAgent() tea.Cmd {
	if m.agent.Stream == nil {
		return nil
	}
	return waitForAgentEvent(m.agent.Stream, m.agent.RunGeneration)
}

func (m *Model) stopAgent() {
	if m.agent.Cancel != nil {
		m.agent.Cancel()
		m.agent.Cancel = nil
	}
}

func sendAgentMsg(ctx context.Context, stream chan<- tea.Msg, msg tea.Msg) {
	select {
	case stream <- msg:
	case <-ctx.Done():
	}
}
