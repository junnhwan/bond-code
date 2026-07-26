package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderTimelineBlockLines(block Block, width int) []string {
	if block.Kind == BlockReasoning {
		return m.renderReasoningBlockLines(block, width)
	}
	// Manual fold (scrollback left/right): keep a one-line summary only.
	if block.ID != "" && m.isEntryFolded(block.ID) && block.Kind == BlockAssistant {
		title := strings.TrimSpace(block.Title)
		if title == "" {
			title = "assistant"
		}
		preview := firstLinePreview(block.Body, max(20, width-8))
		line := accentStyle.Render("│ ") + dimStyle.Render(title+" · folded")
		if preview != "" {
			line += dimStyle.Render(" · " + preview)
		}
		return []string{line}
	}
	rendered := m.renderBlock(block, width)
	if rendered == "" {
		return nil
	}
	// Tool rows already carry the Grok ◆ chrome; no extra indent (keeps diamond
	// flush with assistant │ / user rows for a clean scrollback column).
	return strings.Split(rendered, "\n")
}

// firstLinePreview returns a single-line plain preview of body text.
func firstLinePreview(body string, limit int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[:i]
	}
	return truncatePlain(body, limit)
}

func (m Model) renderBlock(block Block, width int) string {
	rendered := ""
	switch block.Kind {
	case BlockAssistant:
		rendered = m.renderAssistantBlock(block, max(20, width-2))
		// Magenta left bar + role cue: clearly not the user row.
		header := accentStyle.Render("│ ") + dimStyle.Render("bond")
		body := renderBlockMarker("│", accentStyle, rendered)
		if body != "" {
			rendered = header + "\n" + body
		} else {
			rendered = header
		}
	case BlockTool, BlockConfirmation:
		if block.Tool != nil {
			// When tool-detail density is off, drop completed tool calls so a
			// long session's wall of green lines collapses to the calls still
			// in flight (running/failed/pending stay visible).
			if !m.showToolDetails && block.Tool.Status == ToolDone {
				return ""
			}
			rendered = renderToolActivity(block.Tool, max(20, width-4))
		} else {
			rendered = renderBlockMarker("•", toolStyle, toolStyle.Render(block.Title)+" "+block.Body)
		}
	case BlockCommand:
		if block.Panel != nil {
			rendered = renderPanel(block.Panel, width)
		} else {
			// Wrap plain text to the timeline width so long /skills rows are not
			// hard-truncated by fitStyledLine. Title is a short prefix on line 1.
			rendered = renderCommandBlock(block.Title, block.Body, max(20, width-2))
		}
	case BlockSubagent:
		rendered = renderBlockMarker("↳", accentStyle, m.renderSubagentBlock(block, max(20, width-2)))
	case BlockReasoning:
		body := strings.TrimRight(block.Body, "\n")
		if body == "" {
			return ""
		}
		rendered = renderBlockMarker("│", dimStyle, m.renderReasoning(body, max(20, width-2)))
	case BlockError:
		rendered = renderBlockMarker("✗", errorStyle, errorStyle.Render(block.Title)+" "+block.Body)
	case BlockCompaction:
		body := strings.TrimSpace(block.Body)
		if body == "" {
			body = "context compacted"
		}
		rendered = dimStyle.Render("── compacted " + body + " ──")
	default:
		rendered = block.Body
	}
	return m.withBlockTimestamp(block, rendered)
}

// renderCommandBlock paints a slash-command result with word-wrap so multi-skill
// index lines remain readable instead of vanishing past the terminal edge.
func renderCommandBlock(title, body string, width int) string {
	title = strings.TrimSpace(title)
	body = strings.TrimRight(body, "\n")
	var logical []string
	switch {
	case title != "" && body != "":
		first, rest, found := strings.Cut(body, "\n")
		logical = append(logical, strings.TrimSpace(title+" "+first))
		if found && rest != "" {
			logical = append(logical, strings.Split(rest, "\n")...)
		}
	case title != "":
		logical = []string{title}
	default:
		if body == "" {
			return ""
		}
		logical = strings.Split(body, "\n")
	}
	parts := make([]string, 0, len(logical)*2)
	for i, line := range logical {
		if i == 0 && title != "" {
			// Style only the slash name on the first visual segment.
			prefix := commandStyle.Render(title)
			remainder := strings.TrimPrefix(strings.TrimSpace(line), title)
			remainder = strings.TrimSpace(remainder)
			if remainder == "" {
				parts = append(parts, wrapPlainLines(prefix, width)...)
				continue
			}
			// Wrap the plain remainder, then rejoin title onto the first wrapped row.
			wrapped := wrapPlainLines(remainder, max(8, width-lipgloss.Width(prefix)-1))
			if len(wrapped) == 0 {
				parts = append(parts, prefix)
				continue
			}
			parts = append(parts, prefix+" "+wrapped[0])
			parts = append(parts, wrapped[1:]...)
			continue
		}
		parts = append(parts, wrapPlainLines(line, width)...)
	}
	return renderBlockMarker("›", commandStyle, strings.Join(parts, "\n"))
}

