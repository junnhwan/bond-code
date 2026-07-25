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
func TestPaletteOpenListsActions(t *testing.T) {
	model := NewModel(Config{})
	model = model.openPalette()
	if !model.overlay.active() || model.overlay.kind != overlayPalette {
		t.Fatalf("expected palette overlay active, got kind=%v", model.overlay.kind)
	}
	if len(model.overlay.palette.filtered) == 0 {
		t.Fatal("expected palette to list actions even with no registry")
	}
	// View actions (verbose/rail/plan/…) must always be present so the palette
	// is useful without any slash commands configured.
	titles := paletteTitles(model.overlay.palette.filtered)
	if !containsStr(titles, "Toggle verbose tool output") {
		t.Fatalf("expected verbose toggle in palette, got %v", titles)
	}
}

// TestPaletteFiltersByQuery checks typing narrows the list via fuzzyScore.
func TestPaletteFiltersByQuery(t *testing.T) {
	model := NewModel(Config{})
	model = model.openPalette()
	model = sendOverlayRunes(model, "verbose")
	for _, a := range model.overlay.palette.filtered {
		// Every surviving action must mention the query in title, id, or category.
		if !strings.Contains(strings.ToLower(a.Title+a.ID+a.Category), "verbose") {
			t.Fatalf("unexpected non-matching action survived filter: %s", a.Title)
		}
	}
	if len(model.overlay.palette.filtered) == 0 {
		t.Fatal("expected 'verbose' to match the verbose toggle action")
	}
}

// TestPaletteRunsActionAndCloses checks enter runs the selection and closes the
// overlay, leaving the base view with no modal.
func TestPaletteRunsActionAndCloses(t *testing.T) {
	model := NewModel(Config{})
	model = model.openPalette()
	model = sendOverlayRunes(model, "verbose")
	before := model.verbose
	next, _, handled := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("expected enter to be handled by the palette")
	}
	if next.overlay.active() {
		t.Fatal("expected palette to close after running an action")
	}
	if next.verbose == before {
		t.Fatal("expected the verbose toggle action to flip verbose")
	}
}

// TestOverlaySwallowsUnrelatedKey ensures a modal never leaks stray keys to the
// composer: an unrelated rune keeps the alert open and reports handled=true.
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
func TestPaletteIgnoresControlRunes(t *testing.T) {
	model := NewModel(Config{})
	model = model.openPalette()
	full := len(model.overlay.palette.filtered)

	next, _, handled := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x10}})
	if !handled {
		t.Fatal("expected the control-rune key to be handled (swallowed)")
	}
	if got := next.overlay.palette.query; got != "" {
		t.Fatalf("expected query to stay empty after control rune, got %q", got)
	}
	if len(next.overlay.palette.filtered) != full {
		t.Fatalf("expected full action list (%d) to survive, got %d", full, len(next.overlay.palette.filtered))
	}

	// Sanity: a printable rune still types normally.
	next, _, _ = next.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if got := next.overlay.palette.query; got != "v" {
		t.Fatalf("expected printable 'v' to type into query, got %q", got)
	}
}

// TestPromptOverlayIgnoresControlRunes mirrors the palette regression for the
// text-input dialog: a stray control rune must not pollute the prompt value.
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
func TestEscClosesEveryOverlayVariant(t *testing.T) {
	cases := []struct {
		name string
		open func(Model) Model
	}{
		{"palette", func(m Model) Model { return m.openPalette() }},
		{"menu", func(m Model) Model {
			return m.openMenu("M", "", []menuItem{{label: "x", run: func(m Model) (Model, tea.Cmd) { return m, nil }}})
		}},
		{"alert", func(m Model) Model { return m.openAlert("A", "b", toastWarn) }},
		{"confirm", func(m Model) Model {
			return m.openConfirm("C", "d", false, func(m Model, ok bool) (Model, tea.Cmd) { return m, nil })
		}},
		{"prompt", func(m Model) Model {
			return m.openPrompt("P", "q", "", func(m Model, s string) (Model, tea.Cmd) { return m, nil })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := tc.open(NewModel(Config{}))
			next, _, handled := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEsc})
			if !handled {
				t.Fatal("expected esc to be handled")
			}
			if next.overlay.active() {
				t.Fatalf("expected esc to close the %s overlay", tc.name)
			}
		})
	}
}

// sendOverlayRunes types a string into the active overlay one rune at a time via
// the real dispatch path, so refine/repaint runs as it would in production.
func sendOverlayRunes(m Model, s string) Model {
	for _, r := range s {
		next, _, _ := m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next
	}
	return m
}

func paletteTitles(actions []Action) []string {
	out := make([]string, len(actions))
	for i, a := range actions {
		out[i] = a.Title
	}
	return out
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestShortPaletteKeepsSelectedActionVisible(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 8)
	actions := make([]Action, 7)
	for i := range actions {
		actions[i] = Action{Title: "other palette action", Category: "test"}
	}
	actions[5].Title = "selected palette action"
	model.overlay = overlayState{
		kind: overlayPalette,
		palette: paletteOverlay{
			actions:  actions,
			filtered: actions,
			selected: 5,
		},
	}

	view := model.View()
	assertViewFits(t, view, 80, 8)
	if !strings.Contains(view, "selected palette action") {
		t.Fatalf("short palette clipped the selected action at index 5:\n%s", view)
	}
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
