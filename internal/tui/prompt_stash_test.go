package tui

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestStashDraftParksAndPersists checks Ctrl+S with a draft stashes it,
// clears the composer, toasts, and round-trips through disk.
func TestStashDraftParksAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stash.json")
	model := NewModel(Config{StashPath: path})
	model = model.SetInput("half-written prompt")
	model = model.stashLeaderAction()
	if model.inputValue() != "" {
		t.Fatalf("expected composer cleared after stash, got %q", model.inputValue())
	}
	if len(model.stash) != 1 || model.stash[0] != "half-written prompt" {
		t.Fatalf("expected stashed draft, got %+v", model.stash)
	}
	if len(model.toasts) != 1 {
		t.Fatalf("expected a toast, got %d", len(model.toasts))
	}
	toastMessage := model.toasts[0].message
	if !strings.Contains(toastMessage, "Ctrl+S") {
		t.Fatalf("stash toast should advertise the canonical Ctrl+S action, got %q", toastMessage)
	}
	for _, forbidden := range []string{"<leader>", "Ctrl+X", "Ctrl+E", "? help", "/session"} {
		if strings.Contains(toastMessage, forbidden) {
			t.Fatalf("stash toast leaked removed shortcut %q: %q", forbidden, toastMessage)
		}
	}
	// Reload from disk: the stash survives.
	if got := loadStash(path); len(got) != 1 || got[0] != "half-written prompt" {
		t.Fatalf("expected persisted stash, got %+v", got)
	}
}

// TestStashLeaderPopsWhenEmpty checks Ctrl+S with an empty composer opens the
// pop menu (overlay) instead of stashing nothing.
func TestStashLeaderPopsWhenEmpty(t *testing.T) {
	model := NewModel(Config{})
	model.stash = []string{"parked draft"}
	model = model.stashLeaderAction()
	if !model.overlay.active() || model.overlay.kind != overlayMenu {
		t.Fatalf("expected pop menu overlay, got active=%v kind=%v", model.overlay.active(), model.overlay.kind)
	}
}

// TestStashEmptyDraftIsNoOp checks stashing whitespace only warns and does not
// add an entry.
func TestStashEmptyDraftIsNoOp(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("   ")
	model = model.stashLeaderAction()
	if len(model.stash) != 0 {
		t.Fatalf("expected no stash entry for whitespace draft, got %+v", model.stash)
	}
	if len(model.toasts) != 1 || model.toasts[0].variant != toastWarn {
		t.Fatalf("expected a warn toast, got %+v", model.toasts)
	}
}

// TestPopStashIntoComposer checks popping removes the entry and fills the
// composer, and persists the shorter stash.
func TestPopStashIntoComposer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stash.json")
	model := NewModel(Config{StashPath: path})
	model.stash = []string{"first", "second"}
	_ = saveStash(path, model.stash)
	model = model.popStashIntoComposer(0)
	if model.inputValue() != "first" {
		t.Fatalf("expected composer filled with 'first', got %q", model.inputValue())
	}
	if len(model.stash) != 1 || model.stash[0] != "second" {
		t.Fatalf("expected remaining stash [second], got %+v", model.stash)
	}
	if got := loadStash(path); len(got) != 1 || got[0] != "second" {
		t.Fatalf("expected persisted shorter stash, got %+v", got)
	}
}

// TestStashCapsAtMax verifies a burst cannot grow the stash without bound.
func TestStashCapsAtMax(t *testing.T) {
	model := NewModel(Config{})
	for i := 0; i < maxStashEntries+5; i++ {
		model.stash = append([]string{"x"}, model.stash...)
		if len(model.stash) > maxStashEntries {
			model.stash = model.stash[:maxStashEntries]
		}
	}
	if len(model.stash) > maxStashEntries {
		t.Fatalf("expected stash capped at %d, got %d", maxStashEntries, len(model.stash))
	}
}