func renderBlockMarker(marker string, style lipgloss.Style, rendered string) string {
	lines := renderBlockMarkerLines(marker, style, rendered)
	return strings.Join(lines, "\n")
}

func renderBlockMarkerLines(marker string, style lipgloss.Style, rendered string) []string {
	if strings.TrimSpace(rendered) == "" {
		return nil
	}
	return renderBlockMarkerLinesUnchecked(marker, style, rendered)
}

func renderBlockMarkerLinesUnchecked(marker string, style lipgloss.Style, rendered string) []string {
	return withBlockMarkerLines(marker, style, strings.Split(rendered, "\n"))
}

func withBlockMarkerLines(marker string, style lipgloss.Style, lines []string) []string {
	prefix := style.Render(marker) + " "
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines[i] = prefix + trimMarkdownMargin(line)
	}
	return lines
}

func (m Model) renderReasoningBlockLines(block Block, width int) []string {
	body := strings.TrimRight(block.Body, "\n")
	if body == "" {
		return nil
	}
	// Claude Code default: committed thinking is not rendered at all until
	// showThinking (Ctrl+T) or a per-entry expand. Ctrl+O only densifies tools.
	// A folded one-line header still steals vertical space and crowds tools.
	if !m.reasoningVisible(block.ID) {
		return nil
	}
	reasoningWidth := max(20, width-2)
	lines := renderReasoningFullLines(body, reasoningWidth)
	// Thinking: muted magenta accent (Grok accent_thinking).
	thinkStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Accent).Faint(true)
	lines = withBlockMarkerLines("│", thinkStyle, lines)
	return m.withBlockTimestampLines(block, lines)
}

func (m Model) withBlockTimestampLines(block Block, lines []string) []string {
	if !m.verbose || len(lines) == 0 || block.CreatedAt.IsZero() {
		return lines
	}
	stamp := dimStyle.Render(block.CreatedAt.Format("15:04") + " ")
	lines[0] = stamp + lines[0]
	return lines
}

func trimMarkdownMargin(line string) string {
	for i := 0; i < 2 && strings.HasPrefix(line, " "); i++ {
		line = strings.TrimPrefix(line, " ")
	}
	return line
}

func (m Model) withBlockTimestamp(block Block, rendered string) string {
	if !m.verbose || rendered == "" || block.CreatedAt.IsZero() {
		return rendered
	}
	stamp := dimStyle.Render(block.CreatedAt.Format("15:04") + " ")
	lines := strings.Split(rendered, "\n")
	lines[0] = stamp + lines[0]
	return strings.Join(lines, "\n")
}

// renderAssistantBlock renders committed assistant history through the normal
// Markdown cache. In-progress assistant text is rendered separately by
// renderLiveStreamLines and never reaches Glamour.
func (m Model) renderAssistantBlock(block Block, width int) string {
	if block.Body == "" {
		return ""
	}
	return m.renderCachedMarkdownForWidth(block.ID, block.Body, width)
}

// renderTurnTimestamp formats a turn's wall-clock annotation for the optional
// timestamps view: the start HH:MM, plus a (duration) suffix once the turn has
// ended. Returns "" when the turn has no start time (e.g. a seeded placeholder).
func renderTurnTimestamp(turn Turn) string {
	if turn.StartedAt.IsZero() {
		return ""
	}
	start := turn.StartedAt.Format("15:04")
	if turn.EndedAt.IsZero() || !turn.EndedAt.After(turn.StartedAt) {
		return dimStyle.Render("  · " + start)
	}
	d := turn.EndedAt.Sub(turn.StartedAt)
	return dimStyle.Render("  · " + start + " · " + formatDuration(d))
}

// formatDuration renders a short, human duration string (1m 42s / 42s / 8s).
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) - m*60
	return fmt.Sprintf("%dm %ds", m, s)
}

// renderReasoning renders committed extended thinking when showThinking is on.
// Otherwise returns "" — historical thinking stays out of the transcript so
// tools and answers remain the audit trail (live thinking uses a separate
// bottom preview via renderLiveReasoningLines).
func (m Model) renderReasoning(body string, width int) string {
	if !m.showThinking {
		return ""
	}
	return renderReasoningFull(body, width)
}

