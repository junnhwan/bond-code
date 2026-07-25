package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/command"
)

// fakeSessionManager records mutations for assertion without touching disk.
type fakeSessionManager struct {
	entries []SessionInfo
	deleted []string
	titles  map[string]string
	pins    map[string]bool
}

func newFakeSessionManager(entries []SessionInfo) *fakeSessionManager {
	return &fakeSessionManager{
		entries: entries,
		titles:  map[string]string{},
		pins:    map[string]bool{},
	}
}

func (f *fakeSessionManager) List() ([]SessionInfo, error) { return f.entries, nil }
func (f *fakeSessionManager) Delete(id string) error {
	f.deleted = append(f.deleted, id)
	out := f.entries[:0]
	for _, e := range f.entries {
		if e.ID != id {
			out = append(out, e)
		}
	}
	f.entries = out
	return nil
}
func (f *fakeSessionManager) SetTitle(id, title string) error {
	f.titles[id] = title
	return nil
}
func (f *fakeSessionManager) SetPinned(id string, pinned bool) error {
	f.pins[id] = pinned
	return nil
}

// TestSessionManagerOpensAndLists checks the overlay loads entries and lands the
// cursor on the active session.
func TestSessionManagerOpensAndLists(t *testing.T) {
	entries := []SessionInfo{
		{ID: "s1", Title: "older", LastActive: earlierTime(2)},
		{ID: "s2", Title: "current", Active: true, LastActive: earlierTime(1)},
	}
	mgr := newFakeSessionManager(entries)
	model := NewModel(Config{SessionManager: mgr})
	model = model.openSessionManager()
	if !model.overlay.active() || model.overlay.kind != overlaySessions {
		t.Fatalf("expected sessions overlay, got kind=%v", model.overlay.kind)
	}
	if len(model.overlay.sessions.entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(model.overlay.sessions.entries))
	}
	if model.overlay.sessions.selected != 1 {
		t.Fatalf("expected cursor on the active session (idx 1), got %d", model.overlay.sessions.selected)
	}
}

// TestSessionManagerWithoutControllerDegradesGracefully checks the overlay opens
// with an in-view error when no manager is wired (nil), never a crash.
func TestSessionManagerWithoutControllerDegradesGracefully(t *testing.T) {
	model := NewModel(Config{})
	model = model.openSessionManager()
	if !model.overlay.active() {
		t.Fatal("expected overlay to open even without a controller")
	}
	if model.overlay.sessions.loadErr == "" {
		t.Fatal("expected a load error when SessionManager is nil")
	}
}

func TestSessionManagerFooterOnlyAdvertisesReachableOverlayActions(t *testing.T) {
	model := NewModel(Config{SessionManager: newFakeSessionManager([]SessionInfo{{ID: "s1", Title: "current", Active: true}})})
	model.width = 100
	model.height = 12
	model = model.openSessionManager()

	view := model.renderSessionManager()
	for _, want := range []string{"↑/↓ move", "space select", "enter actions", "d delete", "p pin", "esc close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("session manager footer missing reachable action %q:\n%s", want, view)
		}
	}
	for _, forbidden := range []string{"<leader>", "Ctrl+X", "Ctrl+E", "? help", "/session"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("session manager leaked legacy guidance %q:\n%s", forbidden, view)
		}
	}
}

