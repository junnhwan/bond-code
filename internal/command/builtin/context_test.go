package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/command"
)

type contextSummaryStatusProvider struct{}

func (contextSummaryStatusProvider) StatusSnapshot() app.RuntimeStatus {
	return app.RuntimeStatus{
		Context: app.ContextStatus{
			MaxTokens:  100000,
			UsedTokens: 42000,
			Summary:    "2026-06-28 10:00:00Z",
			SummaryText: strings.Join([]string{
				"User asked to improve TUI.",
				"Implemented transcript search.",
				"Open work: model switching.",
			}, "\n"),
		},
	}
}

func TestContextSummaryCommandShowsFullSavedSummary(t *testing.T) {
	result, err := ContextCommand().Run(context.Background(), command.Env{
		StatusProvider: contextSummaryStatusProvider{},
	}, []string{"summary"})
	if err != nil {
		t.Fatalf("context summary command: %v", err)
	}
	for _, want := range []string{"Context summary saved at 2026-06-28 10:00:00Z", "Implemented transcript search.", "Open work: model switching."} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("expected %q in full context summary:\n%s", want, result.Output)
		}
	}
	if result.Panel != nil {
		t.Fatalf("full context summary should render as plain text, got panel %#v", result.Panel)
	}
}

func TestContextPanelDoesNotInlineFullSummary(t *testing.T) {
	result, err := ContextCommand().Run(context.Background(), command.Env{
		StatusProvider: contextSummaryStatusProvider{},
	}, nil)
	if err != nil {
		t.Fatalf("context command: %v", err)
	}
	if strings.Contains(result.Output, "Implemented transcript search.") {
		t.Fatalf("regular /context should not inline full summary:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "summary: 2026-06-28 10:00:00Z") {
		t.Fatalf("regular /context should show summary timestamp:\n%s", result.Output)
	}
}

type contextBreakdownStatusProvider struct{}

func (contextBreakdownStatusProvider) StatusSnapshot() app.RuntimeStatus {
	return app.RuntimeStatus{
		Context: app.ContextStatus{
			MaxTokens:  100000,
			UsedTokens: 42000,
			Breakdown: app.ContextBreakdown{
				SystemTokens:       1200,
				ConversationTokens: 26000,
				ToolResultTokens:   9000,
				SummaryTokens:      1800,
			},
		},
	}
}

func TestContextPanelShowsTokenBreakdown(t *testing.T) {
	result, err := ContextCommand().Run(context.Background(), command.Env{
		StatusProvider: contextBreakdownStatusProvider{},
	}, nil)
	if err != nil {
		t.Fatalf("context command: %v", err)
	}
	for _, want := range []string{
		"system: 1200 tokens",
		"history: 26000 tokens",
		"tool results: 9000 tokens",
		"context summary: 1800 tokens",
	} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("expected %q in context breakdown:\n%s", want, result.Output)
		}
	}
}