// renderReasoningFull renders the whole reasoning body dimmed, with a header
// noting the line count and that it is expanded. It is the unfolded complement
// of renderReasoningPreview.
func renderReasoningFull(body string, width int) string {
	return strings.Join(renderReasoningFullLines(body, width), "\n")
}

func renderReasoningFullLines(body string, width int) []string {
	bodyLines := renderReasoningBodyLines(body, width)
	out := make([]string, 1, len(bodyLines)+1)
	out[0] = renderReasoningFullHeaderLine(len(bodyLines))
	return append(out, bodyLines...)
}

func renderReasoningFullHeaderLine(lineCount int) string {
	return "  " + dimStyle.Render(fmt.Sprintf("⌥ thinking · %d lines (expanded)", lineCount))
}

func renderReasoningBodyLines(body string, width int) []string {
	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = "  " + dimStyle.Render(truncatePlain(line, contentWidth))
	}
	return lines
}

// liveReasoningTailLines is kept for showThinking full live paint helpers.
const liveReasoningTailLines = 4

// renderLiveReasoningLines paints an explicit full live thinking overlay
// (showThinking / Ctrl+T only). Default live preview is a single dock line —
// see liveReasoningDockSnippet — so transcript height stays stable.
func renderLiveReasoningLines(body string, width int, full, hasPending bool) []string {
	thinkStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Accent).Faint(true)
	if !full {
		// Compact mode is dock-only; no multi-line transcript overlay.
		return nil
	}
	var lines []string
	if body != "" {
		lines = renderReasoningFullLines(body, width)
		lines = withBlockMarkerLines("│", thinkStyle, lines)
	} else {
		lines = []string{thinkStyle.Render("│ ") + dimStyle.Render("∴ Thinking…")}
	}
	if hasPending {
		lines = append(lines, dimStyle.Render("│ ···"))
	}
	return lines
}

// liveReasoningDockSnippet returns one plain line of the latest thinking text
// for the busy turn-status row. Fixed single-line height → no scroll jitter.
func liveReasoningDockSnippet(body string, maxRunes int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	lines := lastReasoningLines(body, 1)
	if len(lines) == 0 {
		return ""
	}
	line := strings.Join(strings.Fields(lines[0]), " ")
	if maxRunes < 8 {
		maxRunes = 8
	}
	return truncatePlain(line, maxRunes)
}

// lastReasoningLines returns up to limit trailing non-split lines of body.
func lastReasoningLines(body string, limit int) []string {
	if limit < 1 || body == "" {
		return nil
	}
	all := strings.Split(body, "\n")
	// Drop a trailing empty segment from a final newline.
	if n := len(all); n > 0 && all[n-1] == "" {
		all = all[:n-1]
	}
	if len(all) <= limit {
		return all
	}
	return all[len(all)-limit:]
}

// renderReasoningPreview is kept for evidence dumps / helpers: header-only
// folded chrome for committed thinking when a compact label is needed.
func renderReasoningPreview(body string, width int) string {
	return strings.Join(renderReasoningPreviewLines(body, width), "\n")
}

func renderReasoningPreviewLines(body string, width int) []string {
	_ = width
	lineCount := strings.Count(body, "\n") + 1
	summary := reasoningSummary(body)
	header := fmt.Sprintf("⌥ thinking · %d lines (folded)", lineCount)
	if summary != "" {
		header = fmt.Sprintf("⌥ thinking: %s · %d lines (folded)", summary, lineCount)
	}
	return []string{"  " + dimStyle.Render(header)}
}

func firstReasoningLines(body string, limit int) []string {
	lines := make([]string, 0, min(limit, strings.Count(body, "\n")+1))
	for len(lines) < limit {
		line, rest, found := strings.Cut(body, "\n")
		lines = append(lines, line)
		if !found {
			break
		}
		body = rest
	}
	return lines
}

// reasoningSummary extracts a one-line title from an extended-thinking body:
// the first non-empty line, trimmed and capped. Used as the folded-preview
// header so the user can tell what the thinking was about without expanding it
// (Phase 5E, mirrors opencode's reasoningSummary). Returns "" for empty input.
func reasoningSummary(body string) string {
	for {
		line, rest, found := strings.Cut(body, "\n")
		line = strings.TrimSpace(line)
		if line != "" {
			return truncatePlain(line, 60)
		}
		if !found {
			return ""
		}
		body = rest
	}
}
