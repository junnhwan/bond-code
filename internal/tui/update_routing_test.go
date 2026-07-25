package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
)

func TestKeyRoutePriority(t *testing.T) {
	all := NewModel(Config{})
	all.agent.Pending = &agent.Event{}
	all.question = &ask.Question{}
	all.history.visible = true
	all.overlay.kind = overlayAlert
	all.search.Active = true

	tests := []struct {
		name  string
		model Model
		want  keyRoute
	}{
		{name: "confirmation", model: all, want: keyRouteConfirmation},
		{name: "question", model: func() Model { m := all; m.agent.Pending = nil; return m }(), want: keyRouteQuestion},
		{name: "history", model: func() Model { m := all; m.agent.Pending = nil; m.question = nil; return m }(), want: keyRouteHistory},
		{name: "overlay", model: func() Model { m := all; m.agent.Pending = nil; m.question = nil; m.history.visible = false; return m }(), want: keyRouteOverlay},
		{name: "search", model: func() Model {
			m := all
			m.agent.Pending = nil
			m.question = nil
			m.history.visible = false
			m.overlay = overlayState{}
			return m
		}(), want: keyRouteSearch},
		{name: "composer", model: NewModel(Config{}), want: keyRouteComposer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.model.activeKeyRoute(); got != tt.want {
				t.Fatalf("active key route = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModalRoutingUsesOnlyHighestPriorityOwner(t *testing.T) {
	menuItems := func() []menuItem {
		items := make([]menuItem, 8)
		for i := range items {
			items[i] = menuItem{label: fmt.Sprintf("item %d", i)}
		}
		return items
	}
	longTranscript := func() Model {
		model := NewModel(Config{}).SetSize(80, 12)
		for i := 0; i < 30; i++ {
			model = appendTestAssistant(model, fmt.Sprintf("routing line %d", i))
		}
		return model
	}

	t.Run("confirmation passes Ctrl+C to global interrupt without closing hidden overlay", func(t *testing.T) {
		cancelled := false
		model := NewModel(Config{}).openMenu("hidden", "", menuItems())
		model.overlay.menu.selected = 6
		model.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file"}
		model.agent.Busy = true
		model.agent.Cancel = func() { cancelled = true }

		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		next := updated.(Model)

		if cmd != nil || !cancelled || next.agent.Busy || next.agent.Pending != nil {
			t.Fatalf("Ctrl+C should globally interrupt confirmation owner, cancelled=%v busy=%v pending=%v cmd=%v",
				cancelled, next.agent.Busy, next.agent.Pending != nil, cmd != nil)
		}
		if !next.overlay.active() || next.overlay.menu.selected != 6 {
			t.Fatalf("hidden overlay must not handle Ctrl+C, active=%v selected=%d", next.overlay.active(), next.overlay.menu.selected)
		}
	})

	t.Run("confirmation passes PageUp to transcript without paging hidden overlay", func(t *testing.T) {
		model := longTranscript().openMenu("hidden", "", menuItems())
		model.overlay.menu.selected = 6
		model.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file"}

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
		next := updated.(Model)

		if next.scroll == 0 {
			t.Fatal("PageUp should scroll the transcript behind the visible confirmation")
		}
		if next.overlay.menu.selected != 6 {
			t.Fatalf("hidden overlay must not receive PageUp, selected=%d", next.overlay.menu.selected)
		}
		if next.agent.Pending == nil {
			t.Fatal("paging must leave the selected confirmation owner active")
		}

		scrolled := next.scroll
		updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		next = updated.(Model)
		if next.scroll >= scrolled {
			t.Fatalf("PageDown should scroll the transcript toward latest, before=%d after=%d", scrolled, next.scroll)
		}
		if next.overlay.menu.selected != 6 {
			t.Fatalf("hidden overlay must not receive PageDown, selected=%d", next.overlay.menu.selected)
		}
	})

	t.Run("question owns arrows and passes paging beyond hidden agent history and overlay", func(t *testing.T) {
		model := longTranscript().openMenu("hidden", "", menuItems())
		model.overlay.menu.selected = 6
		model.history.visible = true
		model.history.cursor = 4
		model.focus = FocusAgentBar
		model.agentBarSelected = "agent-a"
		model.question = &ask.Question{Prompt: "choose", Options: []ask.Option{{Label: "one"}, {Label: "two"}}}

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		next := updated.(Model)
		if next.questionCursor != 1 {
			t.Fatalf("question should own Down, cursor=%d", next.questionCursor)
		}
		if next.agentBarSelected != "agent-a" || next.history.cursor != 4 || next.overlay.menu.selected != 6 {
			t.Fatalf("hidden owners changed on question Down: agent=%q history=%d overlay=%d",
				next.agentBarSelected, next.history.cursor, next.overlay.menu.selected)
		}

		updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyPgUp})
		next = updated.(Model)
		if next.scroll == 0 {
			t.Fatal("PageUp should scroll the transcript behind the visible question")
		}
		if next.agentBarSelected != "agent-a" || next.history.cursor != 4 || next.overlay.menu.selected != 6 {
			t.Fatalf("hidden owners changed on question PageUp: agent=%q history=%d overlay=%d",
				next.agentBarSelected, next.history.cursor, next.overlay.menu.selected)
		}
	})
}

func TestClaudeCoreKeyBindings(t *testing.T) {
	t.Run("enter submits", func(t *testing.T) {
		model := NewModel(Config{}).SetInput("hello")
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		next := updated.(Model)
		if next.inputValue() != "" || len(next.timeline.Turns) != 1 {
			t.Fatalf("Enter should submit and clear the composer, input=%q turns=%d", next.inputValue(), len(next.timeline.Turns))
		}
	})

	t.Run("shift enter and Windows fallback are newlines", func(t *testing.T) {
		if !isComposerNewlineKey("shift+enter") || !isComposerNewlineKey("alt+enter") {
			t.Fatal("composer newline routing must recognize Shift+Enter and Alt+Enter")
		}
		model := NewModel(Config{}).SetInput("first")
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
		if got := updated.(Model).inputValue(); got != "first\n" {
			t.Fatalf("Alt+Enter should insert a newline, got %q", got)
		}
	})

	t.Run("mode cycle and Windows fallback", func(t *testing.T) {
		for name, key := range map[string]tea.KeyMsg{
			"shift tab":                      {Type: tea.KeyShiftTab},
			"alt m":                          {Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true},
			"alt uppercase m with Caps Lock": {Type: tea.KeyRunes, Runes: []rune{'M'}, Alt: true},
		} {
			t.Run(name, func(t *testing.T) {
				model := NewModel(Config{})
				updated, _ := model.Update(key)
				if got := updated.(Model).mode; got != ModePlan {
					t.Fatalf("mode cycle should enter plan mode, got %q", got)
				}
			})
		}

		model := NewModel(Config{}).SetInput("submit after fallback")
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true})
		updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
		next := updated.(Model)
		if next.inputValue() != "" || len(next.timeline.Turns) != 1 {
			t.Fatalf("Alt+M must not make the next Enter look like pasted input, input=%q turns=%d", next.inputValue(), len(next.timeline.Turns))
		}
	})

	t.Run("ctrl o toggles transcript details", func(t *testing.T) {
		model := NewModel(Config{})
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
		if !updated.(Model).verbose {
			t.Fatal("Ctrl+O should toggle verbose transcript details")
		}
	})

	t.Run("ctrl up opens agent switcher", func(t *testing.T) {
		model := NewModel(Config{})
		model.subagentTraces["trace-only"] = &AgentTrace{TaskID: "trace-only", AgentType: "reviewer", Status: "running"}
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlUp})
		next := updated.(Model)
		if next.focus != FocusAgentBar || next.agentBarSelected != "trace-only" {
			t.Fatalf("Ctrl+Up should focus the agent switcher, focus=%v selected=%q", next.focus, next.agentBarSelected)
		}
	})

	t.Run("ctrl g opens external editor", func(t *testing.T) {
		t.Setenv("EDITOR", "bondcode-test-editor")
		t.Setenv("TMP", t.TempDir())
		model := NewModel(Config{}).SetInput("draft")
		_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
		if cmd == nil {
			t.Fatal("Ctrl+G should return an external-editor command")
		}
	})

	t.Run("ctrl s stashes and pops", func(t *testing.T) {
		model := NewModel(Config{}).SetInput("park me")
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
		next := updated.(Model)
		if next.inputValue() != "" || len(next.stash) != 1 || next.stash[0] != "park me" {
			t.Fatalf("Ctrl+S should stash the draft, input=%q stash=%v", next.inputValue(), next.stash)
		}
		updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
		next = updated.(Model)
		if next.overlay.kind != overlayMenu {
			t.Fatalf("Ctrl+S on an empty composer should open the stash menu, overlay=%v", next.overlay.kind)
		}
	})

	t.Run("esc cancels and dismisses", func(t *testing.T) {
		cancelled := false
		busy := NewModel(Config{})
		busy.agent.Busy = true
		busy.agent.Cancel = func() { cancelled = true }
		updated, cmd := busy.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if cmd != nil || !cancelled || updated.(Model).agent.Busy {
			t.Fatalf("Esc should cancel a running agent, cancelled=%v busy=%v cmd=%v", cancelled, updated.(Model).agent.Busy, cmd != nil)
		}

		draft := NewModel(Config{}).SetInput("draft")
		updated, cmd = draft.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if cmd != nil || updated.(Model).inputValue() != "" {
			t.Fatal("Esc should dismiss a non-empty draft")
		}
	})

	t.Run("ctrl c keeps interrupt and exit semantics", func(t *testing.T) {
		cancelled := false
		busy := NewModel(Config{})
		busy.agent.Busy = true
		busy.agent.Cancel = func() { cancelled = true }
		updated, cmd := busy.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd != nil || !cancelled || updated.(Model).agent.Busy {
			t.Fatalf("Ctrl+C should interrupt a running agent, cancelled=%v busy=%v cmd=%v", cancelled, updated.(Model).agent.Busy, cmd != nil)
		}

		draft := NewModel(Config{}).SetInput("draft")
		updated, cmd = draft.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd != nil || updated.(Model).inputValue() != "" {
			t.Fatal("Ctrl+C should clear a non-empty draft before exiting")
		}

		empty := NewModel(Config{})
		_, cmd = empty.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd == nil {
			t.Fatal("repeated Ctrl+C on empty input should exit")
		}
	})

	t.Run("ctrl d exits only with empty input", func(t *testing.T) {
		empty := NewModel(Config{})
		_, cmd := empty.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
		if cmd == nil {
			t.Fatal("Ctrl+D should exit when input is empty")
		}
		draft := NewModel(Config{}).SetInput("ab")
		updated, cmd := draft.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
		if cmd != nil {
			t.Fatal("Ctrl+D with input must not exit")
		}
		if got := updated.(Model).inputValue(); got == "" {
			t.Fatal("Ctrl+D with input must not clear the whole draft")
		}
	})
}

