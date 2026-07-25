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
			rendered = renderBlockMarker("›", commandStyle, commandStyle.Render(block.Title)+" "+block.Body)
		}
	case BlockSubagent:
		rendered = renderBlockMarker("↳", accentStyle, renderSubagentBlock(block, max(20, width-2)))
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
	reasoningWidth := max(20, width-2)
	// Global density (showThinking) plus per-entry left/right overrides.
	// Default is folded; left/right must still expand at showThinking=false.
	var lines []string
	if m.reasoningExpanded(block.ID) {
		lines = renderReasoningFullLines(body, reasoningWidth)
	} else {
		lines = renderReasoningPreviewLines(body, reasoningWidth)
	}
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

// renderReasoning renders an extended-thinking block, honoring the
// showThinking density toggle: expanded (full body) or folded (preview). The
// toggle is global — every reasoning block expands or folds together — which
// matches how the display-density controls work elsewhere.
func (m Model) renderReasoning(body string, width int) string {
	if m.showThinking {
		return renderReasoningFull(body, width)
	}
	return renderReasoningPreview(body, width)
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

// renderReasoningPreview renders the model's extended-thinking stream as a
// folded preview: a header line noting thinking happened (and roughly how
// much), the first few lines each capped to the timeline width, then an
// ellipsis for the rest. The full reasoning is rarely worth reading inline,
// so we keep it short — the point is to surface the process, not the prose.
func renderReasoningPreview(body string, width int) string {
	return strings.Join(renderReasoningPreviewLines(body, width), "\n")
}

func renderReasoningPreviewLines(body string, width int) []string {
	_ = width
	lineCount := strings.Count(body, "\n") + 1
	// Folded thinking: header only — body stays hidden until expanded.
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
