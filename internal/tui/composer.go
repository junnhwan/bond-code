package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const composerMaxHeight = 6

func newComposerState(inputWidth int, suggestions *SuggestionList) ComposerState {
	input := newTextarea(inputWidth)
	return ComposerState{
		Input:        input,
		Suggestions:  suggestions,
		HistoryIndex: -1,
	}
}

func newTextarea(inputWidth int) textarea.Model {
	input := textarea.New()
	input.Placeholder = "Build anything"
	input.Prompt = string(rune(0x276f)) + " "
	fs := input.FocusedStyle
	fs.Prompt = lipgloss.NewStyle().Foreground(DefaultTheme.Accent).Bold(true)
	fs.Text = lipgloss.NewStyle().Foreground(DefaultTheme.Text)
	fs.Placeholder = lipgloss.NewStyle().Foreground(DefaultTheme.Dim).Italic(true)
	input.FocusedStyle = fs
	bs := input.BlurredStyle
	bs.Prompt = lipgloss.NewStyle().Foreground(DefaultTheme.Dim)
	bs.Text = lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted)
	bs.Placeholder = lipgloss.NewStyle().Foreground(DefaultTheme.Dim)
	input.BlurredStyle = bs
	input.ShowLineNumbers = false
	input.MaxHeight = composerMaxHeight
	input.SetHeight(1)
	input.SetWidth(inputWidth)
	_ = input.Focus()
	return input
}

func (c ComposerState) value() string {
	return c.Input.Value()
}

func (c ComposerState) setValue(value string) ComposerState {
	c.Input.SetValue(value)
	return c.syncHeight()
}

func (c ComposerState) clear() ComposerState {
	c.Input.SetValue("")
	c.HistoryIndex = -1
	c.HistoryDraft = ""
	c.Pastes = nil
	c = c.clearRawPasteTracking()
	return c.syncHeight().updateSuggestions()
}

// addPaste appends a collapsed paste payload (Phase 5C.1). The marker shows in
// the composer chip row; the original text is re-expanded on submit.
func (c ComposerState) addPaste(text string) ComposerState {
	c.Pastes = append(c.Pastes, PasteEntry{Marker: pasteMarker(text), Text: text})
	return c
}

// pasteMarker summarizes a paste for the chip row: line count for multi-line
// pastes, character count for single-line long ones.
func pasteMarker(text string) string {
	if lines := strings.Count(text, "\n") + 1; lines >= 3 {
		return fmt.Sprintf("[Pasted ~%d lines]", lines)
	}
	return fmt.Sprintf("[Pasted %d chars]", len(text))
}

// expandedValue returns the typed prompt with all collapsed pastes re-expanded
// (each appended on its own), so the model receives the full pasted content
// rather than the markers.
func (c ComposerState) expandedValue() string {
	value := c.Input.Value()
	for _, p := range c.Pastes {
		value += "\n" + p.Text
	}
	return value
}

// shouldCollapsePaste reports whether a pasted payload is large enough to
// collapse into a chip. Multi-line (>=3 lines) or long (>150 chars) pastes
// collapse; single-line short pastes go through the textarea as ordinary input.
func shouldCollapsePaste(text string) bool {
	if strings.Count(text, "\n") >= 2 { // 2 newlines => 3 lines
		return true
	}
	return len(text) > 150
}

const (
	rawPasteBurstGap           = 75 * time.Millisecond
	rawPasteEnterWindow        = 250 * time.Millisecond
	rawPasteMaxAverageInterval = 25 * time.Millisecond
	rawPasteMinBurstRunes      = 3
)

func (c ComposerState) observeRawPasteRunes(now time.Time, count int) ComposerState {
	if count <= 0 {
		return c
	}
	gap := now.Sub(c.RawPasteCandidateAt)
	if c.RawPasteCandidateAt.IsZero() || gap < 0 || gap > rawPasteBurstGap {
		c.RawPasteBurstStartedAt = now
		c.RawPasteBurstRunes = 0
		c.RawPasteActive = false
	}
	if c.RawPasteBurstStartedAt.IsZero() {
		c.RawPasteBurstStartedAt = now
	}
	c.RawPasteBurstRunes += count
	c.RawPasteCandidateAt = now
	return c
}

