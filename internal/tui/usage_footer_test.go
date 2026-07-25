package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
)

func TestUsageFooterShowsOnlyMeaningfulRuntimeValues(t *testing.T) {
	model := NewModel(Config{Status: Status{Model: "claude-sonnet-4"}}).SetSize(100, 24)
	model.agent.ContextTokens = 12345
	model.agent.ContextMaxTokens = 200000
	model.usage = UsageView{ModelCalls: 2, TotalInputTokens: 50000, TotalOutputTokens: 9000}

	// Chrome shell: shortcuts bar is the footer; model/mode/context sit on prompt info.
	view := ansi.Strip(model.View())
	footer := ansi.Strip(model.renderFooter(model.currentLayout()))
	if strings.TrimSpace(footer) == "" {
		t.Fatal("expected non-empty shortcuts/footer row")
	}
	if !strings.Contains(view, "claude-sonnet-4") {
		t.Fatalf("expected model on prompt info line:\n%s", view)
	}
	for _, notWant := range []string{"? help", "/ commands", "@ files"} {
		if strings.Contains(footer, notWant) {
			t.Fatalf("footer should omit permanent help legend %q: %q", notWant, footer)
		}
	}
	if got := renderedHeight(footer); got != 1 {
		t.Fatalf("footer height = %d, want exactly one row: %q", got, footer)
	}
}

func TestUsageFooterOmitsUnavailableModelAndContext(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 24)
	footer := ansi.Strip(model.renderFooter(model.currentLayout()))
	// Idle shortcuts bar (not bare "normal" only).
	if !strings.Contains(footer, "tab") && footer != "normal" {
		t.Fatalf("idle footer unexpected: %q", footer)
	}
}

func TestUsageFooterTransientSearchReplacesRuntimeRow(t *testing.T) {
	model := NewModel(Config{Status: Status{Model: "claude-sonnet-4"}}).SetSize(80, 24)
	model.agent.ContextTokens = 12345
	model.agent.ContextMaxTokens = 200000
	model.search.Active = true
	model.search.Query = "needle"

	footer := ansi.Strip(model.renderFooter(model.currentLayout()))
	for _, want := range []string{"search", "needle"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("transient search footer missing %q: %q", want, footer)
		}
	}
	if got := renderedHeight(footer); got != 1 {
		t.Fatalf("transient footer height = %d, want exactly one row: %q", got, footer)
	}
}

func TestUsageFooterReachableStatesOmitRemovedRoutes(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Model)
	}{
		{name: "runtime", setup: func(*Model) {}},
		{name: "search", setup: func(model *Model) {
			model.search = SearchState{Active: true, Query: "needle", MatchIndex: -1}
		}},
		{name: "stale leader state", setup: func(model *Model) {
			model.leaderPending = true
		}},
		{name: "running", setup: func(model *Model) {
			model.agent.Busy = true
		}},
		{name: "queued", setup: func(model *Model) {
			model.agent.Busy = true
			model.agent.QueuedPrompts = []string{"follow up"}
		}},
		{name: "stale message navigation", setup: func(model *Model) {
			model.timeline = model.timeline.StartUserTurn("prompt")
			model.navTurnIdx = 0
		}},
		{name: "scroll", setup: func(model *Model) {
			model.scroll = 5
		}},
		{name: "permission", setup: func(model *Model) {
			model.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file"}
		}},
		{name: "question", setup: func(model *Model) {
			model.question = &ask.Question{Prompt: "Choose", Options: []ask.Option{{Label: "Continue"}}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(Config{}).SetSize(100, 24)
			tt.setup(&model)
			footer := ansi.Strip(model.renderFooter(model.currentLayout()))
			for _, removed := range []string{"<leader>", "leader  ", "n new", "c compact", "Alt+Ctrl", "alt+ctrl", "P/N move", "p/n move"} {
				if strings.Contains(footer, removed) {
					t.Fatalf("footer state %q leaked removed route %q: %q", tt.name, removed, footer)
				}
			}
		})
	}
}
