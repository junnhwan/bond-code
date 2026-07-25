package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
)

func TestBottomDockMeasurementAndWidth(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(72, 20)
	model.agent.QueuedPrompts = []string{strings.Repeat("queued follow-up ", 8)}
	model.question = &ask.Question{
		Prompt:  strings.Repeat("Choose a path ", 6),
		Options: []ask.Option{{Label: "Continue", Description: strings.Repeat("carefully ", 8)}},
	}

	questionDock := model.measureBottomDock()
	if questionDock.question == "" || questionDock.permission != "" {
		t.Fatalf("question dock precedence is wrong: %#v", questionDock)
	}
	if got, want := questionDock.reservedHeight(), questionDock.componentHeight(); got != want {
		t.Fatalf("reserved dock height = %d, component sum = %d", got, want)
	}

	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md","content":"hello"}`,
	}
	permissionDock := model.measureBottomDock()
	if permissionDock.permission == "" || permissionDock.question != "" {
		t.Fatalf("permission must replace a hidden question: %#v", permissionDock)
	}

	layout := CalculateLayout(44, 20, permissionDock.reservedHeight())
	rendered := model.renderBottomDock(permissionDock, layout)
	for _, part := range rendered.parts("") {
		for _, line := range strings.Split(part, "\n") {
			if got := ansi.StringWidth(strings.TrimRight(line, " ")); got > layout.TimelineW {
				t.Fatalf("bottom dock line width = %d, want <= %d:\n%s", got, layout.TimelineW, part)
			}
		}
	}
}

func TestBottomDockRuntimeFooterOrderAndBounds(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
		wantAll       bool
	}{
		{name: "narrow", width: 36, height: 16},
		{name: "normal", width: 80, height: 24, wantAll: true},
		{name: "wide", width: 160, height: 32, wantAll: true},
	} {
		t.Run(size.name, func(t *testing.T) {
			model := NewModel(Config{Status: Status{Model: "claude-sonnet-4"}}).SetSize(size.width, size.height)
			model.timeline = model.timeline.StartUserTurn("TRANSCRIPT_SENTINEL")
			model = model.SetInput("COMPOSER_SENTINEL")
			model.agent.ContextTokens = 8100
			model.agent.ContextMaxTokens = 100000

			view := ansi.Strip(model.View())
			lines := strings.Split(view, "\n")
			footer := lines[len(lines)-1]
			// Chrome shell: footer is a context-sensitive shortcuts bar (or mode fallback).
			if strings.TrimSpace(footer) == "" {
				t.Fatalf("%s expected non-empty footer, got empty:\n%s", size.name, view)
			}
			if strings.Contains(footer, "/ commands") || strings.Contains(footer, "? help") || strings.Contains(footer, "@ files") {
				t.Fatalf("%s footer contains a permanent shortcut legend: %q", size.name, footer)
			}
			if size.wantAll {
				// Model/mode live on the prompt info line under the composer.
				if !strings.Contains(view, "claude-sonnet-4") {
					t.Fatalf("%s view missing model on prompt info line:\n%s", size.name, view)
				}
				if !strings.Contains(view, "normal") {
					t.Fatalf("%s view missing mode:\n%s", size.name, view)
				}
			}
			if got := ansi.StringWidth(footer); got > size.width {
				t.Fatalf("%s runtime footer width = %d, want <= %d: %q", size.name, got, size.width, footer)
			}
			if strings.Index(view, "TRANSCRIPT_SENTINEL") >= strings.Index(view, "COMPOSER_SENTINEL") ||
				strings.Index(view, "COMPOSER_SENTINEL") >= strings.LastIndex(view, footer) {
				t.Fatalf("%s expected transcript → composer → footer:\n%s", size.name, view)
			}
		})
	}
}

func TestBottomDockTakeoversOwnInputWithoutDuplicatedFooterLegends(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*Model)
		panelText string
		panelHint string
	}{
		{
			name: "permission",
			setup: func(model *Model) {
				model.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file", Risk: "medium"}
			},
			panelText: "Permission required",
			panelHint: "y allow once",
		},
		{
			name: "question",
			setup: func(model *Model) {
				model.question = &ask.Question{Prompt: "Choose a path", Options: []ask.Option{{Label: "Continue"}}}
			},
			panelText: "Choose a path",
			panelHint: "Enter confirm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(Config{Status: Status{Model: "claude-sonnet-4"}}).SetSize(100, 24)
			model = model.SetInput("HIDDEN_COMPOSER_SENTINEL")
			model.agent.ContextTokens = 8100
			model.agent.ContextMaxTokens = 100000
			tt.setup(&model)

			view := ansi.Strip(model.View())
			lines := strings.Split(view, "\n")
			footer := lines[len(lines)-1]
			for _, want := range []string{tt.panelText, tt.panelHint} {
				if !strings.Contains(view, want) {
					t.Fatalf("%s takeover missing %q:\n%s", tt.name, want, view)
				}
			}
			if strings.Contains(view, "HIDDEN_COMPOSER_SENTINEL") {
				t.Fatalf("%s takeover should hide composer:\n%s", tt.name, view)
			}
			if strings.TrimSpace(footer) == "" {
				t.Fatalf("%s footer should not be empty", tt.name)
			}
			for _, notWant := range []string{"/ commands", "? help", "@ files"} {
				if strings.Contains(footer, notWant) {
					t.Fatalf("%s panel legend leaked into footer as %q: %q", tt.name, notWant, footer)
				}
			}
			assertViewFits(t, view, model.width, model.height)
		})
	}
}
