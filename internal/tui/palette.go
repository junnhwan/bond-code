package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The command palette is the primary discovery surface for the TUI. Opened with
// Ctrl+P, it fuzzy-searches the full Action list (slash commands, toggles,
// navigation, session actions) and runs the selection. It is the first user of
// the overlay system + the action registry together, and validates both.

// openPalette builds the action list from the current state and enters the
// palette overlay. The list is rebuilt on each open so newly registered custom
// commands or changed key shortcuts are reflected without a restart.
func (m Model) openPalette() Model {
	actions := buildActionList(m)
	p := paletteOverlay{
		actions:  actions,
		filtered: make([]Action, len(actions)),
		selected: 0,
	}
	copy(p.filtered, actions)
	m.overlay = overlayState{kind: overlayPalette, palette: p}
	return m
}

// handlePaletteKey drives the palette: typing refines, arrows move, enter runs.
// Every key is consumed (handled=true) so the underlying composer never sees it.
func (m Model) handlePaletteKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	p := m.overlay.palette
	switch msg.String() {
	case "esc", "ctrl+c", "ctrl+[":
		return m.closeOverlay(), nil, true
	case "up", "ctrl+p":
		if len(p.filtered) > 0 {
			p.selected--
			if p.selected < 0 {
				p.selected = len(p.filtered) - 1
			}
		}
		m.overlay.palette = p
		return m, nil, true
	case "down", "ctrl+n":
		if len(p.filtered) > 0 {
			p.selected++
			if p.selected >= len(p.filtered) {
				p.selected = 0
			}
		}
		m.overlay.palette = p
		return m, nil, true
	case "home", "ctrl+g":
		p.selected = 0
		m.overlay.palette = p
		return m, nil, true
	case "end":
		p.selected = max(0, len(p.filtered)-1)
		m.overlay.palette = p
		return m, nil, true
	case "pgup":
		p.selected = max(0, p.selected-palettePageSize())
		m.overlay.palette = p
		return m, nil, true
	case "pgdn":
		p.selected = min(len(p.filtered)-1, p.selected+palettePageSize())
		if p.selected < 0 {
			p.selected = 0
		}
		m.overlay.palette = p
		return m, nil, true
	case "backspace", "ctrl+h":
		if len(p.query) > 0 {
			p.query = trimLastByte(p.query)
			p.refine()
		}
		m.overlay.palette = p
		return m, nil, true
	case "ctrl+u", "ctrl+w":
		p.query = ""
		p.refine()
		m.overlay.palette = p
		return m, nil, true
	case "enter":
		if p.selected >= 0 && p.selected < len(p.filtered) {
			chosen := p.filtered[p.selected]
			if chosen.Run == nil {
				return m.closeOverlay(), nil, true
			}
			closed := m.closeOverlay()
			next, cmd := chosen.Run(closed)
			return next, cmd, true
		}
		return m.closeOverlay(), nil, true
	default:
		if msg.Type == tea.KeyRunes {
			// Drop non-printable control runes (e.g. a bare Ctrl arriving as
			// 0x10 / 0x18 on some Windows terminals) so they don't get spliced
			// into the query and silently zero out the match list.
			if added := printableRunes(msg); added != "" {
				p.query += added
				p.refine()
			}
			m.overlay.palette = p
			return m, nil, true
		}
	}
	return m, nil, true
}

// refine recomputes filtered from actions using the current query, reusing the
// shared fuzzyScore helper. The match prefers the title but falls back to the
// ID so "/status" or "verbose" both find their actions.
func (p *paletteOverlay) refine() {
	p.filtered = filterActions(p.actions, p.query)
	if p.selected >= len(p.filtered) {
		p.selected = max(0, len(p.filtered)-1)
	}
}

func filterActions(actions []Action, query string) []Action {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		out := make([]Action, len(actions))
		copy(out, actions)
		return out
	}
	type scored struct {
		a     Action
		score int
	}
	matches := make([]scored, 0, len(actions))
	for _, a := range actions {
		sc := fuzzyScore(a.Title, query)
		if sc < 0 {
			sc = fuzzyScore(a.ID, query)
		}
		if sc < 0 {
			sc = fuzzyScore(strings.ToLower(a.Category), query)
		}
		if sc < 0 {
			continue
		}
		// Small bonus so an exact shortcut match (e.g. "/status") ranks above
		// a generic title substring hit.
		if strings.Contains(strings.ToLower(a.Shortcut), query) {
			sc += 5
		}
		matches = append(matches, scored{a, sc})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		if matches[i].a.Category != matches[j].a.Category {
			return matches[i].a.Category < matches[j].a.Category
		}
		return matches[i].a.Title < matches[j].a.Title
	})
	out := make([]Action, len(matches))
	for i, m := range matches {
		out[i] = m.a
	}
	return out
}

