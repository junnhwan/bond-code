package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The overlay system is the TUI's unified modal layer. It hosts short-lived,
// focused interactions — the command palette, list menus (message/session
// actions), alert/confirm/prompt dialogs — behind a single dispatch point in
// Model.Update and a single render hook in Model.View.
//
// Design notes:
//   - At most one overlay is active at a time. Palette→confirm style
//     transitions REPLACE the active overlay rather than stack, which keeps
//     key dispatch single-target and avoids deep nesting.
//   - The existing agent-driven panels (safety confirmation in confirm.go, the
//     ask-user questioner, and the ctrl+h history browser) are intentionally
//     NOT migrated here: they carry agent-loop response contracts
//     (Confirmer.Respond, fork-resume) that do not fit the generic action model.
//   - Menu/confirm/prompt items carry Run closures so each call site builds its
//     own action list; the overlay machinery stays context-agnostic.

// overlayKind identifies the active modal. overlayNone means the base view owns
// the screen.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayPalette
	overlayMenu
	overlayAlert
	overlayConfirm
	overlayPrompt
	overlayDiff
	overlaySessions
)

// overlayState is the tagged union holding whichever modal is active. Only the
// sub-state matching `kind` is meaningful.
type overlayState struct {
	kind     overlayKind
	palette  paletteOverlay
	menu     menuOverlay
	alert    alertOverlay
	confirm  confirmOverlay
	prompt   promptOverlay
	diff     diffViewerState
	sessions sessionManagerState
}

func (o overlayState) active() bool { return o.kind != overlayNone }

// closeOverlay clears the active layer.
func (m Model) closeOverlay() Model {
	m.overlay = overlayState{}
	return m
}

// --- palette (see palette.go for logic) ---

type paletteOverlay struct {
	actions  []Action
	filtered []Action
	query    string
	selected int
}

// --- menu ---

// menuOverlay is a titled list. Each item carries a Run closure so the same
// machinery drives message-action menus, session-action menus, etc.
type menuOverlay struct {
	title    string
	subtitle string
	items    []menuItem
	selected int
}

type menuItem struct {
	label    string
	shortcut string
	disabled bool
	hint     string
	run      func(m Model) (Model, tea.Cmd)
}

// --- alert ---

type alertOverlay struct {
	title   string
	message string
	variant toastVariant
}

// --- confirm ---

// confirmOverlay is a Yes/No question. onConfirm fires with the user's choice
// and owns the resulting transition (delete session, retry turn, …).
type confirmOverlay struct {
	title     string
	message   string
	approve   bool
	highRisk  bool
	onConfirm func(m Model, approved bool) (Model, tea.Cmd)
}

// --- prompt ---

// promptOverlay is a single-line text input. onSubmit receives the trimmed text.
type promptOverlay struct {
	title    string
	prompt   string
	value    string
	onSubmit func(m Model, text string) (Model, tea.Cmd)
}

// openMenu pushes a titled list menu overlay.
func (m Model) openMenu(title, subtitle string, items []menuItem) Model {
	m.overlay = overlayState{
		kind: overlayMenu,
		menu: menuOverlay{
			title:    title,
			subtitle: subtitle,
			items:    items,
			selected: firstEnabledItem(items),
		},
	}
	return m
}

// openAlert pushes an informational modal the user dismisses with Enter/Esc.
func (m Model) openAlert(title, message string, variant toastVariant) Model {
	m.overlay = overlayState{
		kind:  overlayAlert,
		alert: alertOverlay{title: title, message: message, variant: variant},
	}
	return m
}

// openConfirm pushes a Yes/No modal. When highRisk is true the user must select
// Yes explicitly then press Enter (bare Enter on No is a no-op), mirroring the
// safety confirmation panel's rule for high-risk tool calls.
func (m Model) openConfirm(title, message string, highRisk bool, onConfirm func(Model, bool) (Model, tea.Cmd)) Model {
	m.overlay = overlayState{
		kind: overlayConfirm,
		confirm: confirmOverlay{
			title:     title,
			message:   message,
			approve:   false,
			highRisk:  highRisk,
			onConfirm: onConfirm,
		},
	}
	return m
}

