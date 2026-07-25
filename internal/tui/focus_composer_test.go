package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestScrollbackFocusBlursComposerCursor(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 24)
	if !m.composer.Input.Focused() {
		t.Fatal("cold start should focus the prompt")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.focus != FocusScrollback {
		t.Fatalf("tab should enter scrollback, focus=%s", m.focus)
	}
	if m.composer.Input.Focused() {
		t.Fatal("scrollback focus must Blur the textarea (no blinking cursor)")
	}

	// Space focuses prompt without inserting a space.
	before := m.inputValue()
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = next.(Model)
	if m.focus != FocusComposer || !m.composer.Input.Focused() {
		t.Fatalf("space should focus prompt, focus=%s focused=%v", m.focus, m.composer.Input.Focused())
	}
	if m.inputValue() != before {
		t.Fatalf("space focus must not insert into draft, got %q", m.inputValue())
	}
}

func TestLetterAutoFocusesComposerAndInserts(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 24)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.focus != FocusScrollback {
		t.Fatalf("setup: want scrollback, got %s", m.focus)
	}
	if m.composer.Input.Focused() {
		t.Fatal("setup: prompt must be blurred")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = next.(Model)
	if m.focus != FocusComposer {
		t.Fatalf("letter should auto-focus composer, got %s", m.focus)
	}
	if !m.composer.Input.Focused() {
		t.Fatal("letter auto-focus must Focus the textarea")
	}
	if !strings.Contains(m.inputValue(), "h") {
		t.Fatalf("letter should insert into draft, got %q", m.inputValue())
	}
}

func TestClickComposerFocusesAndScrollbackBlurs(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 28)
	m.timeline = m.timeline.StartUserTurn("hello")
	m, _ = m.withFocus(FocusScrollback)
	if m.composer.Input.Focused() {
		t.Fatal("expected blurred prompt in scrollback")
	}

	// Find composer Y and click it.
	composerY := -1
	for y := 0; y < m.height; y++ {
		if m.resolveMouseHit(5, y).kind == mouseHitComposer {
			composerY = y
			break
		}
	}
	if composerY < 0 {
		t.Fatal("composer hit not found")
	}
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: 5, Y: composerY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if next.focus != FocusComposer || !next.composer.Input.Focused() {
		t.Fatalf("click composer should focus prompt, focus=%s focused=%v", next.focus, next.composer.Input.Focused())
	}
}

func TestFocusedComposerUsesActivePromptBorder(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 24)
	// Focused: active border token.
	focused := m.composerViewForWidth(40)
	// Blurred scrollback: idle border.
	m, _ = m.withFocus(FocusScrollback)
	blurred := m.composerViewForWidth(40)

	// Active rule is brighter (PromptActive) vs dim PromptBorder — rendered
	// strings differ when focus changes.
	if focused == blurred {
		t.Fatal("focused and blurred composer chrome should differ")
	}
	// Focused path uses bold active rule; both still have the horizontal rule.
	if !strings.Contains(ansi.Strip(focused), "─") || !strings.Contains(ansi.Strip(blurred), "─") {
		t.Fatalf("expected prompt rule in both states\nfocus=%q\nblur=%q", focused, blurred)
	}
}

func TestIsLetterAutoFocusRunes(t *testing.T) {
	if !isLetterAutoFocusRunes([]rune("a")) {
		t.Fatal("letter a should auto-focus")
	}
	if !isLetterAutoFocusRunes([]rune("Hello")) {
		t.Fatal("word should auto-focus")
	}
	if isLetterAutoFocusRunes([]rune(" ")) {
		t.Fatal("space must not letter-auto-focus")
	}
	if isLetterAutoFocusRunes([]rune{0x01}) {
		t.Fatal("control rune must not auto-focus")
	}
}

func TestSpaceFocusesComposerWithNonEmptyDraft(t *testing.T) {
	// Criterion 2: Space focuses without inserting — even when draft is non-empty.
	m := NewModel(Config{}).SetSize(80, 24)
	m.composer = m.composer.setValue("draft already here")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.focus != FocusScrollback {
		t.Fatalf("setup: want scrollback, got %s", m.focus)
	}
	before := m.inputValue()
	if before == "" {
		t.Fatal("setup: draft must be non-empty")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = next.(Model)
	if m.focus != FocusComposer {
		t.Fatalf("Space from scrollback must focus composer with non-empty draft, got %s", m.focus)
	}
	if !m.composer.Input.Focused() {
		t.Fatal("Space focus must Focus the textarea")
	}
	if m.inputValue() != before {
		t.Fatalf("Space must not insert into draft, before=%q after=%q", before, m.inputValue())
	}
	if strings.HasSuffix(m.inputValue(), " ") && before != m.inputValue() {
		t.Fatal("Space must not append a space character")
	}
}
