package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/command"
)

// TestBaseComposerRoutingCtrlXDoesNotArmWhichKey keeps the reusable legacy
// implementation unreachable from the base composer.
func TestBaseComposerRoutingCtrlXDoesNotArmWhichKey(t *testing.T) {
	model := NewModel(Config{})
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	next := updated.(Model)
	if next.leaderPending || next.whichKeyVisible || cmd != nil {
		t.Fatalf("Ctrl+X must not arm which-key, pending=%v visible=%v cmd=%v", next.leaderPending, next.whichKeyVisible, cmd != nil)
	}
}

// TestWhichKeyShowsOnlyWhileLeaderIdle checks the popup honors the msg only when
// the user is still idling on the leader layer; a fast follow-up key dismisses.
func TestWhichKeyShowsOnlyWhileLeaderIdle(t *testing.T) {
	model := NewModel(Config{})
	model.leaderPending = true
	// Timer fires while still pending -> visible.
	next, _ := model.Update(whichKeyShowMsg{})
	m := next.(Model)
	if !m.whichKeyVisible {
		t.Fatal("expected which-key visible when timer fires during leader idle")
	}
	// Already resolved (not pending) -> ignored.
	model2 := NewModel(Config{})
	model2.leaderPending = false
	next2, _ := model2.Update(whichKeyShowMsg{})
	if next2.(Model).whichKeyVisible {
		t.Fatal("expected which-key to stay hidden when leader is no longer pending")
	}
}

// TestLeaderKeyDismissesWhichKey checks resolving the leader hides the popup.
func TestLeaderKeyDismissesWhichKey(t *testing.T) {
	model := NewModel(Config{})
	model.leaderPending = true
	model.whichKeyVisible = true
	// 'q' resolves the leader (quit); handleLeaderKey clears visibility.
	next, _ := model.handleLeaderKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if next.whichKeyVisible {
		t.Fatal("expected which-key hidden after a leader key press")
	}
	if next.leaderPending {
		t.Fatal("expected leaderPending cleared after a leader key press")
	}
}

// TestRenderWhichKeyListsBindings checks the panel renders the canonical bindings.
func TestRenderWhichKeyListsBindings(t *testing.T) {
	model := NewModel(Config{})
	out := model.renderWhichKey()
	if out == "" {
		t.Fatal("expected non-empty which-key panel")
	}
	for _, want := range []string{"diff viewer", "session manager", "edit draft", "quick-switch"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected which-key panel to mention %q, got:\n%s", want, out)
		}
	}
}

// TestLeaderBindingsCoversCoreActions is a sanity check that the display list
// stays in sync with the dispatch as new bindings land.
func TestLeaderBindingsCoversCoreActions(t *testing.T) {
	keys := map[string]bool{}
	for _, b := range leaderBindings() {
		keys[b.key] = true
	}
	for _, want := range []string{"d", "t", "l", "p", "e", "n", "c", "s", "g", "b", "q"} {
		if !keys[want] {
			t.Errorf("leaderBindings missing key %q", want)
		}
	}
}

func TestClaudeCoreKeyActionHints(t *testing.T) {
	actions := buildActionList(NewModel(Config{}))
	shortcuts := make(map[string]string, len(actions))
	for _, action := range actions {
		shortcuts[action.ID] = action.Shortcut
	}
	for actionID, descriptorID := range map[string]string{
		"view.verbose":  "key.details",
		"prompt.stash":  "key.stash",
		"prompt.editor": "key.external-editor",
		"view.plan":     "key.mode-cycle",
	} {
		descriptor, ok := command.LookupDirectKeyDescriptor(descriptorID)
		if !ok {
			t.Fatalf("missing canonical direct-key descriptor %q", descriptorID)
		}
		if got := shortcuts[actionID]; got != descriptor.DisplayShortcut {
			t.Errorf("%s shortcut = %q, want canonical %q", actionID, got, descriptor.DisplayShortcut)
		}
	}
	for _, id := range []string{
		"view.thinking", "view.diff", "view.search", "view.history",
		"session.manage", "view.back", "view.forward",
	} {
		if got := shortcuts[id]; got != "" {
			t.Errorf("removed base route %s still advertises %q", id, got)
		}
	}
}