func TestBaseComposerRoutingRemovesLegacyActions(t *testing.T) {
	t.Run("palette and leader are no longer base routes", func(t *testing.T) {
		model := NewModel(Config{})
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
		next := updated.(Model)
		if next.overlay.active() {
			t.Fatal("Ctrl+P must not open the command palette")
		}
		updated, cmd := next.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
		next = updated.(Model)
		_ = next
		_ = cmd
	})

	t.Run("display search and help aliases become ordinary editing or reserved", func(t *testing.T) {
		model := NewModel(Config{})
		model.timeline = model.timeline.StartUserTurn("keep timeline")
		model.timeline = model.timeline.UpsertToolBlock(&ToolBlock{ID: "tool", Name: "run_command", Status: ToolDone, Output: strings.Repeat("line\n", 40), Collapsed: true})

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
		model = updated.(Model)
		if model.search.Active {
			t.Fatal("Ctrl+F must not open transcript search")
		}
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
		model = updated.(Model)
		if !model.timeline.Turns[0].Blocks[0].Tool.Collapsed {
			t.Fatal("Ctrl+E must not toggle the latest tool block")
		}
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
		model = updated.(Model)
		if len(model.timeline.Turns) != 1 {
			t.Fatal("reserved Ctrl+L must not clear timeline history")
		}
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v?")})
		model = updated.(Model)
		if got := model.inputValue(); got != "v?" {
			t.Fatalf("v and ? should type normally, got %q", got)
		}
		if model.verbose || len(model.timeline.Turns) != 1 {
			t.Fatal("v and ? must not activate display/help actions")
		}
	})

	t.Run("session turn and transcript legacy routes are removed", func(t *testing.T) {
		model := NewModel(Config{}).SetSize(80, 12).SetInput("draft words")
		model.sessionHistory = []string{"old", "current"}
		model.sessionHistIdx = 1
		for i := 0; i < 30; i++ {
			model = appendTestAssistant(model, fmt.Sprintf("legacy route line %d", i))
		}
		model.scroll = 7
		for _, key := range []tea.KeyMsg{
			{Type: tea.KeyLeft, Alt: true},
			{Type: tea.KeyRight, Alt: true},
			{Type: tea.KeyCtrlP, Alt: true},
			{Type: tea.KeyCtrlN, Alt: true},
			{Type: tea.KeyCtrlY, Alt: true},
			{Type: tea.KeyCtrlE, Alt: true},
			{Type: tea.KeyCtrlU, Alt: true},
			{Type: tea.KeyCtrlD, Alt: true},
		} {
			updated, _ := model.Update(key)
			model = updated.(Model)
		}
		if model.sessionHistIdx != 1 || model.scroll != 7 {
			t.Fatalf("legacy navigation changed state: session=%d scroll=%d", model.sessionHistIdx, model.scroll)
		}

		empty := NewModel(Config{})
		updated, _ := empty.Update(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
		if updated.(Model).sessionHistIdx != empty.sessionHistIdx {
			t.Fatal("Alt+Left on empty input must remain a no-op")
		}
	})

	t.Run("ctrl h does not open session history", func(t *testing.T) {
		model := NewModel(Config{SessionHistory: &fakeSessionHistory{}})
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
		if updated.(Model).history.visible {
			t.Fatal("Ctrl+H must not open the session history browser")
		}
	})

	t.Run("history and transcript paging remain", func(t *testing.T) {
		model := NewModel(Config{}).SetSize(80, 12)
		model = model.SetInput("older")
		model, _ = model.Submit(context.Background())
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
		model = updated.(Model)
		if model.inputValue() != "older" {
			t.Fatalf("Up should recall prompt history, got %q", model.inputValue())
		}
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(Model)
		if model.inputValue() != "" {
			t.Fatalf("Down should restore the history draft, got %q", model.inputValue())
		}
		model.timeline = TimelineState{}
		for i := 0; i < 20; i++ {
			model = appendTestAssistant(model, fmt.Sprintf("line %d", i))
		}
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
		model = updated.(Model)
		if model.scroll == 0 {
			t.Fatal("PageUp should scroll transcript history")
		}
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		if updated.(Model).scroll >= model.scroll {
			t.Fatal("PageDown should scroll toward the transcript bottom")
		}
	})
}

func TestBaseComposerRoutingPreservesConfirmationAndQuestionPriority(t *testing.T) {
	t.Run("confirmation", func(t *testing.T) {
		model := NewModel(Config{})
		model.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file", Risk: "medium"}
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
		next := updated.(Model)
		if next.verbose || next.agent.Pending == nil {
			t.Fatal("confirmation must retain priority over base shortcuts")
		}
		updated, cmd := next.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
		next = updated.(Model)
		if cmd != nil || next.agent.Pending == nil {
			t.Fatal("confirmation must swallow Ctrl+G instead of opening the editor")
		}
	})

	t.Run("question", func(t *testing.T) {
		model := NewModel(Config{})
		model.question = &ask.Question{Prompt: "choose", Options: []ask.Option{{Label: "one"}, {Label: "two"}}}
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
		next := updated.(Model)
		if next.verbose || next.question == nil {
			t.Fatal("question must retain priority over base shortcuts")
		}
		updated, cmd := next.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
		next = updated.(Model)
		if cmd != nil || next.question == nil {
			t.Fatal("question must swallow Ctrl+G instead of opening the editor")
		}
	})
}