// openPrompt pushes a single-line text input modal.
func (m Model) openPrompt(title, prompt, initial string, onSubmit func(Model, string) (Model, tea.Cmd)) Model {
	m.overlay = overlayState{
		kind: overlayPrompt,
		prompt: promptOverlay{
			title:    title,
			prompt:   prompt,
			value:    initial,
			onSubmit: onSubmit,
		},
	}
	return m
}

// handleOverlayKey dispatches a key to the active overlay. Returns handled=true
// when the overlay swallows the key so it must not reach the composer or the
// main key switch. A key that closes the overlay still returns handled=true so
// it does not fall through (e.g. Esc closes AND is consumed).
func (m Model) handleOverlayKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if !m.overlay.active() {
		return m, nil, false
	}
	switch m.overlay.kind {
	case overlayPalette:
		return m.handlePaletteKey(msg)
	case overlayMenu:
		return m.handleMenuKey(msg)
	case overlayAlert:
		return m.handleAlertKey(msg)
	case overlayConfirm:
		return m.handleConfirmOverlayKey(msg)
	case overlayPrompt:
		return m.handlePromptOverlayKey(msg)
	case overlayDiff:
		return m.handleDiffViewerKey(msg)
	case overlaySessions:
		return m.handleSessionManagerKey(msg)
	}
	return m, nil, false
}

// --- menu key handling ---

func (m Model) handleMenuKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	menu := m.overlay.menu
	switch msg.String() {
	case "esc", "ctrl+c", "q", "ctrl+[":
		return m.closeOverlay(), nil, true
	case "up", "k":
		menu.selected = moveMenuCursor(menu.items, menu.selected, -1)
		m.overlay.menu = menu
		return m, nil, true
	case "down", "j":
		menu.selected = moveMenuCursor(menu.items, menu.selected, +1)
		m.overlay.menu = menu
		return m, nil, true
	case "home", "g":
		menu.selected = firstEnabledItem(menu.items)
		m.overlay.menu = menu
		return m, nil, true
	case "end", "G":
		menu.selected = lastEnabledItem(menu.items)
		m.overlay.menu = menu
		return m, nil, true
	case "pgup":
		menu.selected = moveMenuCursor(menu.items, menu.selected, -menuPageSize())
		m.overlay.menu = menu
		return m, nil, true
	case "pgdn":
		menu.selected = moveMenuCursor(menu.items, menu.selected, menuPageSize())
		m.overlay.menu = menu
		return m, nil, true
	case "enter":
		if menu.selected >= 0 && menu.selected < len(menu.items) {
			item := menu.items[menu.selected]
			if item.disabled || item.run == nil {
				return m, nil, true
			}
			// Close first so Run sees a clean base state and may open a
			// follow-up overlay (e.g. a confirm) if it needs to.
			closed := m.closeOverlay()
			next, cmd := item.run(closed)
			return next, cmd, true
		}
		return m, nil, true
	}
	return m, nil, true
}

func moveMenuCursor(items []menuItem, cur, delta int) int {
	if len(items) == 0 {
		return 0
	}
	for i := 0; i < len(items); i++ {
		cur += delta
		if cur < 0 {
			cur = len(items) - 1
		}
		if cur >= len(items) {
			cur = 0
		}
		if !items[cur].disabled {
			return cur
		}
	}
	return cur
}

func firstEnabledItem(items []menuItem) int {
	for i, it := range items {
		if !it.disabled {
			return i
		}
	}
	return 0
}

func lastEnabledItem(items []menuItem) int {
	for i := len(items) - 1; i >= 0; i-- {
		if !items[i].disabled {
			return i
		}
	}
	return 0
}

func menuPageSize() int { return 6 }

// --- alert key handling ---

func (m Model) handleAlertKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "enter", "esc", " ", "ctrl+c", "q":
		return m.closeOverlay(), nil, true
	}
	return m, nil, true
}

// --- confirm key handling ---

