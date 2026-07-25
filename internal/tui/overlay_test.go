package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestToastPushAndEvict verifies the toast lifecycle: push appends, and a toast
// past its expiry is dropped by tickToasts without touching still-fresh ones.
func TestToastPushAndEvict(t *testing.T) {
	model := NewModel(Config{})
	model = model.pushToast("fresh", toastInfo)
	model = model.pushToast("stale", toastError)
	if len(model.toasts) != 2 {
		t.Fatalf("expected 2 toasts, got %d", len(model.toasts))
	}

	// Force one toast to be expired.
	model.toasts[1].expireAt = time.Now().Add(-time.Second)
	model = model.tickToasts()

	if len(model.toasts) != 1 {
		t.Fatalf("expected 1 toast after eviction, got %d", len(model.toasts))
	}
	if model.toasts[0].message != "fresh" {
		t.Fatalf("expected the fresh toast to survive, got %q", model.toasts[0].message)
	}
}

// TestToastStackIsCapped guards against a notification burst flooding the corner.
func TestToastStackIsCapped(t *testing.T) {
	model := NewModel(Config{})
	for i := 0; i < 10; i++ {
		model = model.pushToast("n", toastInfo)
	}
	if len(model.toasts) > 4 {
		t.Fatalf("expected toast stack capped at 4, got %d", len(model.toasts))
	}
}

// TestPaletteOpenListsActions checks Ctrl+P surfaces the registry + view actions.
func TestOverlaySwallowsUnrelatedKey(t *testing.T) {
	model := NewModel(Config{})
	model = model.openAlert("Test", "notice", toastInfo)
	next, _, handled := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if !handled {
		t.Fatal("expected overlay to swallow an unrelated key")
	}
	if !next.overlay.active() {
		t.Fatal("expected alert to stay open after an unrelated key")
	}
}

// TestMenuRunsSelectedItem checks a menu runs the highlighted item on enter and
// that disabled items are skipped during cursor navigation.
func TestMenuRunsSelectedItem(t *testing.T) {
	model := NewModel(Config{})
	ran := ""
	model = model.openMenu("Actions", "", []menuItem{
		{label: "First", run: func(m Model) (Model, tea.Cmd) { ran = "first"; return m, nil }},
		{label: "Disabled", disabled: true, run: func(m Model) (Model, tea.Cmd) { ran = "disabled"; return m, nil }},
		{label: "Second", run: func(m Model) (Model, tea.Cmd) { ran = "second"; return m, nil }},
	})
	if model.overlay.menu.selected != 0 {
		t.Fatalf("expected initial selection on first enabled item, got %d", model.overlay.menu.selected)
	}
	// Move down: must skip the disabled middle item and land on Third.
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyDown})
	if model.overlay.menu.selected != 2 {
		t.Fatalf("expected cursor to skip disabled item to index 2, got %d", model.overlay.menu.selected)
	}
	next, _, handled := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled || ran != "second" {
		t.Fatalf("expected enter to run 'second', ran=%q handled=%v", ran, handled)
	}
	if next.overlay.active() {
		t.Fatal("expected menu to close after running an item")
	}
}

// TestConfirmOverlayReportsDecision covers the Yes/No dialog for the non-high
// risk path: 'y' confirms immediately, 'n'/esc rejects.
func TestConfirmOverlayReportsDecision(t *testing.T) {
	model := NewModel(Config{})
	var approved bool
	decided := 0
	model = model.openConfirm("Delete?", "sure?", false, func(m Model, ok bool) (Model, tea.Cmd) {
		decided++
		approved = ok
		return m, nil
	})
	next, _, _ := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if decided != 1 || !approved {
		t.Fatalf("expected y to confirm, decided=%d approved=%v", decided, approved)
	}
	if next.overlay.active() {
		t.Fatal("expected confirm to close after deciding")
	}
}

// TestConfirmOverlayLeftRightMatchesVisualYesNo ensures ← → follow the rendered
// order "Yes    No" (Yes left, No right) and wrap so both directions always move.
func TestConfirmOverlayLeftRightMatchesVisualYesNo(t *testing.T) {
	model := NewModel(Config{})
	model = model.openConfirm("Delete?", "sure?", true, func(m Model, ok bool) (Model, tea.Cmd) {
		return m, nil
	})
	// Default is No (safe).
	if model.overlay.confirm.approve {
		t.Fatal("expected default selection on No")
	}
	// Left from No → Yes (Yes is left).
	next, _, _ := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyLeft})
	if !next.overlay.confirm.approve {
		t.Fatal("left from No should select Yes (left option)")
	}
	// Left from Yes wraps to No.
	next, _, _ = next.handleOverlayKey(tea.KeyMsg{Type: tea.KeyLeft})
	if next.overlay.confirm.approve {
		t.Fatal("left from Yes should wrap to No")
	}
	// Right from No wraps to Yes.
	next, _, _ = next.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRight})
	if !next.overlay.confirm.approve {
		t.Fatal("right from No should wrap to Yes")
	}
	// Right from Yes → No.
	next, _, _ = next.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRight})
	if next.overlay.confirm.approve {
		t.Fatal("right from Yes should select No (right option)")
	}
}