func (c ComposerState) consumeRawPasteEnter(now time.Time) (ComposerState, bool) {
	gap := now.Sub(c.RawPasteCandidateAt)
	if c.RawPasteCandidateAt.IsZero() || gap < 0 || gap > rawPasteEnterWindow {
		return c.clearRawPasteTracking(), false
	}

	fastBurst := false
	if c.RawPasteBurstRunes >= rawPasteMinBurstRunes {
		intervals := c.RawPasteBurstRunes - 1
		span := c.RawPasteCandidateAt.Sub(c.RawPasteBurstStartedAt)
		fastBurst = span >= 0 && span/time.Duration(intervals) <= rawPasteMaxAverageInterval
	}
	if !c.RawPasteActive && !fastBurst {
		return c.clearRawPasteTracking(), false
	}

	// Keep paste mode across adjacent lines, including blank lines. A pause
	// longer than rawPasteBurstGap before new runes leaves paste mode.
	c.RawPasteActive = true
	c.RawPasteCandidateAt = now
	c.RawPasteBurstStartedAt = time.Time{}
	c.RawPasteBurstRunes = 0
	return c, true
}

func (c ComposerState) clearRawPasteTracking() ComposerState {
	c.RawPasteCandidateAt = time.Time{}
	c.RawPasteBurstStartedAt = time.Time{}
	c.RawPasteBurstRunes = 0
	c.RawPasteActive = false
	return c
}

func (c ComposerState) insertNewline() ComposerState {
	// Insert the newline at the cursor rather than appending to the end, so
	// editing in the middle of a draft and pressing Alt+Enter breaks the line
	// under the cursor instead of at the tail. InsertString is a pointer
	// receiver method; taking the address of the value field mutates it in
	// place before we copy the state back.
	c.Input.InsertString("\n")
	c.HistoryIndex = -1
	c.HistoryDraft = ""
	return c.syncHeight().updateSuggestions()
}

func (c ComposerState) rememberPrompt(prompt string) ComposerState {
	if shouldSkipPromptHistory(prompt) {
		c.HistoryIndex = -1
		c.HistoryDraft = ""
		return c
	}
	prompt = strings.TrimSpace(prompt)
	if len(c.History) == 0 || c.History[len(c.History)-1] != prompt {
		c.History = append(c.History, prompt)
	}
	c.HistoryIndex = -1
	c.HistoryDraft = ""
	return c
}

func (c ComposerState) canUseHistory() bool {
	if len(c.History) == 0 || strings.Contains(c.Input.Value(), "\n") {
		return false
	}
	return c.Input.LineInfo().RowOffset == 0
}

func (c ComposerState) previousHistory() ComposerState {
	if len(c.History) == 0 {
		return c
	}
	if c.HistoryIndex == -1 {
		c.HistoryDraft = c.Input.Value()
		c.HistoryIndex = len(c.History) - 1
	} else if c.HistoryIndex > 0 {
		c.HistoryIndex--
	}
	c.Input.SetValue(c.History[c.HistoryIndex])
	return c.syncHeight().updateSuggestions()
}

func (c ComposerState) nextHistory() ComposerState {
	if c.HistoryIndex == -1 {
		return c
	}
	if c.HistoryIndex < len(c.History)-1 {
		c.HistoryIndex++
		c.Input.SetValue(c.History[c.HistoryIndex])
	} else {
		c.HistoryIndex = -1
		c.Input.SetValue(c.HistoryDraft)
		c.HistoryDraft = ""
	}
	return c.syncHeight().updateSuggestions()
}

func (c ComposerState) updateSuggestions() ComposerState {
	if c.Suggestions == nil {
		return c
	}
	value := c.Input.Value()
	if strings.HasPrefix(value, "/") && !strings.Contains(value, " ") {
		c.Suggestions.Show(strings.TrimPrefix(value, "/"))
	} else {
		c.Suggestions.Hide()
	}
	return c
}

func (c ComposerState) commandFilter() string {
	value := c.Input.Value()
	if !strings.HasPrefix(value, "/") {
		return ""
	}
	parts := strings.SplitN(value, " ", 2)
	return strings.TrimPrefix(parts[0], "/")
}

func (c ComposerState) syncHeight() ComposerState {
	c.Input.SetHeight(composerHeightForWidth(c.Input.Value(), c.Input.Width()))
	return c
}

func composerHeightForWidth(value string, width int) int {
	if width < 1 {
		width = 1
	}

	height := 0
	for _, line := range strings.Split(value, "\n") {
		// The textarea renders a cursor cell after the value. A line that exactly
		// fills the content width therefore needs one more visual row.
		wrapped := ansi.Wordwrap(line, width, "")
		wrapped = ansi.Hardwrap(wrapped, width, true)
		rows := strings.Count(wrapped, "\n") + 1
		if rows == 1 && ansi.StringWidth(line) == width {
			rows++
		}
		height += rows
		if height >= composerMaxHeight {
			return composerMaxHeight
		}
	}
	return max(1, height)
}

func (m Model) inputValue() string {
	return m.composer.value()
}

func (m Model) promptHistory() []string {
	return append([]string(nil), m.composer.History...)
}
