package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInsertNewlineAtCursor(t *testing.T) {
	// Regression: Alt+Enter used to append "\n" to the end of the draft
	// regardless of cursor position, so editing mid-line and breaking the line
	// sent the newline to the tail. insertNewline must insert at the cursor.
	c := newComposerState(80, nil)
	c = c.setValue("hello world")
	// Park the cursor after "hello" (col 5).
	c.Input.SetCursor(5)
	c = c.insertNewline()
	if got := c.value(); got != "hello\n world" {
		t.Fatalf("newline should split at cursor, got %q", got)
	}

	// Cursor at the very start: newline prepends.
	c = newComposerState(80, nil)
	c = c.setValue("abc")
	c.Input.SetCursor(0)
	c = c.insertNewline()
	if got := c.value(); got != "\nabc" {
		t.Fatalf("newline at start should prepend, got %q", got)
	}
}

func TestShouldCollapsePaste(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"hello", false},
		{"line1\nline2", false}, // 2 lines — under threshold
		{"line1\nline2\nline3", true},
		{strings.Repeat("a", 150), false}, // exactly 150 — under threshold
		{strings.Repeat("a", 151), true},  // >150 chars
		{"", false},
	}
	for _, tc := range cases {
		if got := shouldCollapsePaste(tc.in); got != tc.want {
			t.Errorf("shouldCollapsePaste(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPasteMarker(t *testing.T) {
	if got := pasteMarker("a\nb\nc\nd"); got != "[Pasted ~4 lines]" {
		t.Fatalf("multi-line marker should summarize by line count, got %q", got)
	}
	if got := pasteMarker("ab"); got != "[Pasted 2 chars]" {
		t.Fatalf("single-line marker should summarize by char count, got %q", got)
	}
}

// TestComposerAddPasteAndExpand confirms a collapsed paste is kept out of the
// textarea, shown as a marker chip, and re-expanded onto the prompt at submit.
func TestComposerAddPasteAndExpand(t *testing.T) {
	c := newComposerState(76, nil)
	c = c.addPaste("line1\nline2\nline3")
	c = c.addPaste("x\ny\nz")
	c = c.setValue("prompt")

	expanded := c.expandedValue()
	for _, want := range []string{"prompt", "line1\nline2\nline3", "x\ny\nz"} {
		if !strings.Contains(expanded, want) {
			t.Errorf("expandedValue missing %q, got %q", want, expanded)
		}
	}

	// The textarea itself stays clean — the raw paste never reaches it.
	if strings.Contains(c.Input.Value(), "line1") {
		t.Fatalf("textarea should not hold the raw paste, got %q", c.Input.Value())
	}

	// clear() wipes pastes along with the input.
	c = c.clear()
	if len(c.Pastes) != 0 {
		t.Fatalf("clear should wipe pastes, got %d", len(c.Pastes))
	}
}

func TestRawMultilinePasteEnterDoesNotSubmitEachLine(t *testing.T) {
	m := NewModel(Config{})
	m = m.SetSize(100, 30)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first pasted line")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if got := m.inputValue(); got != "first pasted line\n" {
		t.Fatalf("raw paste newline should stay in one draft, got %q", got)
	}
	if len(m.timeline.Turns) != 0 {
		t.Fatalf("raw paste newline must not submit a user turn, got %d turns", len(m.timeline.Turns))
	}
}

func TestRawMultilinePasteKeepsAllLinesInOneDraft(t *testing.T) {
	m := NewModel(Config{}).SetSize(100, 30)
	for _, line := range []string{"first pasted line", "second pasted line"} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(line)})
		m = updated.(Model)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("final pasted line")})
	m = updated.(Model)

	want := "first pasted line\nsecond pasted line\nfinal pasted line"
	if got := m.inputValue(); got != want {
		t.Fatalf("raw multiline paste should remain one draft: got %q, want %q", got, want)
	}
	if len(m.timeline.Turns) != 0 {
		t.Fatalf("raw multiline paste must not submit intermediate turns, got %d", len(m.timeline.Turns))
	}
}

func TestRawMultilinePasteSurvivesWindowsConsoleBatchDelay(t *testing.T) {
	m := NewModel(Config{}).SetSize(100, 30).SetInput("first pasted line")
	now := time.Now()
	// Bubble Tea's Windows console reader peeks in batches. Model the fast rune
	// burst followed by an Enter that arrives 50 ms later. The old 20 ms timer
	// submitted this line and sent the remaining pasted lines into the queue.
	m.composer.RawPasteBurstStartedAt = now.Add(-66 * time.Millisecond)
	m.composer.RawPasteCandidateAt = now.Add(-50 * time.Millisecond)
	m.composer.RawPasteBurstRunes = len([]rune("first pasted line"))

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if got := m.inputValue(); got != "first pasted line\n" {
		t.Fatalf("batched Windows paste newline should stay in the draft, got %q", got)
	}
	if len(m.timeline.Turns) != 0 || len(m.agent.QueuedPrompts) != 0 {
		t.Fatalf("batched Windows paste must not submit or queue: turns=%d queue=%d", len(m.timeline.Turns), len(m.agent.QueuedPrompts))
	}
}

func TestRawPasteTrackerDoesNotMisclassifyHumanTyping(t *testing.T) {
	c := newComposerState(80, nil)
	now := time.Unix(100, 0)
	for range 5 {
		c = c.observeRawPasteRunes(now, 1)
		now = now.Add(40 * time.Millisecond)
	}

	var pasted bool
	c, pasted = c.consumeRawPasteEnter(now)
	if pasted {
		t.Fatal("ordinary human-speed typing must submit instead of inserting a paste newline")
	}
	if !c.RawPasteCandidateAt.IsZero() || c.RawPasteActive {
		t.Fatal("ordinary submit should clear raw-paste tracking")
	}
}

func TestRawPasteTrackerKeepsAdjacentBlankLines(t *testing.T) {
	c := newComposerState(80, nil)
	now := time.Unix(100, 0)
	for range 5 {
		c = c.observeRawPasteRunes(now, 1)
		now = now.Add(time.Millisecond)
	}

	var pasted bool
	c, pasted = c.consumeRawPasteEnter(now)
	if !pasted {
		t.Fatal("fast rune burst should enter raw-paste mode")
	}
	c, pasted = c.consumeRawPasteEnter(now.Add(10 * time.Millisecond))
	if !pasted {
		t.Fatal("adjacent blank line should remain part of the same raw paste")
	}
}

func TestExpiredRawPasteCandidateAllowsNormalSubmit(t *testing.T) {
	m := NewModel(Config{}).SetSize(100, 30)
	m.composer = m.composer.setValue("finished prompt")
	m.composer.RawPasteCandidateAt = time.Now().Add(-time.Second)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if got := m.inputValue(); got != "" {
		t.Fatalf("ordinary Enter should submit after paste window expires, got draft %q", got)
	}
	if len(m.timeline.Turns) != 1 {
		t.Fatalf("ordinary Enter should submit one turn, got %d", len(m.timeline.Turns))
	}
}

func TestRawMultilinePasteWithSingleRuneEventsDoesNotSubmitEachLine(t *testing.T) {
	m := NewModel(Config{}).SetSize(100, 30)
	for _, r := range "first pasted line" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if got := m.inputValue(); got != "first pasted line\n" {
		t.Fatalf("single-rune raw paste newline should stay in draft, got %q", got)
	}
	if len(m.timeline.Turns) != 0 {
		t.Fatalf("single-rune raw paste must not submit, got %d turns", len(m.timeline.Turns))
	}
}
