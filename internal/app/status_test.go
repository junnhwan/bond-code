package app

import (
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/memory"
)

func TestStatusSnapshotIncludesCoreRuntimeFields(t *testing.T) {
	a := &App{
		Config:           &config.Config{Model: config.ModelConfig{Model: "fake"}},
		SessionID:        "session-1",
		MaxContextTokens: 100000,
	}
	snap := a.StatusSnapshot()

	if snap.SessionID != "session-1" {
		t.Fatalf("expected session id, got %#v", snap)
	}
	if snap.Model != "fake" {
		t.Fatalf("expected model fake, got %#v", snap)
	}
	if snap.Context.MaxTokens != 100000 {
		t.Fatalf("expected context max tokens, got %#v", snap.Context)
	}
}

func TestStatusSnapshotIncludesCumulativeMeasuredUsage(t *testing.T) {
	a := &App{}
	a.recordMeasuredUsage(agent.Event{
		Type:                 agent.EventContextMeasured,
		MeasuredInputTokens:  100,
		MeasuredOutputTokens: 4,
	})
	a.recordMeasuredUsage(agent.Event{
		Type:                 agent.EventContextMeasured,
		MeasuredInputTokens:  100,
		MeasuredOutputTokens: 8,
		MeasuredUsageFinal:   true,
	})
	a.recordMeasuredUsage(agent.Event{
		Type:                 agent.EventContextMeasured,
		MeasuredInputTokens:  140,
		MeasuredOutputTokens: 12,
		MeasuredUsageFinal:   true,
	})

	snap := a.StatusSnapshot()

	if snap.Context.UsedTokens != 140 {
		t.Fatalf("expected latest measured input tokens in context, got %#v", snap.Context)
	}
	if snap.Usage.ModelCalls != 2 || snap.Usage.TotalInputTokens != 240 || snap.Usage.TotalOutputTokens != 20 {
		t.Fatalf("expected cumulative measured usage, got %#v", snap.Usage)
	}
	if snap.Usage.LastInputTokens != 140 || snap.Usage.LastOutputTokens != 12 {
		t.Fatalf("expected last measured usage, got %#v", snap.Usage)
	}
}

func TestStatusSnapshotTracksSubagentActiveByTaskID(t *testing.T) {
	a := &App{
		Config:    &config.Config{Subagent: config.SubagentConfig{Enabled: true}},
		SessionID: "session-1",
	}

	a.recordSubagentEvent(agent.Event{
		Type:       agent.EventSubagentStarted,
		ToolCallID: "task-1",
		Message:    "started task",
		CreatedAt:  time.Now(),
	})
	if snap := a.StatusSnapshot(); snap.Subagents.Active != 1 {
		t.Fatalf("expected one active subagent after start, got %#v", snap.Subagents)
	}

	a.flushSubagentEvents(nil, nil, nil)
	if snap := a.StatusSnapshot(); snap.Subagents.Active != 1 {
		t.Fatalf("expected flushed started subagent to remain active, got %#v", snap.Subagents)
	}
	a.recordSubagentEvent(agent.Event{
		Type:       agent.EventSubagentFinished,
		ToolCallID: "task-1",
		Message:    "finished task",
		CreatedAt:  time.Now(),
	})

	snap := a.StatusSnapshot()
	if snap.Subagents.Active != 0 {
		t.Fatalf("expected no active subagents after finish, got %#v", snap.Subagents)
	}
	if snap.Subagents.Latest != "finished task" {
		t.Fatalf("expected latest finished status, got %#v", snap.Subagents)
	}
}

func TestStatusSnapshotIncludesMemoryTopicCount(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(memory.MemoryFile{
		Type: memory.TypeProject, Name: "Go", Description: "Project uses Go", Body: "Project uses Go.",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(memory.MemoryFile{
		Type: memory.TypeFeedback, Name: "Tests", Description: "Run tests", Body: "Run tests after edits.",
	}); err != nil {
		t.Fatal(err)
	}
	a := &App{
		Config:         &config.Config{Memory: config.MemoryConfig{Enabled: true, MaxChars: 4000}},
		MemoryStore:    store,
		MemoryMaxChars: 4000,
	}

	snap := a.StatusSnapshot()
	if snap.Memory.Topics != 2 {
		t.Fatalf("expected 2 topics, got %#v", snap.Memory)
	}
	if snap.Memory.Chars == 0 {
		t.Fatalf("expected non-empty index chars, got %#v", snap.Memory)
	}
}
