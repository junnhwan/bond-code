package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

func TestPrepareModelMessagesAppliesOptionalContextGovernance(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system rules"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "1", ToolName: "read_file", Content: strings.Repeat("old file ", 200)},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "2", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "2", ToolName: "read_file", Content: strings.Repeat("mid file ", 200)},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "3", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "3", ToolName: "read_file", Content: "recent"},
		{Role: llm.RoleUser, Content: "current question"},
	}
	tests := []struct {
		name             string
		configure        func(*Loop)
		wantSameLength   bool
		wantContextEvent bool
		wantMicroClear   bool
	}{
		{
			name:           "disabled preserves full messages",
			configure:      func(*Loop) {},
			wantSameLength: true,
		},
		{
			name: "enabled micro-clears old tool results",
			configure: func(loop *Loop) {
				loop.SetContextManager(contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{
					AutoCompact:            true,
					MicroCompactKeepRecent: 1,
					MicroCompactMinChars:   10,
				})), 100000)
			},
			wantContextEvent: true,
			wantMicroClear:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := NewLoop(LoopConfig{}, llm.NewFakeClient(nil), tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))
			tt.configure(loop)
			var events []Event

			got := loop.prepareModelMessages(context.Background(), messages, func(event Event) {
				events = append(events, event)
			}, 2)

			if tt.wantSameLength && len(got) != len(messages) {
				t.Fatalf("message count = %d, want %d", len(got), len(messages))
			}
			if loopTestHasEvent(events, EventContextUpdated) != tt.wantContextEvent {
				t.Fatalf("context event present = %v, want %v", loopTestHasEvent(events, EventContextUpdated), tt.wantContextEvent)
			}
			if tt.wantMicroClear {
				cleared := 0
				for _, msg := range got {
					if msg.Role == llm.RoleTool && msg.Content == "[Old tool result content cleared]" {
						cleared++
					}
				}
				if cleared < 1 {
					t.Fatalf("expected micro-cleared tool results, got %#v", got)
				}
			}
		})
	}
}

func TestPrepareModelMessagesInjectsStoredSummary(t *testing.T) {
	loop := NewLoop(LoopConfig{}, llm.NewFakeClient(nil), tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))
	loop.SetContextManager(contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{AutoCompact: true})), 100000)
	store := contextx.NewSummaryStore(t.TempDir(), "session-1")
	_ = store.Save(contextx.SummaryArtifact{Version: 2, Summary: "## Goal\nFinish the rewrite"})
	loop.SetContextSummaryStore(store)

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system rules"},
		{Role: llm.RoleUser, Content: "continue"},
	}

	got := loop.prepareModelMessages(context.Background(), messages, func(Event) {}, 17)
	if len(got) < 2 {
		t.Fatalf("expected system + user, got %#v", got)
	}
	if got[0].Role != llm.RoleSystem {
		t.Fatalf("expected system prompt, got %#v", got[0])
	}
	if !strings.Contains(got[1].Content, "## Context Summary") || !strings.Contains(got[1].Content, "Finish the rewrite") {
		t.Fatalf("expected injected context summary, got %#v", got[1])
	}
}