// TestSessionManagerSpaceMarksAndBulkDelete confirms multi-select: space marks
// rows, enter opens a bulk confirm, Yes deletes every marked id.
func TestSessionManagerSpaceMarksAndBulkDelete(t *testing.T) {
	entries := []SessionInfo{
		{ID: "s1", Title: "one"},
		{ID: "s2", Title: "two"},
		{ID: "s3", Title: "current", Active: true},
	}
	mgr := newFakeSessionManager(entries)
	model := NewModel(Config{SessionManager: mgr})
	model = model.openSessionManager()
	// Cursor starts on active s3. Move to s1, mark; move to s2, mark.
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyUp})
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyUp})
	if model.overlay.sessions.entries[model.overlay.sessions.selected].ID != "s1" {
		t.Fatalf("cursor want s1, got %s", model.overlay.sessions.entries[model.overlay.sessions.selected].ID)
	}
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyDown})
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if len(model.overlay.sessions.marked) != 2 {
		t.Fatalf("expected 2 marked, got %v", model.overlay.sessions.marked)
	}
	// Active session cannot be marked.
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyDown})
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if model.overlay.sessions.marked["s3"] {
		t.Fatal("active session must not be markable")
	}
	// Enter with marks → confirm; arm Yes + enter.
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if model.overlay.kind != overlayConfirm {
		t.Fatalf("expected bulk confirm, got kind=%v", model.overlay.kind)
	}
	if !strings.Contains(model.overlay.confirm.title, "2") {
		t.Fatalf("expected multi-delete title, got %q", model.overlay.confirm.title)
	}
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(mgr.deleted) != 2 {
		t.Fatalf("expected 2 deletes, got %v", mgr.deleted)
	}
	got := map[string]bool{}
	for _, id := range mgr.deleted {
		got[id] = true
	}
	if !got["s1"] || !got["s2"] {
		t.Fatalf("deleted ids want s1+s2, got %v", mgr.deleted)
	}
}

// TestSessionManagerPinKeyTogglesViaController checks the 'p' quick key flips
// pin state through the controller and refreshes the in-memory list.
func TestSessionManagerPinKeyTogglesViaController(t *testing.T) {
	entries := []SessionInfo{{ID: "s1", Title: "a"}}
	mgr := newFakeSessionManager(entries)
	model := NewModel(Config{SessionManager: mgr})
	model = model.openSessionManager()
	// entries start unpinned; 'p' pins s1.
	next, _, handled := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if !handled {
		t.Fatal("expected p to be handled")
	}
	if !mgr.pins["s1"] {
		t.Fatal("expected controller.SetPinned(s1, true) call")
	}
	_ = next
}

// TestSessionManagerActionsMenuOffersSwitch checks Enter opens a per-session
// menu containing the switch action (disabled on the active session itself).
func TestSessionManagerActionsMenuOffersSwitch(t *testing.T) {
	entries := []SessionInfo{
		{ID: "s1", Title: "inactive"},
		{ID: "s2", Title: "current", Active: true},
	}
	mgr := newFakeSessionManager(entries)
	model := NewModel(Config{SessionManager: mgr})
	model = model.openSessionManager()
	// cursor is on s2 (active). Move up to s1.
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyUp})
	if model.overlay.sessions.entries[model.overlay.sessions.selected].ID != "s1" {
		t.Fatal("expected cursor on s1 after up")
	}
	// Enter opens the actions menu.
	next, _, handled := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled || next.overlay.kind != overlayMenu {
		t.Fatalf("expected enter to open menu overlay, got kind=%v handled=%v", next.overlay.kind, handled)
	}
	items := next.overlay.menu.items
	if len(items) == 0 || items[0].label != "Switch to this session" {
		t.Fatalf("expected first menu item to be switch, got %+v", items)
	}
}

// TestQuickSwitchNoOpWhenNoManager checks the compatibility helper is safe with no manager.
func TestQuickSwitchNoOpWhenNoManager(t *testing.T) {
	model := NewModel(Config{})
	next, _ := model.quickSwitchSession(1)
	if next.overlay.active() {
		t.Fatal("expected quick switch to be a no-op without a manager")
	}
}

// TestQuickSwitchTargetsFirstEntry checks slot 1 hits the top of the list.
func TestQuickSwitchTargetsFirstEntry(t *testing.T) {
	entries := []SessionInfo{{ID: "s1"}, {ID: "s2"}}
	mgr := newFakeSessionManager(entries)
	switched := ""
	model := NewModel(Config{
		SessionManager: mgr,
		CommandEnv: command.Env{
			SwitchSession: func(id string) error { switched = id; return nil },
		},
		ReloadSessionSeed: func(string) []SeedMessage { return nil },
	})
	model.quickSwitchSession(1)
	if switched != "s1" {
		t.Fatalf("expected quick switch to target s1, got %q", switched)
	}
}

// earlierTime returns a time.Time that many hours ago, for ordering test fixtures.
func earlierTime(hoursAgo int) time.Time {
	return time.Now().Add(-time.Duration(hoursAgo) * time.Hour)
}
