package app

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
)

func TestCompactReplacesHistoryWithAISummary(t *testing.T) {
	client := llm.NewFakeClient([]llm.Chunk{{Content: "## Goal\nShip files", Done: true}})
	summaryStore := contextx.NewSummaryStore(t.TempDir(), "session-1")
	application := &App{
		LLM:              client,
		ContextSummary:   summaryStore,
		MaxContextTokens: 100000,
		ContextManager: contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{
			AutoCompact:      true,
			MaxTokens:        100000,
			KeepRecentTokens: 200, // force a cut over short turns
			ReserveTokens:    1000,
		})),
	}
	for i := 0; i < 30; i++ {
		application.history = append(application.history,
			llm.Message{Role: llm.RoleUser, Content: strings.Repeat("turn content ", 20) + string(rune('a'+i%26))},
			llm.Message{Role: llm.RoleAssistant, Content: strings.Repeat("assistant reply ", 20)},
		)
	}
	beforeLen := len(application.history)

	var events []agent.Event
	result, err := application.compactHistoryLocked(context.Background(), func(e agent.Event) { events = append(events, e) }, true)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if !result.Compacted {
		t.Fatalf("expected Compacted=true, got %#v", result)
	}
	if got := len(application.history); got >= beforeLen {
		t.Fatalf("expected history to shrink, before=%d after=%d", beforeLen, got)
	}
	if result.AfterTokens >= result.BeforeTokens {
		t.Fatalf("expected compaction to reduce tokens, before=%d after=%d", result.BeforeTokens, result.AfterTokens)
	}
	foundSummary := false
	for _, msg := range application.history {
		if msg.Role == llm.RoleUser && strings.Contains(msg.Content, "compacted into the following summary") {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Fatalf("expected compact summary message in history, got %#v", application.history)
	}

	artifact, err := summaryStore.Load()
	if err != nil {
		t.Fatalf("load summary: %v", err)
	}
	if artifact == nil || !strings.Contains(artifact.Summary, "Goal") {
		t.Fatalf("expected persisted AI summary, got %#v", artifact)
	}

	if !hasEvent(events, agent.EventCompactionStarted) || !hasEvent(events, agent.EventCompactionFinished) {
		t.Fatalf("expected started+finished events, got %+v", eventTypes(events))
	}
}

func TestCompactNoopBelowThreshold(t *testing.T) {
	client := llm.NewFakeClient([]llm.Chunk{{Content: "should not be used", Done: true}})
	application := &App{
		LLM:              client,
		MaxContextTokens: 100000,
		ContextManager: contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{
			AutoCompact:   true,
			MaxTokens:     100000,
			ReserveTokens: 16384,
		})),
		history: []llm.Message{
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, Content: "hello"},
		},
	}
	result, err := application.compactHistoryLocked(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Compacted {
		t.Fatalf("expected no compaction below threshold, got %#v", result)
	}
}

func TestCompactFoldsPreviousSummary(t *testing.T) {
	client := llm.NewFakeClient([]llm.Chunk{{Content: "## Goal\nv2", Done: true}})
	application := &App{
		LLM:              client,
		MaxContextTokens: 100000,
		ContextSummary:   contextx.NewSummaryStore(t.TempDir(), "session-1"),
		ContextManager: contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{
			AutoCompact:      true,
			MaxTokens:        100000,
			KeepRecentTokens: 50,
		})),
	}
	_ = application.ContextSummary.Save(contextx.SummaryArtifact{Version: 2, Summary: "previous summary text"})
	for i := 0; i < 20; i++ {
		application.history = append(application.history,
			llm.Message{Role: llm.RoleUser, Content: strings.Repeat("do more ", 30)},
			llm.Message{Role: llm.RoleAssistant, Content: strings.Repeat("working ", 30)},
		)
	}
	if _, err := application.compactHistoryLocked(context.Background(), nil, true); err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	sent := client.LastMessages()
	if len(sent) == 0 {
		t.Fatalf("expected summarization call, got none")
	}
	joined := ""
	for _, m := range sent {
		joined += m.Content
	}
	if !strings.Contains(joined, "previous summary text") {
		t.Fatalf("expected previous summary folded into prompt, got %q", joined)
	}
}

func hasEvent(events []agent.Event, typ agent.EventType) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func eventTypes(events []agent.Event) []agent.EventType {
	out := make([]agent.EventType, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}
