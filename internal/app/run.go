package app

import (
	"context"
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/session"
)

func (a *App) Chat(ctx context.Context, prompt string) (*agent.RunResult, error) {
	return a.RunWithEvents(ctx, prompt, nil)
}

func (a *App) RunWithEvents(ctx context.Context, prompt string, sink agent.EventSink) (*agent.RunResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Attach the live TUI sink for the duration of this turn so subagent events
	// (e.g. orchestrator node activity emitted while a tool blocks the main
	// loop) reach the UI immediately instead of waiting for the next main-loop
	// event.
	a.setLiveSink(sink)
	defer a.setLiveSink(nil)

	messages := a.messagesForPrompt(ctx, prompt)
	if err := a.appendSessionMessage(session.RoleUser, prompt, time.Now()); err != nil {
		return nil, err
	}

	var sessionErr error
	wrappedSink := func(event agent.Event) {
		if event.Type == agent.EventContextMeasured {
			a.recordMeasuredUsage(event)
		}
		a.flushSubagentEvents(&event, sink, &sessionErr)
		if sink != nil {
			sink(event)
		}
		if sessionErr == nil {
			sessionErr = a.appendSessionAgentEvent(event)
		}
	}
	// If the window is filling, summarize the history before this turn so the
	// governor's hard snip rarely has to fire. Summarization failures are
	// non-fatal: a compaction error must not block the turn itself.
	if _, cerr := a.compactHistoryLocked(ctx, wrappedSink, false); cerr == nil {
		messages = a.messagesForPrompt(ctx, prompt)
	}
	result, err := a.Agent.RunMessagesWithEvents(ctx, messages, wrappedSink)
	if result != nil && len(result.Messages) > 0 {
		a.history = append([]llm.Message(nil), result.Messages...)
		// Persist a resume snapshot after every turn that changed history, so a
		// crash or restart can pick the session back up. Failures are non-fatal.
		_ = a.saveHistorySnapshot()
	}
	// B1: automatic memory extraction — fire-and-forget on a detached context so
	// it never blocks the turn or gets cancelled when the request ends. Failures
	// (including a nil extractor) are swallowed silently.
	if a.MemoryExtractor != nil && result != nil {
		msgs := append([]llm.Message(nil), result.Messages...)
		ext := a.MemoryExtractor
		observe.SafeGo("memory-extract", func() {
			_, _ = ext.Extract(context.WithoutCancel(ctx), msgs)
		})
	}
	// B3: LLM "Dream" memory consolidation — fire-and-forget; only runs the LLM
	// pass when active memory count exceeds the threshold. Failures are silent.
	if a.Config != nil && a.Config.Memory.DreamEnabled && a.LLM != nil && a.MemoryStore != nil {
		client := a.LLM
		store := a.MemoryStore
		threshold := a.Config.Memory.DreamThreshold
		observe.SafeGo("memory-dream", func() {
			_, _ = ConsolidateMemories(context.WithoutCancel(ctx), client, store, threshold)
		})
	}
	if err != nil {
		return result, err
	}
	if sessionErr != nil {
		return result, sessionErr
	}
	return result, nil
}
