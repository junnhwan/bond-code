package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// One foreground color per context source so the proportional bar reads as a
// composition at a glance: system prompt = brand accent (the base), conversation
// = warm body text, tool results = the cool tool blue, compaction summary =
// amber to flag "this part is compressed memory".
var (
	breakdownSystemStyle  = lipgloss.NewStyle().Foreground(DefaultTheme.Accent)
	breakdownConvStyle    = lipgloss.NewStyle().Foreground(DefaultTheme.Text)
	breakdownToolStyle    = lipgloss.NewStyle().Foreground(DefaultTheme.Tool)
	breakdownSummaryStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Warning)
)

// renderContextBreakdownBar renders the token composition as a single bar of
// `width` filled cells, each segment proportional to its share of the total.
// Mirrors opencode's context breakdown bar: segments always sum to width so the
// bar reads as a ratio and never over/underflows. Returns "" when empty.
func renderContextBreakdownBar(view ContextBreakdownView, width int) string {
	total := view.System + view.Conversation + view.ToolResult + view.Summary
	if total <= 0 || width <= 0 {
		return ""
	}
	segments := []struct {
		tokens int
		style  lipgloss.Style
	}{
		{view.System, breakdownSystemStyle},
		{view.Conversation, breakdownConvStyle},
		{view.ToolResult, breakdownToolStyle},
		{view.Summary, breakdownSummaryStyle},
	}
	lastNonEmpty := -1
	for i, s := range segments {
		if s.tokens > 0 {
			lastNonEmpty = i
		}
	}
	var sb strings.Builder
	allocated := 0
	for i, s := range segments {
		if s.tokens <= 0 {
			continue
		}
		w := width * s.tokens / total
		if i == lastNonEmpty {
			w = width - allocated // absorb rounding remainder -> bar fills width
		}
		if w > 0 {
			sb.WriteString(s.style.Render(strings.Repeat("█", w)))
			allocated += w
		}
	}
	return sb.String()
}

// renderContextBreakdownLegend renders one dim line listing each non-empty
// segment's token count (sys / conv / tool / sum), truncated to width.
func renderContextBreakdownLegend(view ContextBreakdownView, width int) string {
	parts := []string{}
	if view.System > 0 {
		parts = append(parts, "sys "+formatTokens(view.System))
	}
	if view.Conversation > 0 {
		parts = append(parts, "conv "+formatTokens(view.Conversation))
	}
	if view.ToolResult > 0 {
		parts = append(parts, "tool "+formatTokens(view.ToolResult))
	}
	if view.Summary > 0 {
		parts = append(parts, "sum "+formatTokens(view.Summary))
	}
	if len(parts) == 0 {
		return ""
	}
	return dimStyle.Render(truncatePlain(strings.Join(parts, " · "), width))
}

// renderContextBreakdownLines returns the bar followed by its legend for
// /context panels and tests. Returns nil when there is no breakdown.
func renderContextBreakdownLines(view ContextBreakdownView, width int) []string {
	bar := renderContextBreakdownBar(view, max(8, width))
	if bar == "" {
		return nil
	}
	legend := renderContextBreakdownLegend(view, width)
	if legend == "" {
		return []string{bar}
	}
	return []string{bar, legend}
}
