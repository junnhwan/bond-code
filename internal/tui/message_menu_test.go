package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
)

// TestMessageMenuBuildsForTurn checks the four canonical actions appear for a
// normal (idle, chat-configured) turn.
func TestMessageMenuBuildsForTurn(t *testing.T) {
	model := NewModel(Config{Chat: chatStub{}})
	model = model.beginUserTurn("fix the bug")
	model.navTurnIdx = 0
	model = model.openMessageMenuForTurn(0)
	if !model.overlay.active() || model.overlay.kind != overlayMenu {
		t.Fatalf("expected menu overlay, got active=%v kind=%v", model.overlay.active(), model.overlay.kind)
	}
	items := model.overlay.menu.items
	want := []string{"Copy prompt", "Edit in composer", "Re-run prompt", "Fork from here"}
	if len(items) != len(want) {
		t.Fatalf("expected %d items, got %d (%+v)", len(want), len(items), items)
	}
	for i, label := range want {
		if items[i].label != label {
			t.Errorf("item %d label=%q want %q", i, items[i].label, label)
		}
	}
}

// TestMessageMenuDisablesMutationsWhenBusy verifies copy/edit stay available
// while the agent runs, but re-run and fork (which would start/branch a turn)
// are disabled with a hint.
func TestMessageMenuDisablesMutationsWhenBusy(t *testing.T) {
	model := NewModel(Config{Chat: chatStub{}})
	model = model.beginUserTurn("hello")
	model.agent.Busy = true
	model.navTurnIdx = 0
	model = model.openMessageMenuForTurn(0)
	items := model.overlay.menu.items
	if items[0].disabled || items[1].disabled {
		t.Error("expected copy/edit to stay enabled while busy")
	}
	if !items[2].disabled {
		t.Error("expected re-run disabled while busy")
	}
	if !items[3].disabled {
		t.Error("expected fork disabled while busy")
	}
}

// TestMessageMenuRerunDisabledWithoutChat checks the re-run item reflects a
// missing agent rather than letting the user trigger a no-op turn.
func TestMessageMenuRerunDisabledWithoutChat(t *testing.T) {
	model := NewModel(Config{}) // no Chat
	model = model.beginUserTurn("hello")
	model.navTurnIdx = 0
	model = model.openMessageMenuForTurn(0)
	if !model.overlay.menu.items[2].disabled {
		t.Fatal("expected re-run disabled when no chat is configured")
	}
}

// TestMessageMenuEditFillsComposer exercises the edit closure: the prompt lands
// in the composer and turn navigation is disarmed.
func TestMessageMenuEditFillsComposer(t *testing.T) {
	model := NewModel(Config{})
	model = model.beginUserTurn("edit me please")
	model.navTurnIdx = 0
	model = model.openMessageMenuForTurn(0)
	next, _ := model.overlay.menu.items[1].run(model)
	if next.inputValue() != "edit me please" {
		t.Fatalf("expected composer filled with prompt, got %q", next.inputValue())
	}
	if next.navTurnIdx != -1 {
		t.Fatal("expected navTurnIdx reset after edit")
	}
}

// TestMessageMenuCopyToastsOnSuccess overrides the clipboard and checks the
// copy closure surfaces a success toast.
func TestMessageMenuCopyToastsOnSuccess(t *testing.T) {
	orig := copyToClipboard
	copyToClipboard = func(string) error { return nil }
	defer func() { copyToClipboard = orig }()

	model := NewModel(Config{})
	model = model.beginUserTurn("copy this")
	model.navTurnIdx = 0
	model = model.openMessageMenuForTurn(0)
	next, _ := model.overlay.menu.items[0].run(model)
	if len(next.toasts) != 1 {
		t.Fatalf("expected 1 toast on copy, got %d", len(next.toasts))
	}
	if next.toasts[0].variant != toastSuccess {
		t.Fatal("expected success toast variant")
	}
}

func TestMessageMenuOpensForSelectedTurn(t *testing.T) {
	model := NewModel(Config{Chat: chatStub{}})
	model = model.beginUserTurn("nav target")

	next := model.openMessageMenuForTurn(0)
	if !next.overlay.active() || next.overlay.kind != overlayMenu {
		t.Fatalf("expected selected turn to open a message menu, got active=%v kind=%v",
			next.overlay.active(), next.overlay.kind)
	}
}

// TestEnterSubmitsWhenNotNavigating guards the other branch: with no turn armed,
// Enter still submits the composer (no menu).
func TestEnterSubmitsWhenNotNavigating(t *testing.T) {
	model := NewModel(Config{Chat: chatStub{}})
	model = model.SetInput("a real draft")
	model.navTurnIdx = -1
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := updated.(Model)
	if next.overlay.active() {
		t.Fatal("expected no overlay when submitting a typed draft")
	}
}

// chatStub is a minimal ChatRunner so menu builders can see a non-nil Chat
// without hitting the network. Its methods are never actually invoked by these
// tests (they check menu state, not execution).
type chatStub struct{}

func (chatStub) RunWithEvents(_ context.Context, _ string, _ agent.EventSink) (*agent.RunResult, error) {
	return nil, nil
}
func (chatStub) Compact(_ context.Context, _ agent.EventSink) error { return nil }