// TestHighRiskConfirmRequiresExplicitYes mirrors the safety confirmation rule:
// on a high-risk confirm, bare Enter on No must not confirm; the user must arm
// Yes first.
func TestHighRiskConfirmRequiresExplicitYes(t *testing.T) {
	model := NewModel(Config{})
	decided := false
	model = model.openConfirm("Risky", "sure?", true, func(m Model, ok bool) (Model, tea.Cmd) {
		decided = true
		return m, nil
	})
	// Bare enter on No (default): no decision.
	next, _, _ := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if decided {
		t.Fatal("expected bare enter on No to NOT confirm a high-risk action")
	}
	if !next.overlay.active() {
		t.Fatal("expected high-risk confirm to stay open after bare enter")
	}
	// Arm Yes then Enter: confirms.
	next, _, _ = next.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	next, _, _ = next.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !decided {
		t.Fatal("expected y+enter to confirm a high-risk action")
	}
}

// TestPromptOverlaySubmitsText checks the text-input dialog accumulates typed
// runes and submits the trimmed value on enter.
func TestPromptOverlaySubmitsText(t *testing.T) {
	model := NewModel(Config{})
	var submitted string
	model = model.openPrompt("Name", "enter a name", "", func(m Model, text string) (Model, tea.Cmd) {
		submitted = text
		return m, nil
	})
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("  hello  ")})
	next, _, _ := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if submitted != "hello" {
		t.Fatalf("expected trimmed submit 'hello', got %q", submitted)
	}
	if next.overlay.active() {
		t.Fatal("expected prompt overlay to close on submit")
	}
}

// TestPaletteIgnoresControlRunes is a regression test for a Windows-terminal
// bug where a bare Ctrl (or a Ctrl+letter combo) arrived as a KeyRunes event
// carrying a non-printable control rune (e.g. 0x10 for Ctrl+P). The raw byte
// used to be spliced into the query, invisibly zeroing the match list until
// backspace. The palette must filter such runes the way the composer textarea
// already does.
func TestPromptOverlayIgnoresControlRunes(t *testing.T) {
	model := NewModel(Config{})
	var submitted string
	model = model.openPrompt("Name", "enter a name", "", func(m Model, text string) (Model, tea.Cmd) {
		submitted = text
		return m, nil
	})
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x10}})
	model, _, _ = model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if submitted != "hi" {
		t.Fatalf("expected control rune to be filtered so submit is 'hi', got %q", submitted)
	}
}

// TestEscClosesEveryOverlayVariant is a table test that esc dismisses each
// overlay kind, so no modal can trap the user.
func sendOverlayRunes(m Model, s string) Model {
	for _, r := range s {
		next, _, _ := m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next
	}
	return m
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestShortMenuKeepsSelectedItemVisible(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 8)
	items := make([]menuItem, 7)
	for i := range items {
		items[i] = menuItem{label: "other menu item"}
	}
	items[5].label = "selected menu item"
	model = model.openMenu("Actions", "Choose one", items)
	model.overlay.menu.selected = 5

	view := model.View()
	assertViewFits(t, view, 80, 8)
	if !strings.Contains(view, "selected menu item") {
		t.Fatalf("short menu clipped the selected item at index 5:\n%s", view)
	}
}

func TestShortConfirmKeepsActiveControlsVisible(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 8)
	model = model.openConfirm(
		"Confirm action",
		strings.Repeat("long confirmation message ", 20),
		false,
		func(m Model, approved bool) (Model, tea.Cmd) { return m, nil },
	)

	view := model.View()
	assertViewFits(t, view, 80, 8)
	for _, want := range []string{"Yes", "❯ No", "enter confirm"} {
		if !strings.Contains(view, want) {
			t.Fatalf("short confirmation clipped active control %q:\n%s", want, view)
		}
	}
}

func TestOverlayNavigationUsesArrowEnterEscape(t *testing.T) {
	runs := []string{}
	open := func() Model {
		model := NewModel(Config{})
		return model.openMenu("Actions", "choose", []menuItem{
			{label: "first", run: func(m Model) (Model, tea.Cmd) { runs = append(runs, "first"); return m, nil }},
			{label: "second", run: func(m Model) (Model, tea.Cmd) { runs = append(runs, "second"); return m, nil }},
		})
	}

	model := open()
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.overlay.menu.selected != 1 {
		t.Fatalf("Down should select the second overlay item, got %d", model.overlay.menu.selected)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.overlay.menu.selected != 0 {
		t.Fatalf("Up should select the first overlay item, got %d", model.overlay.menu.selected)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay.active() || len(runs) != 1 || runs[0] != "second" {
		t.Fatalf("Enter should run the selected item and close, active=%v runs=%v", model.overlay.active(), runs)
	}

	model = open()
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	model = updated.(Model)
	if model.verbose || !model.overlay.active() {
		t.Fatal("overlay must retain priority over Ctrl+O")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.overlay.active() || len(runs) != 1 {
		t.Fatalf("Esc should dismiss without running an item, active=%v runs=%v", model.overlay.active(), runs)
	}
}
