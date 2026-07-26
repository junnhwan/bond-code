package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/command"
)

func TestCanonicalDirectKeyDescriptorsReachTUIRoutes(t *testing.T) {
	t.Setenv("EDITOR", "bondcode-test-editor")
	t.Setenv("TMP", t.TempDir())

	seenTargets := make(map[command.ExecutionTargetID]struct{})
	for _, descriptor := range command.DirectKeyDescriptors() {
		descriptor := descriptor
		t.Run(descriptor.ID, func(t *testing.T) {
			if _, duplicate := seenTargets[descriptor.ExecutionTarget]; duplicate {
				t.Fatalf("duplicate route coverage for target %q", descriptor.ExecutionTarget)
			}
			seenTargets[descriptor.ExecutionTarget] = struct{}{}
			assertDirectKeyRoute(t, descriptor)
		})
	}
	if got, want := len(seenTargets), len(command.DirectKeyDescriptors()); got != want {
		t.Fatalf("covered execution targets = %d, want %d", got, want)
	}
}

func assertDirectKeyRoute(t *testing.T, descriptor command.DirectKeyDescriptor) {
	t.Helper()
	variants := strings.Split(descriptor.DisplayShortcut, " / ")

	switch descriptor.ExecutionTarget {
	case "tui-local.submit":
		for _, variant := range variants {
			model := NewModel(Config{}).SetInput("hello")
			next, _ := updateDirectKey(t, model, variant)
			if next.inputValue() != "" || len(next.timeline.Turns) != 1 {
				t.Fatalf("%s did not submit the prompt", variant)
			}
		}
	case "tui-local.composer.newline":
		for _, variant := range variants {
			// Bubble Tea cannot deliver Shift+Enter; keep the routing name wired.
			if strings.EqualFold(variant, "Shift+Enter") {
				if !isComposerNewlineKey(strings.ToLower(variant)) {
					t.Fatalf("%s is not connected to composer newline routing", variant)
				}
				continue
			}
			model := NewModel(Config{}).SetInput("first")
			next, _ := updateDirectKey(t, model, variant)
			if got := next.inputValue(); got != "first\n" {
				t.Fatalf("%s inserted %q, want newline", variant, got)
			}
		}
	case "tui-local.cancel":
		for _, variant := range variants {
			cancelled := false
			model := NewModel(Config{})
			model.agent.Busy = true
			model.agent.Cancel = func() { cancelled = true }
			next, cmd := updateDirectKey(t, model, variant)
			if cmd != nil || !cancelled || next.agent.Busy {
				t.Fatalf("%s did not cancel the active run", variant)
			}
		}
	case "tui-local.interrupt":
		for _, variant := range variants {
			cancelled := false
			model := NewModel(Config{})
			model.agent.Busy = true
			model.agent.Cancel = func() { cancelled = true }
			next, cmd := updateDirectKey(t, model, variant)
			if cmd != nil || !cancelled || next.agent.Busy {
				t.Fatalf("%s did not interrupt the active run", variant)
			}
		}
	case "tui-local.exit.empty":
		for _, variant := range variants {
			_, cmd := updateDirectKey(t, NewModel(Config{}), variant)
			if cmd == nil {
				t.Fatalf("%s did not request exit on empty input", variant)
			}
		}
	case "tui-local.mode.cycle":
		for _, variant := range variants {
			next, _ := updateDirectKey(t, NewModel(Config{}), variant)
			if next.mode != ModePlan {
				t.Fatalf("%s did not cycle into plan mode", variant)
			}
		}
	case "tui-local.view.verbose":
		for _, variant := range variants {
			model := NewModel(Config{})
			beforeDetails := model.showToolDetails
			next, _ := updateDirectKey(t, model, variant)
			if !next.verbose {
				t.Fatalf("%s did not toggle expanded details", variant)
			}
			// Expanded mode must keep tool rows visible (not thrash density off).
			if !next.showToolDetails {
				t.Fatalf("%s turned off tool details while expanding", variant)
			}
			if !beforeDetails && next.showToolDetails != true {
				t.Fatalf("%s should force showToolDetails on when expanding", variant)
			}
			// Tool density must not flip historical thinking open.
			if next.showThinking {
				t.Fatalf("%s must not enable showThinking", variant)
			}
		}
	case "tui-local.view.thinking":
		for _, variant := range variants {
			next, _ := updateDirectKey(t, NewModel(Config{}), variant)
			if !next.showThinking {
				t.Fatalf("%s did not enable historical thinking", variant)
			}
			if next.verbose {
				t.Fatalf("%s must not flip verbose tool density", variant)
			}
		}
	case "tui-local.history.reverse":
		for _, variant := range variants {
			model := NewModel(Config{}).SetInput("draft")
			model.composer.History = []string{"older prompt"}
			next, _ := updateDirectKey(t, model, variant)
			if !next.search.Active || !next.reverseHistory.active {
				t.Fatalf("%s did not open reverse history search", variant)
			}
		}
	case "tui-local.agent.switcher":
		for _, variant := range variants {
			model := NewModel(Config{})
			model.subagentTraces["trace-only"] = &AgentTrace{TaskID: "trace-only", AgentType: "reviewer", Status: "running"}
			next, _ := updateDirectKey(t, model, variant)
			if next.focus != FocusAgentBar || next.agentBarSelected != "trace-only" {
				t.Fatalf("%s did not open the Agent switcher", variant)
			}
		}
	case "tui-local.prompt.editor":
		for _, variant := range variants {
			_, cmd := updateDirectKey(t, NewModel(Config{}).SetInput("draft"), variant)
			if cmd == nil {
				t.Fatalf("%s did not open the external editor", variant)
			}
		}
	case "tui-local.prompt.stash":
		for _, variant := range variants {
			next, _ := updateDirectKey(t, NewModel(Config{}).SetInput("park me"), variant)
			if next.inputValue() != "" || len(next.stash) != 1 || next.stash[0] != "park me" {
				t.Fatalf("%s did not stash the draft", variant)
			}
		}
	case "tui-local.screen.redraw":
		for _, variant := range variants {
			model := NewModel(Config{}).SetInput("draft")
			model.timeline = model.timeline.StartUserTurn("keep")
			version := model.timeline.Version
			next, cmd := updateDirectKey(t, model, variant)
			if cmd == nil || next.timeline.Version != version || next.inputValue() != "draft" {
				t.Fatalf("%s did not request a non-destructive redraw", variant)
			}
		}
	default:
		t.Fatalf("canonical descriptor %q has no TUI route assertion for target %q", descriptor.ID, descriptor.ExecutionTarget)
	}
}

func updateDirectKey(t *testing.T, model Model, displayShortcut string) (Model, tea.Cmd) {
	t.Helper()
	msg, ok := directKeyMessage(displayShortcut)
	if !ok {
		t.Fatalf("canonical display shortcut %q has no Bubble Tea route fixture", displayShortcut)
	}
	updated, cmd := model.Update(msg)
	return updated.(Model), cmd
}

func directKeyMessage(displayShortcut string) (tea.KeyMsg, bool) {
	switch strings.ToLower(displayShortcut) {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case "alt+enter":
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, true
	case "ctrl+j":
		return tea.KeyMsg{Type: tea.KeyCtrlJ}, true
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}, true
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}, true
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}, true
	case "alt+m":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true}, true
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}, true
	case "ctrl+t":
		return tea.KeyMsg{Type: tea.KeyCtrlT}, true
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}, true
	case "ctrl+up":
		return tea.KeyMsg{Type: tea.KeyCtrlUp}, true
	case "ctrl+g":
		return tea.KeyMsg{Type: tea.KeyCtrlG}, true
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}, true
	case "ctrl+l":
		return tea.KeyMsg{Type: tea.KeyCtrlL}, true
	default:
		return tea.KeyMsg{}, false
	}
}
