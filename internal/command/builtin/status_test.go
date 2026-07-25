package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/command"
)

type fakeStatusProvider struct{}

func (fakeStatusProvider) StatusSnapshot() app.RuntimeStatus {
	return app.RuntimeStatus{
		SessionID:  "s1",
		Model:      "fake",
		ToolCount:  3,
		Permission: "confirm",
		Context: app.ContextStatus{
			MaxTokens: 100000,
			Stats:     "context tokens: 10 -> 10",
			Breakdown: app.ContextBreakdown{
				SystemTokens:       1000,
				ConversationTokens: 2000,
				ToolResultTokens:   3000,
				SummaryTokens:      400,
			},
		},
		Memory:   app.MemoryStatus{Enabled: true, Topics: 2, Chars: 120, MaxChars: 4000},
		Planning: app.PlanningStatus{Enabled: true, Summary: "ready: todo_002"},
		Skills:   app.SkillsStatus{Enabled: true, Count: 2, Root: "skills"},
		MCP:      app.MCPStatus{Enabled: true, Servers: 1, Tools: 2},
	}
}

func TestStatusCommandIncludesContextBreakdown(t *testing.T) {
	result, err := StatusCommand().Run(context.Background(), command.Env{
		StatusProvider: fakeStatusProvider{},
	}, nil)
	if err != nil {
		t.Fatalf("status command: %v", err)
	}
	for _, want := range []string{
		"context system: 1000 tokens",
		"context history: 2000 tokens",
		"context tool results: 3000 tokens",
		"context summary: 400 tokens",
	} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("expected %q in output:\n%s", want, result.Output)
		}
	}
	if result.Panel == nil {
		t.Fatal("expected structured status panel")
	}
	if !panelContainsRow(result.Panel, "tool results", "3000 tokens") {
		t.Fatalf("expected status panel to include tool result token breakdown: %#v", result.Panel)
	}
}

func panelContainsRow(panel *command.Panel, key, value string) bool {
	for _, section := range panel.Sections {
		for _, row := range section.Rows {
			if row.Key == key && row.Value == value {
				return true
			}
		}
	}
	return false
}

func TestStatusCommandRendersRuntimeSnapshot(t *testing.T) {
	result, err := StatusCommand().Run(context.Background(), command.Env{
		StatusProvider: fakeStatusProvider{},
	}, nil)
	if err != nil {
		t.Fatalf("status command: %v", err)
	}
	output := result.Output
	for _, want := range []string{"session: s1", "model: fake", "context:", "memory: enabled=true topics=2 index_chars=120/4000", "planning:", "skills: enabled=true count=2 root=skills", "mcp:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output:\n%s", want, output)
		}
	}
}
