package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func isPureCtrlF(msg tea.KeyMsg) bool {
	return !msg.Alt && strings.EqualFold(msg.String(), "ctrl+f")
}

type composerDraftSnapshot struct {
	value               string
	cursorLine          int
	cursorColumn        int
	historyIndex        int
	historyDraft        string
	pastes              []PasteEntry
	rawPasteCandidateAt time.Time
}

func snapshotComposerDraft(composer ComposerState) composerDraftSnapshot {
	line := composer.Input.LineInfo()
	return composerDraftSnapshot{
		value:               composer.Input.Value(),
		cursorLine:          composer.Input.Line(),
		cursorColumn:        line.StartColumn + line.ColumnOffset,
		historyIndex:        composer.HistoryIndex,
		historyDraft:        composer.HistoryDraft,
		pastes:              append([]PasteEntry(nil), composer.Pastes...),
		rawPasteCandidateAt: composer.RawPasteCandidateAt,
	}
}

func (s composerDraftSnapshot) restore(composer ComposerState) ComposerState {
	composer.Input.SetValue(s.value)
	for composer.Input.Line() > s.cursorLine {
		composer.Input.CursorUp()
	}
	composer.Input.SetCursor(s.cursorColumn)
	composer.HistoryIndex = s.historyIndex
	composer.HistoryDraft = s.historyDraft
	composer.Pastes = append([]PasteEntry(nil), s.pastes...)
	composer.RawPasteCandidateAt = s.rawPasteCandidateAt
	return composer.syncHeight()
}

type reverseHistorySearchState struct {
	active          bool
	query           string
	normalizedQuery string
	selected        int
	original        *composerDraftSnapshot
	prompts         []string
	normalized      []string
	matchingIndices []int
}

func (m Model) startReverseHistorySearch() Model {
	if len(m.composer.History) == 0 {
		return m
	}
	original := snapshotComposerDraft(m.composer)
	m.reverseHistory = reverseHistorySearchState{
		active:   true,
		selected: 0,
		original: &original,
	}
	m, _ = m.syncReverseHistoryPrompts()
	m = m.rebuildReverseHistoryMatches()
	m.search = SearchState{Active: true, MatchIndex: -1}
	return m
}

func (m Model) startTranscriptSearch() Model {
	m.reverseHistory = reverseHistorySearchState{}
	if m.composer.Suggestions != nil {
		m.composer.Suggestions.Hide()
	}
	m.search = SearchState{Active: true, MatchIndex: -1}
	return m
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.reverseHistory.active {
		return m.handleReverseHistorySearchKey(msg)
	}
	key := strings.ToLower(strings.TrimSpace(msg.String()))
	switch {
	case key == "esc" || key == "ctrl+c":
		m.search = SearchState{}
		return m, nil, true
	case key == "enter" || key == "f3" || isPureCtrlF(msg):
		return m.advanceSearchMatch(1), nil, true
	case key == "shift+f3":
		return m.advanceSearchMatch(-1), nil, true
	case key == "backspace" || key == "ctrl+h":
		if m.search.Query != "" {
			_, size := utf8.DecodeLastRuneInString(m.search.Query)
			m.search.Query = m.search.Query[:len(m.search.Query)-size]
			m.search.MatchIndex = -1
			m = m.syncSearchToQuery()
		}
		return m, nil, true
	case len(msg.Runes) > 0:
		m.search.Query += string(msg.Runes)
		m.search.MatchIndex = -1
		m = m.syncSearchToQuery()
		return m, nil, true
	default:
		return m, nil, true
	}
}

func (m Model) handleReverseHistorySearchKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	var historyChanged bool
	m, historyChanged = m.syncReverseHistoryPrompts()
	if historyChanged {
		m = m.rebuildReverseHistoryMatches()
	}

	key := strings.ToLower(strings.TrimSpace(msg.String()))
	switch {
	case key == "esc" || key == "ctrl+c":
		return m.closeReverseHistorySearch(false), nil, true
	case key == "enter":
		return m.closeReverseHistorySearch(true), nil, true
	case key == "ctrl+r" || key == "up" || key == "ctrl+p":
		m = m.moveReverseHistorySelection(1)
		return m, nil, true
	case key == "down" || key == "ctrl+n":
		m = m.moveReverseHistorySelection(-1)
		return m, nil, true
	case key == "backspace" || key == "ctrl+h":
		if m.reverseHistory.query != "" {
			_, size := utf8.DecodeLastRuneInString(m.reverseHistory.query)
			m.reverseHistory.query = m.reverseHistory.query[:len(m.reverseHistory.query)-size]
			m.reverseHistory.selected = 0
			m = m.rebuildReverseHistoryMatches()
		}
		return m, nil, true
	case len(msg.Runes) > 0:
		m.reverseHistory.query += string(msg.Runes)
		m.reverseHistory.selected = 0
		m = m.rebuildReverseHistoryMatches()
		return m, nil, true
	default:
		return m, nil, true
	}
}