func (m Model) handleConfirmOverlayKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	c := m.overlay.confirm
	switch msg.String() {
	case "esc", "n", "ctrl+c", "q":
		return m.commitConfirm(c, false)
	case "left", "h":
		// Visual order is "Yes    No" (Yes left, No right). Left moves toward
		// Yes; at the leftmost choice it wraps to No so selection always cycles.
		c.approve = moveConfirmChoice(c.approve, -1)
		m.overlay.confirm = c
		return m, nil, true
	case "right", "l":
		// Right moves toward No; wraps Yes ← No at the right end.
		c.approve = moveConfirmChoice(c.approve, +1)
		m.overlay.confirm = c
		return m, nil, true
	case "tab":
		c.approve = !c.approve
		m.overlay.confirm = c
		return m, nil, true
	case "y":
		if !c.highRisk {
			return m.commitConfirm(c, true)
		}
		// high-risk: 'y' only arms the Yes selection; Enter confirms.
		c.approve = true
		m.overlay.confirm = c
		return m, nil, true
	case "enter":
		if c.highRisk && !c.approve {
			return m, nil, true
		}
		return m.commitConfirm(c, c.approve)
	}
	return m, nil, true
}

// moveConfirmChoice steps the Yes/No cursor. approve=true is Yes (index 0 /
// left); false is No (index 1 / right). delta is -1 (left) or +1 (right) and
// wraps so left/right always move the highlight.
func moveConfirmChoice(approve bool, delta int) bool {
	// Order matches renderConfirmBox: [Yes, No].
	idx := 1
	if approve {
		idx = 0
	}
	idx = (idx + delta) % 2
	if idx < 0 {
		idx += 2
	}
	return idx == 0
}

// commitConfirm closes the overlay and fires the onConfirm callback with the
// user's choice. The close happens before the callback so it sees a clean base.
func (m Model) commitConfirm(c confirmOverlay, approved bool) (Model, tea.Cmd, bool) {
	closed := m.closeOverlay()
	if c.onConfirm != nil {
		next, cmd := c.onConfirm(closed, approved)
		return next, cmd, true
	}
	return closed, nil, true
}

// --- prompt key handling ---

func (m Model) handlePromptOverlayKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	p := m.overlay.prompt
	switch msg.String() {
	case "esc", "ctrl+c":
		return m.closeOverlay(), nil, true
	case "enter":
		text := strings.TrimSpace(p.value)
		closed := m.closeOverlay()
		if p.onSubmit != nil {
			next, cmd := p.onSubmit(closed, text)
			return next, cmd, true
		}
		return closed, nil, true
	case "backspace", "ctrl+h":
		if len(p.value) > 0 {
			p.value = trimLastByte(p.value)
		}
		m.overlay.prompt = p
		return m, nil, true
	case "ctrl+u", "ctrl+w":
		p.value = ""
		m.overlay.prompt = p
		return m, nil, true
	case "ctrl+a":
		// no-op: single-line, cursor stays at end
		return m, nil, true
	default:
		if msg.Type == tea.KeyRunes {
			// Drop non-printable control runes (e.g. Ctrl+X → 0x18 on some
			// terminals) so they don't silently pollute the prompt value.
			if added := printableRunes(msg); added != "" {
				p.value += added
			}
			m.overlay.prompt = p
			return m, nil, true
		}
	}
	return m, nil, true
}

