package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
	"github.com/junnhwan/bond-code/internal/command"
)

func TestViewSnapshotStatesFitCommonTerminalSizes(t *testing.T) {
	sizes := []struct {
		width  int
		height int
	}{
		{80, 24},
		{120, 32},
		{160, 40},
	}

	scenarios := []struct {
		name    string
		model   func() Model
		present []string
		absent  []string
	}{
		{
			name:    "empty",
			model:   func() Model { return NewModel(Config{}) },
			present: []string{"Bond Code", "terminal coding agent", "/help", "normal"},
			absent:  []string{"Type a message", "Local coding agent runtime", "running · Esc/Ctrl+C stop"},
		},
		{
			name: "assistant streaming",
			model: func() Model {
				model := NewModel(Config{})
				model.agent.Busy = true
				model.timeline = model.timeline.StartUserTurn("summarize")
				model.timeline = model.timeline.AppendAssistantChunk("reading README")
				return model
			},
			present: []string{"summarize", "reading README"},
			absent:  nil,
		},
		{
			name: "tool running",
			model: func() Model {
				model := NewModel(Config{})
				model.timeline = model.timeline.StartUserTurn("run tests")
				model = model.ApplyAgentEvent(agent.Event{
					Type:     agent.EventToolRequested,
					ToolName: "run_command",
					Input:    `{"command":"go test ./..."}`,
				})
				return model
			},
			present: []string{"Run", "go test ./...", "running"},
			absent:  []string{`"command"`},
		},
		{
			name: "tool failed",
			model: func() Model {
				model := NewModel(Config{})
				model.timeline = model.timeline.StartUserTurn("read missing")
				model = model.ApplyAgentEvent(agent.Event{
					Type:     agent.EventToolResult,
					ToolName: "read_file",
					Input:    `{"path":"missing.md"}`,
					Error:    "file not found",
				})
				return model
			},
			present: []string{"Read", "missing.md", "failed"},
			absent:  []string{`{"path"`},
		},
		{
			name: "permission pending",
			model: func() Model {
				model := NewModel(Config{})
				model = model.ApplyAgentEvent(agent.Event{
					Type:     agent.EventToolConfirmationRequested,
					ToolName: "write_file",
					Risk:     "medium",
					Input:    `{"path":"README.md"}`,
				})
				return model
			},
			present: []string{"Permission required", "Risk: medium", "y allow once"},
			absent:  []string{"/ commands", "@ files", "interrupted"},
		},

		{
			name: "question takeover",
			model: func() Model {
				model := NewModel(Config{})
				model.question = &ask.Question{
					Prompt:  "Choose a path",
					Options: []ask.Option{{Label: "Continue", Description: "Use the focused path"}},
				}
				return model
			},
			present: []string{"Choose a path", "Continue", "↑↓ select"},
			absent:  []string{"/ commands", "@ files"},
		},
		{
			name: "long output collapsed",
			model: func() Model {
				model := NewModel(Config{})
				model.timeline = model.timeline.StartUserTurn("diff")
				model = model.ApplyAgentEvent(agent.Event{
					Type:     agent.EventToolResult,
					ToolName: "run_command",
					Input:    `{"command":"git diff"}`,
					Output:   strings.Repeat("+ changed line\n", 80),
				})
				return model
			},
			// Grok collapsed header: ◆ Run … · N lines (no Claude "collapsed" label).
			present: []string{"◆", "Run", "git diff", "lines"},
			absent:  []string{strings.Repeat("+ changed line\n", 4)},
		},
		{
			name: "command suggestions",
			model: func() Model {
				registry := command.NewRegistry()
				_ = registry.Register(command.Command{
					Name:        "status",
					Description: "Show current runtime status",
					Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
						return command.Result{}, nil
					},
				})
				model := NewModel(Config{Commands: registry})
				model = model.SetInput("/")
				return model.updateSuggestions()
			},
			present: []string{"/status", "Show current runtime status", "normal"},
			absent:  []string{" - Show current runtime status"},
		},
	}

	for _, size := range sizes {
		for _, scenario := range scenarios {
			t.Run(scenario.name, func(t *testing.T) {
				model := scenario.model().SetSize(size.width, size.height)
				view := model.View()
				assertViewFits(t, view, size.width, size.height)
				// Strip ANSI so shimmer-styled brand glyphs still match "bond".
				plain := ansi.Strip(view)
				for _, want := range scenario.present {
					if !strings.Contains(plain, want) && !strings.Contains(view, want) {
						t.Fatalf("%dx%d %s missing %q:\n%s", size.width, size.height, scenario.name, want, plain)
					}
				}
				for _, notWant := range append(scenario.absent,
					"SESSION", "PROJECT", "TOOLS", "│ sess", "│ perm", "│ todo", "│ agent",
				) {
					if notWant != "" && (strings.Contains(plain, notWant) || strings.Contains(view, notWant)) {
						t.Fatalf("%dx%d %s should not contain %q:\n%s", size.width, size.height, scenario.name, notWant, plain)
					}
				}
			})
		}
	}
}

