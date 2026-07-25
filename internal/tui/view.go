package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) View() string {
	start := time.Now()
	defer m.prof.frameSince(start)
	// The session-tree browser is a full-screen modal overlay.
	if m.history.visible && m.agent.Pending == nil && m.question == nil {
		view := m.renderHistoryView()
		if len(m.toasts) > 0 {
			view = blitTopRight(view, renderToasts(m.toasts, m.width), m.width)
		}
		return paintGrokNightSurface(view, m.width, m.height, m.height)
	}
	dock := m.measureBottomDock()
	layout := CalculateLayout(m.width, m.height, dock.reservedHeight())
	m = m.clampScroll(layout)
	dock = m.renderBottomDock(dock, layout)
	body := m.renderMainBody(layout)

	footer := m.renderFooter(layout)

	view := m.composeBaseView(body, dock, footer, layout)

	// Floating layers own the final defensive terminal-fit pass so ordinary
	// stream and spinner frames are not padded and scanned twice.
	view = m.composeFloatingLayers(view)
	// Paint GrokNight black surface last so idle welcome and in-session chrome
	// both fill #0a0a0a / #141414 instead of the host default background.
	bodyRows := renderedHeight(body)
	if bodyRows > m.height {
		bodyRows = m.height
	}
	return paintGrokNightSurface(view, m.width, m.height, bodyRows)
}

func (m Model) promptVisible() bool {
	return m.agent.Pending == nil && m.question == nil && !m.search.Active
}

// queuedView renders prompts parked while the agent is busy, pinned just above
// the composer so they are not lost as the timeline scrolls. Returns "" when
// nothing is queued.
func (m Model) queuedView() string {
	return m.queuedViewForWidth(m.width)
}

func (m Model) queuedViewForWidth(width int) string {
	if len(m.agent.QueuedPrompts) == 0 {
		return ""
	}
	label := func(i int) string {
		if len(m.agent.QueuedPrompts) == 1 {
			return "queued"
		}
		return fmt.Sprintf("queued #%d", i+1)
	}
	limit := width - 16
	if limit < 8 {
		limit = 8
	}
	lines := make([]string, 0, len(m.agent.QueuedPrompts))
	for i, p := range m.agent.QueuedPrompts {
		text := strings.ReplaceAll(strings.TrimSpace(p), "\n", " ")
		// Distinct from main's "Esc/Ctrl+C stop run + queue" legend.
		labelText := "\u23f3 " + label(i)
		if i == 0 {
			withHint := labelText + " \u00b7 enter sends next"
			if lipgloss.Width(withHint)+2+8 <= width {
				labelText = withHint
			}
		}
		renderedLabel := warningStyle.Render(labelText)
		limit = width - lipgloss.Width(renderedLabel) - 2
		if limit < 8 {
			limit = 8
		}
		lines = append(lines, fmt.Sprintf("%s  %s", renderedLabel, truncatePlain(text, limit)))
	}
	return strings.Join(lines, "\n")
}

func renderedHeight(value string) int {
	if value == "" {
		return 0
	}
	return lipgloss.Height(value)
}

func (m Model) composerViewForWidth(width int) string {
	if width < 4 {
		width = 4
	}
	// Grok prompt chrome: ❯ + text, soft top rule (prompt_border / active),
	// model · mode info line. No heavy rounded box.
	innerWidth := max(1, width)
	input := m.composer.Input
	fs := input.FocusedStyle
	fs.Prompt = lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted).Bold(true)
	fs.Text = lipgloss.NewStyle().Foreground(DefaultTheme.Text)
	fs.Placeholder = lipgloss.NewStyle().Foreground(DefaultTheme.Dim).Italic(true)
	hoverComposer := m.hover.kind == mouseHitComposer
	flashComposer := m.flash.active() && m.flash.kind == mouseHitComposer
	if hoverComposer || flashComposer {
		bg := DefaultTheme.Hover
		if flashComposer && animPulseOn(m.animFrame) {
			bg = DefaultTheme.Selection
		}
		fs.Text = fs.Text.Background(bg)
		fs.Placeholder = fs.Placeholder.Background(bg)
		fs.Prompt = fs.Prompt.Background(bg)
	}
	input.FocusedStyle = fs
	input.SetWidth(innerWidth)
	body := input.View()
	// Attachment chips: dim row above the input listing @path mentions / pastes.
	if chips := m.promptAttachmentsLine(); chips != "" {
		body = truncateStyled(chips, innerWidth) + "\n" + body
	}
	if info := m.promptInfoLine(); info != "" {
		body = body + "\n" + truncateStyled(dimStyle.Render("  "+info), width)
	}
	// Soft rule above the prompt (Grok prompt_border / prompt_border_active).
	// Focused = brighter active border; blur/scrollback = dim idle rule.
	ruleColor := DefaultTheme.PromptBorder
	focusedPrompt := m.focus == FocusComposer || hoverComposer
	if focusedPrompt {
		ruleColor = DefaultTheme.PromptActive
	}
	ruleStyle := lipgloss.NewStyle().Foreground(ruleColor)
	if focusedPrompt {
		ruleStyle = ruleStyle.Bold(true)
	}
	rule := ruleStyle.Render(strings.Repeat("\u2500", max(1, width)))
	return rule + "\n" + body
}