// printableRunes returns only the printable runes from a key event. Terminal
// control combos can arrive as a KeyRunes carrying a control rune (e.g. 0x10
// for Ctrl+P on some Windows/ConPTY setups); without filtering, those invisible
// bytes get spliced into the palette query / prompt value and silently break
// matching — the box looks empty yet shows "no matching actions". We keep every
// rune unicode.IsPrint says occupies a cell, which includes CJK ideographs and
// punctuation, so normal typing is unaffected.
func printableRunes(msg tea.KeyMsg) string {
	var b strings.Builder
	for _, r := range msg.Runes {
		if unicode.IsPrint(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trimLastByte(s string) string {
	// Trim a single UTF-8 rune off the end, not just a byte.
	if s == "" {
		return s
	}
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[:len(r)-1])
}

// --- overlay rendering ---

// renderOverlay renders the active modal as a full screen: a centered box on a
// neutral backdrop. The base view is not composited behind it (matching the
// ctrl+h history browser) — modals are intentionally focused, short-lived
// surfaces; the conversation reappears the moment they close.
func (m Model) renderOverlay() string {
	if !m.overlay.active() {
		return ""
	}
	// The diff viewer is a full-screen overlay (like the ctrl+h history
	// browser): it owns the whole viewport, so it skips the centered-box path.
	if m.overlay.kind == overlayDiff {
		return m.renderDiffViewer()
	}
	// The session manager is the same: a full-screen list, not a centered box.
	if m.overlay.kind == overlaySessions {
		return m.renderSessionManager()
	}
	var box string
	switch m.overlay.kind {
	case overlayPalette:
		box = m.renderPaletteBox()
	case overlayMenu:
		box = m.renderMenuBox()
	case overlayAlert:
		box = m.renderAlertBox()
	case overlayConfirm:
		box = m.renderConfirmBox()
	case overlayPrompt:
		box = m.renderPromptBox()
	}
	if strings.TrimSpace(box) == "" {
		return ""
	}
	return centerBoxOverViewport(box, m.width, m.height)
}

// centerBoxOverViewport centers box (possibly multi-line) in a width×height
// field filled with the panel background, giving the modal a subtle backdrop
// that reads as "the base view is inactive".
func centerBoxOverViewport(box string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceBackground(DefaultTheme.BackgroundPanel),
	)
}

// overlayBoxStyle is the shared frame for every modal: a rounded border on the
// panel background with horizontal padding so content breathes.
func overlayBoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(DefaultTheme.Border).
		Background(DefaultTheme.BackgroundPanel).
		Padding(0, 1)
}

func overlayTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(DefaultTheme.Accent).Bold(true)
}

// --- menu / alert / confirm / prompt boxes ---

func (m Model) renderMenuBox() string {
	menu := m.overlay.menu
	w := menuBoxWidth(m.width)
	bodyW := w - 4 // border + padding
	var lines []string
	if menu.title != "" {
		lines = append(lines, overlayTitleStyle().Render(truncatePlain(menu.title, bodyW)))
	}
	if menu.subtitle != "" {
		lines = append(lines, dimStyle.Render(truncatePlain(menu.subtitle, bodyW)))
	}
	if menu.title != "" || menu.subtitle != "" {
		lines = append(lines, dimStyle.Render(strings.Repeat("─", clampInt(bodyW, 1, 60))))
	}

	// Reserve the frame and footer before choosing list rows. Rendering only a
	// window of items keeps the active row inside the box before it is centered,
	// rather than relying on the final terminal clip (which can hide selection).
	listH := overlayListHeight(m.height, len(lines)+2)
	if len(menu.items) == 0 {
		lines = append(lines, dimStyle.Render("(no actions)"))
	} else {
		start, end := paletteWindow(menu.selected, len(menu.items), listH)
		for i := start; i < end; i++ {
			lines = append(lines, renderMenuItemLine(menu.items[i], i == menu.selected, bodyW))
		}
	}
	lines = append(lines, "", dimStyle.Render("↑/↓ move · enter run · esc close"))
	content := strings.Join(lines, "\n")
	return overlayBoxStyle().Width(w).Render(content)
}

// overlayListHeight returns the rows available to a selectable list after the
// rounded border and non-list content have been reserved.
func overlayListHeight(viewportH, fixedContentH int) int {
	const borderH = 2
	return max(1, viewportH-borderH-fixedContentH)
}

func renderMenuItemLine(item menuItem, selected bool, width int) string {
	glyph := "  "
	marker := " "
	if selected {
		glyph = "❯ "
		marker = "❯"
	}
	label := truncatePlain(item.label, max(8, width-12))
	line := glyph + label
	if item.disabled {
		line = dimStyle.Render(line)
		if item.hint != "" {
			line += " " + dimStyle.Render("("+item.hint+")")
		}
		return line
	}
	if item.shortcut != "" {
		gap := width - lipgloss.Width(line) - len(item.shortcut) - 2
		if gap < 1 {
			gap = 1
		}
		line += strings.Repeat(" ", gap) + dimStyle.Render(item.shortcut)
	}
	if selected {
		_ = marker
		return lipgloss.NewStyle().
			Foreground(DefaultTheme.Text).
			Background(DefaultTheme.Selection).
			Bold(true).
			Render(line)
	}
	return line
}

func menuBoxWidth(viewportW int) int {
	w := 56
	if viewportW-6 < w {
		w = viewportW - 6
	}
	if w < 32 {
		w = 32
	}
	return w
}