func (m Model) syncReverseHistoryPrompts() (Model, bool) {
	if samePromptHistory(m.reverseHistory.prompts, m.composer.History) {
		return m, false
	}
	m.reverseHistory.prompts = append([]string(nil), m.composer.History...)
	m.reverseHistory.normalized = make([]string, len(m.reverseHistory.prompts))
	for i, prompt := range m.reverseHistory.prompts {
		m.reverseHistory.normalized[i] = strings.ToLower(prompt)
	}
	return m, true
}

func (m Model) rebuildReverseHistoryMatches() Model {
	m.reverseHistory.normalizedQuery = strings.ToLower(m.reverseHistory.query)
	matches := make([]int, 0, len(m.reverseHistory.normalized))
	for i := len(m.reverseHistory.normalized) - 1; i >= 0; i-- {
		if strings.Contains(m.reverseHistory.normalized[i], m.reverseHistory.normalizedQuery) {
			matches = append(matches, i)
		}
	}
	m.reverseHistory.matchingIndices = matches
	if len(matches) == 0 {
		m.reverseHistory.selected = 0
	} else if m.reverseHistory.selected >= len(matches) {
		m.reverseHistory.selected = len(matches) - 1
	}
	return m
}

func (m Model) moveReverseHistorySelection(delta int) Model {
	if len(m.reverseHistory.matchingIndices) == 0 {
		m.reverseHistory.selected = 0
		return m
	}
	m.reverseHistory.selected += delta
	if m.reverseHistory.selected < 0 {
		m.reverseHistory.selected = 0
	}
	if m.reverseHistory.selected >= len(m.reverseHistory.matchingIndices) {
		m.reverseHistory.selected = len(m.reverseHistory.matchingIndices) - 1
	}
	return m
}

func (m Model) selectedReverseHistoryPrompt() (string, bool) {
	if len(m.reverseHistory.matchingIndices) == 0 {
		return "", false
	}
	selected := m.reverseHistory.selected
	if selected < 0 || selected >= len(m.reverseHistory.matchingIndices) {
		selected = 0
	}
	promptIndex := m.reverseHistory.matchingIndices[selected]
	if promptIndex < 0 || promptIndex >= len(m.reverseHistory.prompts) {
		return "", false
	}
	return m.reverseHistory.prompts[promptIndex], true
}

func (m Model) closeReverseHistorySearch(accept bool) Model {
	selected, found := m.selectedReverseHistoryPrompt()
	original := m.reverseHistory.original
	m.reverseHistory = reverseHistorySearchState{}
	m.search = SearchState{}
	if original == nil {
		return m
	}
	if !accept || !found {
		m.composer = original.restore(m.composer)
		return m.updateSuggestions()
	}
	m.composer.Input.SetValue(selected)
	m.composer.HistoryIndex = -1
	m.composer.HistoryDraft = ""
	m.composer.Pastes = nil
	m.composer.RawPasteCandidateAt = time.Time{}
	m.composer = m.composer.syncHeight()
	return m.updateSuggestions()
}

func (m Model) syncSearchToQuery() Model {
	if strings.TrimSpace(m.search.Query) == "" {
		m.search.MatchIndex = -1
		return m
	}
	matches := m.searchMatches(m.currentLayout())
	if len(matches) == 0 {
		m.search.MatchIndex = -1
		return m
	}
	if m.search.MatchIndex < 0 || m.search.MatchIndex >= len(matches) {
		m.search.MatchIndex = len(matches) - 1
	}
	return m.scrollToSearchMatch(matches[m.search.MatchIndex])
}