// promptInfoLine is the Grok-like model · mode · context · todo · permission row under the prompt.
func (m Model) promptInfoLine() string {
	parts := make([]string, 0, 5)
	if model := strings.TrimSpace(m.cfg.Status.Model); model != "" {
		parts = append(parts, model)
	}
	parts = append(parts, m.mode.Label())
	if ctx := m.contextLabel(); ctx != "" {
		parts = append(parts, ctx)
	}
	if chip := todoChip(m.live.Tasks); chip != "" {
		parts = append(parts, chip)
	}
	if perm := strings.TrimSpace(m.cfg.Status.PermissionMode); perm != "" {
		parts = append(parts, perm)
	}
	return strings.Join(parts, " · ")
}

// composerHeight is the rendered prompt height including the soft top rule,
// optional chips, and the model/mode info line.
func (m Model) composerHeight() int {
	h := m.composer.Input.Height() + 1 // top rule
	if m.promptAttachmentsLine() != "" {
		h++
	}
	if m.promptInfoLine() != "" {
		h++
	}
	return max(1, h)
}

// promptAttachmentsLine renders a chip row for every @path mention in the
// composer draft. Empty when the draft has no mentions.
func (m Model) promptAttachmentsLine() string {
	mentions := extractFileMentions(m.inputValue())
	pastes := m.composer.Pastes
	if len(mentions) == 0 && len(pastes) == 0 {
		return ""
	}
	chipStyle := lipgloss.NewStyle().
		Background(DefaultTheme.Selection).
		Foreground(DefaultTheme.Text)
	var parts []string
	for _, p := range mentions {
		parts = append(parts, chipStyle.Render(" "+truncatePlain(p, 32)+" "))
	}
	for _, p := range pastes {
		parts = append(parts, chipStyle.Render(" "+p.Marker+" "))
	}
	return dimStyle.Render("📎") + " " + strings.Join(parts, " ")
}

func (m Model) renderFooter(layout LayoutState) string {
	width := layout.TimelineW
	// Permission / ask-user takeovers own the dock; their shortcuts win over
	// search chrome. Busy guidance lives on turn-status, not a second
	// "running · Esc…" legend that fights the shortcuts bar.
	if m.agent.Pending != nil || m.question != nil {
		if line := m.shortcutsBarLine(width); line != "" {
			return line
		}
		return ""
	}
	if m.search.Active {
		return truncateStyled(dimStyle.Render(m.searchFooter(width)), width)
	}
	// Scroll position is not advertised in the footer: the old "↑ N lines /
	// ↓ N new" chrome stuck around while flipping through history and the
	// counters were easy to misread. Shortcuts (or runtime identity) stay put.
	if line := m.shortcutsBarLine(width); line != "" {
		return line
	}
	return truncateStyled(dimStyle.Render(m.runtimeFooter()), width)
}

// runtimeFooter is a fallback identity row when shortcuts have nothing to show.
func (m Model) runtimeFooter() string {
	parts := []string{m.mode.Label()}
	if model := strings.TrimSpace(m.cfg.Status.Model); model != "" {
		parts = append(parts, model)
	}
	if context := m.contextLabel(); context != "" {
		parts = append(parts, context)
	}
	return strings.Join(parts, " · ")
}