func TestEmptyTranscriptRendersBrandWelcome(t *testing.T) {
	model := NewModel(Config{}).SetSize(120, 32)
	view := ansi.Strip(model.View())

	if !containsBraille(view) {
		t.Fatalf("expected empty transcript to render braille bond mark:\n%s", view)
	}
	for _, want := range []string{"Bond Code", "terminal coding agent", "/status", "@path"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected empty transcript to render brand welcome text %q:\n%s", want, view)
		}
	}
	// Distinct from main's help block.
	if strings.Contains(view, "Ask about this repo, edit files, run tests") {
		t.Fatalf("empty transcript still uses main welcome help block:\n%s", view)
	}
}

func TestEmptyComposerRendersSinglePromptLine(t *testing.T) {
	model := NewModel(Config{}).SetSize(100, 24)
	view := model.View()

	// The composer prompt uses the Grok-like ❯ glyph.
	if got := strings.Count(view, "❯"); got < 1 {
		t.Fatalf("expected empty composer to render ❯ prompt, got %d:\n%s", got, view)
	}
}

func assertViewFits(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Fatalf("view has %d lines, exceeds height %d:\n%s", len(lines), height, view)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("view line width %d exceeds terminal width %d:\n%s", got, width, view)
		}
	}
}

func TestClaudeCoreSnapshotSimplifiesBaseComposerAndRuntimeFooter(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{name: "narrow", width: 40, height: 16},
		{name: "normal", width: 80, height: 24},
		{name: "wide", width: 160, height: 32},
	} {
		t.Run(size.name, func(t *testing.T) {
			model := NewModel(Config{Status: Status{Model: "claude-sonnet-4"}}).SetSize(size.width, size.height)
			model.agent.ContextTokens = 8100
			model.agent.ContextMaxTokens = 100000
			view := ansi.Strip(model.View())
			assertViewFits(t, view, size.width, size.height)

			composer := ansi.Strip(model.composerViewForWidth(model.currentLayout().TimelineW))
			lines := strings.Split(view, "\n")
			footer := lines[len(lines)-1]
			baseDock := composer + "\n" + footer
			for _, notWant := range []string{"/help for commands", "/ commands", "? help", "@ files", "ctrl+e expand", "PgUp/PgDn", "chars ", "tok ~"} {
				if strings.Contains(baseDock, notWant) {
					t.Fatalf("%s simplified composer/footer contains permanent help/decorative field %q:\n%s", size.name, notWant, baseDock)
				}
			}
			if strings.TrimSpace(footer) == "" {
				t.Fatalf("%s final row should be non-empty shortcuts/footer, got %q:\n%s", size.name, footer, view)
			}
			if got := lipgloss.Width(footer); got > size.width {
				t.Fatalf("%s footer width = %d, want <= %d: %q", size.name, got, size.width, footer)
			}
		})
	}
}