func (m Model) advanceSearchMatch(delta int) Model {
	matches := m.searchMatches(m.currentLayout())
	if len(matches) == 0 {
		m.search.MatchIndex = -1
		return m
	}
	if m.search.MatchIndex < 0 || m.search.MatchIndex >= len(matches) {
		m.search.MatchIndex = len(matches) - 1
	} else {
		m.search.MatchIndex = (m.search.MatchIndex + delta + len(matches)) % len(matches)
	}
	return m.scrollToSearchMatch(matches[m.search.MatchIndex])
}

func (m Model) scrollToSearchMatch(line int) Model {
	layout := m.currentLayout()
	lines := m.searchLines(layout)
	maxStart := len(lines) - layout.TimelineH
	if maxStart <= 0 {
		m.scroll = 0
		m.scrollPaused = false
		return m
	}
	start := line - 1
	if start < 0 {
		start = 0
	}
	if start > maxStart {
		start = maxStart
	}
	m.scroll = maxStart - start
	m.scrollPaused = m.scroll > 0
	return m
}

func (m Model) searchMatches(layout LayoutState) []int {
	query := strings.ToLower(strings.TrimSpace(m.search.Query))
	if query == "" {
		return nil
	}
	lines := m.searchLines(layout)
	matches := make([]int, 0)
	for i, line := range lines {
		if strings.Contains(strings.ToLower(ansi.Strip(line)), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

func (m Model) searchLines(layout LayoutState) []string {
	if m.focus == FocusAgentWindow {
		return m.agentWindowLines(layout.TimelineW)
	}
	if len(m.timeline.Turns) > 0 {
		return m.workspaceTimelineLines(layout.TimelineW)
	}
	return nil
}

func (m Model) searchFooter(width int) string {
	if m.reverseHistory.active {
		return m.reverseHistorySearchFooter(width)
	}
	query := m.search.Query
	if query == "" {
		query = "<type query>"
	}
	label := fmt.Sprintf("search  %s", query)
	if strings.TrimSpace(m.search.Query) != "" {
		matches := m.searchMatches(m.currentLayout())
		if len(matches) == 0 {
			label += " · 0/0"
		} else {
			index := m.search.MatchIndex
			if index < 0 || index >= len(matches) {
				index = 0
			}
			label += fmt.Sprintf(" · %d/%d", index+1, len(matches))
		}
	}
	label += " · Enter/F3 next · Shift+F3 prev · Esc close"
	return truncateStyled(label, width)
}

func (m Model) reverseHistorySearchFooter(width int) string {
	query := m.reverseHistory.query
	if query == "" {
		query = "(all)"
	}
	selected := "<no match>"
	if prompt, ok := m.selectedReverseHistoryPrompt(); ok {
		selected = strings.Join(strings.Fields(prompt), " ")
	}
	label := fmt.Sprintf("reverse  %s → %s · ↑↓/Ctrl+P/N · Enter accept · Esc cancel", query, selected)
	return truncateStyled(label, width)
}

// highlightSearchQuery wraps occurrences of query (case-insensitive) in a styled
// line so the user can see every match, not just the jumped-to one. It walks the
// ANSI-stripped text and re-emits each non-overlapping match wrapped in the
// highlight style, leaving the surrounding ANSI sequences intact. Returns the
// line unchanged when the query is empty or absent.
func highlightSearchQuery(line, query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return line
	}
	lowerQuery := strings.ToLower(query)
	plain := ansi.Strip(line)
	lowerPlain := strings.ToLower(plain)
	idx := strings.Index(lowerPlain, lowerQuery)
	if idx < 0 {
		return line
	}
	// Find the runic offset in the original (non-lowercased) plain text so
	// multibyte queries align correctly. plain and lowerPlain share byte length.
	var b strings.Builder
	prev := 0
	for idx >= 0 {
		end := idx + len(query)
		b.WriteString(plain[prev:idx])
		b.WriteString(searchHighlightStyle.Render(plain[idx:end]))
		prev = end
		next := strings.Index(lowerPlain[prev:], lowerQuery)
		if next < 0 {
			break
		}
		idx = prev + next
	}
	b.WriteString(plain[prev:])
	return b.String()
}

// applySearchHighlight runs the query highlighter over every visible line of the
// rendered timeline body. Called only while the search dock is open so idle
// frames pay no cost.
func applySearchHighlight(body, query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = highlightSearchQuery(line, query)
	}
	return strings.Join(lines, "\n")
}