// renderPaletteBox renders the palette modal: a title, the query line, a
// scrollable filtered list grouped faintly by category, and a keymap footer.
func (m Model) renderPaletteBox() string {
	p := m.overlay.palette
	w := paletteWidth(m.width)
	bodyW := w - 4 // border + padding
	listH := paletteListHeight(m.height)

	var lines []string
	lines = append(lines, overlayTitleStyle().Render("Command Palette"))
	queryDisplay := p.query
	if queryDisplay == "" {
		queryDisplay = dimStyle.Render("type to search actions…")
	} else {
		queryDisplay = accentStyle.Render(queryDisplay)
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(DefaultTheme.Text).Render("❯ ")+queryDisplay)
	lines = append(lines, dimStyle.Render(strings.Repeat("─", clampInt(bodyW, 1, 60))))

	if len(p.filtered) == 0 {
		lines = append(lines, dimStyle.Render("no matching actions"))
	} else {
		itemH := listH
		showMore := len(p.filtered) > itemH
		start, end := paletteWindow(p.selected, len(p.filtered), itemH)
		extraRows := paletteExtraRows(m.height, listH)
		separatorRows := extraRows
		if showMore && separatorRows > 0 {
			separatorRows--
		}
		prevCat := ""
		for i := start; i < end; i++ {
			a := p.filtered[i]
			if a.Category != prevCat && prevCat != "" && separatorRows > 0 {
				lines = append(lines, "")
				separatorRows--
			}
			prevCat = a.Category
			lines = append(lines, renderPaletteLine(a, i == p.selected, bodyW))
		}
		if showMore && extraRows > 0 {
			lines = append(lines, dimStyle.Render("  … more matches above/below"))
		}
	}
	lines = append(lines, "", dimStyle.Render("type to filter · ↑↓ move · enter run · esc close"))
	content := strings.Join(lines, "\n")
	return overlayBoxStyle().Width(w).Render(content)
}

func renderPaletteLine(a Action, selected bool, width int) string {
	glyph := "  "
	if selected {
		glyph = "❯ "
	}
	title := truncatePlain(a.Title, max(8, width-18))
	line := glyph + title
	cat := dimStyle.Render("[" + a.Category + "]")
	sc := ""
	if a.Shortcut != "" {
		sc = dimStyle.Render(a.Shortcut)
	}
	tail := strings.TrimSpace(cat + "  " + sc)
	if tail != "" {
		gap := width - lipgloss.Width(line) - lipgloss.Width(tail) - 1
		if gap < 1 {
			gap = 1
		}
		line += strings.Repeat(" ", gap) + tail
	}
	if selected {
		return lipgloss.NewStyle().
			Foreground(DefaultTheme.Text).
			Background(DefaultTheme.Selection).
			Bold(true).
			Render(line)
	}
	return line
}

// paletteWindow returns the [start, end) index range of items to render so the
// cursor stays visible. It mirrors the "window around marker" idea used by the
// history browser.
func paletteWindow(selected, total, maxVisible int) (int, int) {
	if total <= maxVisible {
		return 0, total
	}
	half := maxVisible / 2
	start := selected - half
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > total {
		end = total
		start = end - maxVisible
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

func paletteWidth(viewportW int) int {
	w := 68
	if viewportW-6 < w {
		w = viewportW - 6
	}
	if w < 34 {
		w = 34
	}
	return w
}

func paletteListHeight(viewportH int) int {
	h := viewportH - 9 // title + query + rule + footer + padding
	if h < 1 {
		h = 1
	}
	if h > 16 {
		h = 16
	}
	return h
}

func paletteExtraRows(viewportH, listH int) int {
	const fixedRows = 7 // border + title + query + rule + blank + footer
	return max(0, viewportH-fixedRows-listH)
}

func palettePageSize() int { return 8 }