// transientFooter is retained for tests that probe short-lived control copy.
// Production footer prefers shortcutsBarLine (see renderFooter). Scroll-position
// chrome was removed; do not reintroduce "↑ N lines" here.
func (m Model) transientFooter(width int) string {
	if m.agent.Pending != nil || m.question != nil {
		return ""
	}
	switch {
	case m.search.Active:
		return m.searchFooter(width)
	case m.agent.Busy && len(m.agent.QueuedPrompts) > 0:
		return fmt.Sprintf("running · queued %d", len(m.agent.QueuedPrompts))
	case m.agent.Busy:
		return "running · Esc/Ctrl+C stop · Enter queue"
	default:
		return ""
	}
}

func truncateStyled(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= limit {
		return value
	}
	return ansi.Truncate(value, limit, "...")
}

func (m Model) renderHeader() string {
	status := m.cfg.Status
	width := max(1, m.width)
	title := accentStyle.Render("◆ BondCode")
	segments := []string{
		shortPath(defaultString(status.ProjectRoot, ".")),
		defaultString(status.Model, "model"),
	}
	if branch := strings.TrimSpace(status.GitBranch); branch != "" {
		segments = append(segments, "⎇ "+branch)
	}
	segments = append(segments, defaultString(status.PermissionMode, "confirm"))
	left := title + "  " + dimStyle.Render(strings.Join(segments, " · "))
	if m.mode.IsPlan() {
		left += "  " + warningStyle.Render("⬡ plan")
	}

	content := left
	if ctx := m.contextColored(); ctx != "" {
		ctxWidth := lipgloss.Width(ctx)
		if ctxWidth+2 < width {
			left = truncateStyled(left, width-ctxWidth-2)
			gap := width - lipgloss.Width(left) - ctxWidth
			if gap < 1 {
				gap = 1
			}
			content = left + strings.Repeat(" ", gap) + ctx
		}
	}

	headerLine := fitStyledLine(content, width)
	sep := dimStyle.Render(strings.Repeat("─", width))
	return headerLine + "\n" + sep
}

// contextLabel renders the live context-window fill ratio reported by the
// agent loop via EventContextUpdated. It returns "" until the first report
// lands, so the header omits the context segment at idle rather than showing
// a stale static max ("max 100000 tokens").
// contextUsed is the token count backing the header's ctx %: the model's real
// measured input tokens when available, otherwise the governor estimate. Using
// measured tokens keeps the header consistent with /status.
func (m Model) contextUsed() int {
	if m.agent.MeasuredTokens > 0 {
		return m.agent.MeasuredTokens
	}
	return m.agent.ContextTokens
}

// formatTokens renders a token count compactly (5.2k / 1.0M) so the header's ctx
// segment stays readable on large windows where a raw integer percentage would
// sit at 0% for a long time and look empty.
func formatTokens(n int) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// compactionDividerBody formats the before→after token counts shown on a
// compaction divider block. A non-positive after means compaction did not
// produce a usable size (e.g. the failure branch in compact.go), so "" is
// returned and no divider block is appended. before<=0 (no prior measurement)
// collapses to a single after count.
func compactionDividerBody(before, after int) string {
	if after <= 0 {
		return ""
	}
	if before > 0 {
		return fmt.Sprintf("%s → %s tokens", formatTokens(before), formatTokens(after))
	}
	return formatTokens(after) + " tokens"
}

func (m Model) contextLabel() string {
	used := m.contextUsed()
	if used <= 0 || m.agent.ContextMaxTokens <= 0 {
		return ""
	}
	pct := used * 100 / m.agent.ContextMaxTokens
	return fmt.Sprintf("ctx %s/%s %d%%", formatTokens(used), formatTokens(m.agent.ContextMaxTokens), pct)
}

// contextColored renders the context fill ratio as a percentage plus a small
// bar, colored by threshold (<60% green / <85% amber / >=85% red).
func (m Model) contextColored() string {
	used := m.contextUsed()
	if used <= 0 || m.agent.ContextMaxTokens <= 0 {
		return ""
	}
	pct := used * 100 / m.agent.ContextMaxTokens
	style := successStyle
	switch {
	case pct >= 85:
		style = errorStyle
	case pct >= 60:
		style = warningStyle
	}
	return style.Render(fmt.Sprintf("ctx %s/%s %d%% %s", formatTokens(used), formatTokens(m.agent.ContextMaxTokens), pct, renderContextBar(pct, 8)))
}

func renderContextBar(pct, width int) string {
	if width < 1 {
		width = 1
	}
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