func (m Model) renderAlertBox() string {
	a := m.overlay.alert
	w := alertBoxWidth(m.width, a.message)
	bodyW := w - 4
	var lines []string
	title := a.title
	if title == "" {
		switch a.variant {
		case toastError:
			title = "Error"
		case toastWarn:
			title = "Warning"
		case toastSuccess:
			title = "Success"
		default:
			title = "Notice"
		}
	}
	lines = append(lines, overlayTitleStyle().Render(truncatePlain(title, bodyW)))
	lines = append(lines, wrapPlain(a.message, bodyW)...)
	lines = append(lines, "", dimStyle.Render("enter / esc to close"))
	content := strings.Join(lines, "\n")
	style := overlayBoxStyle().Width(w)
	switch a.variant {
	case toastError:
		style = style.BorderForeground(DefaultTheme.Error)
	case toastWarn:
		style = style.BorderForeground(DefaultTheme.Warning)
	case toastSuccess:
		style = style.BorderForeground(DefaultTheme.Success)
	}
	return style.Render(content)
}

func alertBoxWidth(viewportW int, message string) int {
	w := 0
	for _, line := range strings.Split(message, "\n") {
		if len(line) > w {
			w = len(line)
		}
	}
	w += 6 // border + padding + breathing room
	if w > viewportW-4 {
		w = viewportW - 4
	}
	if w < 34 {
		w = 34
	}
	return w
}

func (m Model) renderConfirmBox() string {
	c := m.overlay.confirm
	w := menuBoxWidth(m.width)
	bodyW := w - 4
	var lines []string
	if c.title != "" {
		lines = append(lines, overlayTitleStyle().Render(truncatePlain(c.title, bodyW)))
	}

	// Keep the interaction rows inside the box before centering. Only the
	// variable-height message is shortened; the blank/choice row, key hint, and
	// rounded border are always reserved so the active decision stays visible.
	const controlRows = 3 // blank + choices + hint
	messageH := overlayListHeight(m.height, len(lines)+controlRows)
	message := strings.Join(wrapPlain(c.message, bodyW), "\n")
	message = fitRenderedBlockHeight(message, messageH, bodyW)
	lines = append(lines, strings.Split(message, "\n")...)

	yes := "  Yes"
	no := "  No"
	if c.approve {
		yes = confirmStyle.Render("❯ Yes")
	} else {
		no = confirmStyle.Render("❯ No")
	}
	lines = append(lines, "", yes+"    "+no)
	hint := "← → select · enter confirm · esc cancel"
	if c.highRisk {
		hint = "high risk: select Yes + enter · esc cancel"
	}
	lines = append(lines, dimStyle.Render(hint))
	content := strings.Join(lines, "\n")
	style := overlayBoxStyle().Width(w)
	if c.highRisk {
		style = style.BorderForeground(DefaultTheme.Warning)
	}
	return style.Render(content)
}

func (m Model) renderPromptBox() string {
	p := m.overlay.prompt
	w := menuBoxWidth(m.width)
	bodyW := w - 4
	var lines []string
	if p.title != "" {
		lines = append(lines, overlayTitleStyle().Render(truncatePlain(p.title, bodyW)))
	}
	if p.prompt != "" {
		lines = append(lines, dimStyle.Render(truncatePlain(p.prompt, bodyW)))
	}
	value := p.value
	if value == "" {
		value = dimStyle.Render("…")
	}
	lines = append(lines, accentStyle.Render("> ")+value+"_")
	lines = append(lines, "", dimStyle.Render("enter submit · esc cancel"))
	content := strings.Join(lines, "\n")
	return overlayBoxStyle().Width(w).Render(content)
}

// wrapPlain hard-wraps s to width cells for plain (unstyled) text. Used by the
// alert/confirm boxes whose messages come from arbitrary strings.
func wrapPlain(s string, width int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{""}
	}
	if width < 8 {
		width = 8
	}
	var out []string
	for _, paragraph := range strings.Split(s, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if len(line)+1+len(word) > width {
				out = append(out, line)
				line = word
			} else {
				line += " " + word
			}
		}
		out = append(out, line)
	}
	return out
}
