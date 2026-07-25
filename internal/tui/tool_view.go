package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const maxExpandedToolOutputLines = 12

// renderToolActivity paints a Grok-like tool row:
//
//	◆ Read path/to/file · 12ms
//	◆ Run go test ./...
//
// Single header line by default (diamond + bold verb + colored subject + dim
// meta). No Claude-style "✓ … / ⎿ …" two-line card. Expanded bodies hang under
// the diamond with a light indent.
func renderToolActivity(tool *ToolBlock, width int) string {
	if tool == nil {
		return ""
	}
	if width < 20 {
		width = 20
	}

	r := RendererFor(tool.Name)
	registered := toolIsRegistered(tool.Name)
	verbose := renderVerbose

	glyph, glyphStyle := toolStatusGlyph(tool)
	verb := tool.Name
	if v := r.Verb(tool); v != "" {
		verb = v
	}
	subjectRaw := toolActivitySubject(tool)
	if s := r.Subject(tool, verbose); s != "" {
		subjectRaw = s
	}

	// Grok header: ◆ + bold Verb + path/command operand (not dim-all-gray).
	verbStyle := lipgloss.NewStyle().
		Foreground(DefaultTheme.Text).
		Bold(true)
	if tool.Status == ToolFailed || tool.Status == ToolBlocked {
		verbStyle = errorStyle
	} else if tool.Status == ToolRunning || tool.Status == ToolPending {
		verbStyle = lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted).Bold(true)
	}

	subjectStyle := dimStyle
	if looksLikePathSubject(subjectRaw) {
		subjectStyle = pathStyle
	} else if tool.Name == "run_command" || strings.EqualFold(verb, "Run") {
		subjectStyle = commandStyle
	}

	// Budget for subject after diamond + verb + spaces + optional meta.
	meta := toolHeaderMeta(tool, registered, r, verbose)
	metaBudget := 0
	if meta != "" {
		metaBudget = lipgloss.Width(meta) + 3 // " · "
	}
	prefixW := 2 + lipgloss.Width(verb) + 1 // "◆ " + verb + " "
	subjLimit := max(8, width-prefixW-metaBudget)
	subject := truncatePlain(subjectRaw, subjLimit)

	var b strings.Builder
	b.WriteString(glyphStyle.Render(glyph))
	b.WriteString(" ")
	b.WriteString(verbStyle.Render(verb))
	if subject != "" {
		b.WriteString(" ")
		b.WriteString(subjectStyle.Render(subject))
	}
	if meta != "" {
		b.WriteString(dimStyle.Render(" · " + meta))
	}
	header := b.String()
	// Soft full-row dim when completed + collapsed (Grok muted_collapsed feel).
	if tool.Status == ToolDone && tool.Collapsed && !verbose {
		// Keep path/command colors; only ensure we don't add a second status line.
	}

	lines := []string{header}

	// Expanded body only — never a second "⎿ result" chrome row.
	expanded := !tool.Collapsed || verbose
	if !expanded {
		return strings.Join(lines, "\n")
	}
	if registered && tool.Status != ToolPending {
		if detail := r.Detail(tool, max(20, width-4), verbose); detail != "" {
			lines = append(lines, indentRenderedBlock(detail, "  "))
			return strings.Join(lines, "\n")
		}
	}
	if tool.Error != "" {
		lines = append(lines, "  "+errorStyle.Render(truncatePlain(tool.Error, max(8, width-4))))
	}
	if shouldCollapseToolOutput(tool.Output) || (verbose && strings.TrimSpace(tool.Output) != "") {
		if details := renderToolDetails(tool, max(20, width-2)); details != "" {
			lines = append(lines, details)
		}
	} else if out := strings.TrimSpace(tool.Output); out != "" && expanded {
		// Short output: hang under header without "output:" label noise.
		for _, ol := range splitToolOutputLines(out) {
			lines = append(lines, "  "+renderDiffLine(ol, max(8, width-4)))
		}
	}
	return strings.Join(lines, "\n")
}

// toolHeaderMeta is the dim trailing fragment on the Grok tool header
// (duration, running, collapse hint, semantic one-line result). Kept short.
func toolHeaderMeta(tool *ToolBlock, registered bool, r ToolRenderer, verbose bool) string {
	parts := make([]string, 0, 3)
	switch tool.Status {
	case ToolPending:
		if strings.EqualFold(tool.Risk, "high") {
			parts = append(parts, "confirm · y/n")
		} else {
			parts = append(parts, "confirm · y approve · n reject")
		}
	case ToolRunning:
		parts = append(parts, "running")
	case ToolFailed, ToolBlocked, ToolRejected:
		if s := toolActivityStatus(tool); s != "" {
			parts = append(parts, s)
		}
	case ToolDone:
		// Prefer a short semantic result over bare "done".
		if registered {
			if core := strings.TrimSpace(r.Result(tool, verbose)); core != "" &&
				!strings.EqualFold(core, "done") && core != toolActivitySubject(tool) {
				parts = append(parts, truncatePlain(core, 28))
			}
		}
		if tool.Collapsed && !verbose && shouldCollapseToolOutput(tool.Output) {
			parts = append(parts, fmt.Sprintf("%d lines", lineCount(tool.Output)))
		}
	}
	if tool.Duration > 0 && tool.Status != ToolRunning {
		parts = append(parts, tool.Duration.Round(time.Millisecond).String())
	}
	return strings.Join(parts, " · ")
}

// looksLikePathSubject reports whether a tool subject is likely a filesystem path.
func looksLikePathSubject(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, `/\`) {
		return true
	}
	return strings.Contains(s, ".") && !strings.Contains(s, " ")
}

// toolStatusGlyph is the Grok diamond bullet; color carries lifecycle status
// (accent while running, green when done, red on failure) — not ✓/✗ glyphs.
func toolStatusGlyph(tool *ToolBlock) (string, lipgloss.Style) {
	if tool == nil {
		return "◆", toolStyle
	}
	switch tool.Status {
	case ToolPending:
		return "◆", confirmStyle
	case ToolRunning:
		return "◆", accentStyle
	case ToolDone:
		return "◆", successStyle
	case ToolFailed:
		return "◆", errorStyle
	case ToolBlocked:
		return "◆", errorStyle
	case ToolRejected:
		return "◆", confirmStyle
	default:
		return "◆", toolStyle
	}
}

// toolActivityResultLine composes the indented status/result line beneath a
// tool call. It prefers the trailing hint (duration, summary, approve keys,
// collapsed note) and falls back to the bare status label.
func toolActivityResultLine(tool *ToolBlock) string {
	status := toolActivityStatus(tool)
	trailing := toolActivityTrailing(tool)
	if trailing == "" {
		return status
	}
	if status == "" || strings.Contains(trailing, "collapsed") {
		return trailing
	}
	return status + " · " + trailing
}

func toolActivitySubject(tool *ToolBlock) string {
	if tool == nil {
		return ""
	}
	params := parseToolInput(tool.Input)
	value := func(keys ...string) string {
		for _, key := range keys {
			if v := strings.TrimSpace(params[key]); v != "" {
				return v
			}
		}
		return ""
	}
	switch tool.Name {
	case "read_file", "write_file", "list_dir":
		return firstNonEmpty(value("path"), tool.Summary, toolLabel(tool), tool.Name)
	case "search_text", "search_code", "web_search":
		pattern := firstNonEmpty(value("query", "pattern"), tool.Summary)
		if pattern != "" {
			return fmt.Sprintf("%q", pattern)
		}
	case "run_command", "execute_command":
		return firstNonEmpty(value("command"), tool.Summary, toolLabel(tool), tool.Name)
	}
	return firstNonEmpty(tool.Summary, toolLabel(tool), tool.Name)
}

func toolActivityStatus(tool *ToolBlock) string {
	if tool == nil {
		return ""
	}
	switch tool.Status {
	case ToolPending:
		return "confirm"
	case ToolRunning:
		return "running"
	case ToolDone:
		if (tool.Name == "search_text" || tool.Name == "search_code") && tool.Summary != "" {
			return tool.Summary
		}
		return "done"
	case ToolFailed:
		return "failed"
	case ToolBlocked:
		return "blocked"
	case ToolRejected:
		return "rejected"
	default:
		return string(tool.Status)
	}
}

func toolActivityTrailing(tool *ToolBlock) string {
	if tool == nil {
		return ""
	}
	if tool.Status == ToolPending {
		if strings.EqualFold(tool.Risk, "high") {
			return "← → select · Enter · n reject"
		}
		return "y approve · n reject"
	}
	if tool.Collapsed && !renderVerbose && shouldCollapseToolOutput(tool.Output) {
		return fmt.Sprintf("collapsed %d lines · Ctrl+O details", lineCount(tool.Output))
	}
	if tool.Duration > 0 {
		return tool.Duration.Round(time.Millisecond).String()
	}
	if tool.Summary != "" && tool.Summary != toolActivitySubject(tool) && tool.Summary != tool.Name {
		return truncatePlain(tool.Summary, 32)
	}
	return ""
}

func renderToolDetails(tool *ToolBlock, width int) string {
	contentWidth := max(1, width-2)
	var lines []string
	// Hang under diamond header with two spaces (Grok block indent, not ⎿).
	pad := "  "
	dim := func(text string) {
		lines = append(lines, pad+dimStyle.Render(truncatePlain(text, contentWidth)))
	}

	if tool.Error != "" {
		lines = append(lines, pad+errorStyle.Render(truncatePlain(tool.Error, contentWidth)))
	}
	if tool.Output != "" {
		outputLines := splitToolOutputLines(tool.Output)
		hidden := 0
		if len(outputLines) > maxExpandedToolOutputLines {
			hidden = len(outputLines) - maxExpandedToolOutputLines
			outputLines = outputLines[:maxExpandedToolOutputLines]
		}
		for _, outputLine := range outputLines {
			lines = append(lines, pad+renderDiffLine(outputLine, contentWidth))
		}
		if hidden > 0 {
			dim(fmt.Sprintf("… +%d lines", hidden))
		}
	}
	return strings.Join(lines, "\n")
}

// renderDiffLine colors unified-diff +/- lines (green/red) while leaving the
// file headers (+++/---) and ordinary output dim.
func renderDiffLine(line string, width int) string {
	line = truncatePlain(line, width)
	switch {
	case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
		return dimStyle.Render(line)
	case strings.HasPrefix(line, "+"):
		return diffAddStyle.Render(line)
	case strings.HasPrefix(line, "-"):
		return diffRemoveStyle.Render(line)
	default:
		return dimStyle.Render(line)
	}
}

func splitToolOutputLines(output string) []string {
	output = strings.TrimSuffix(output, "\n")
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

func lineCount(value string) int {
	value = strings.TrimSuffix(value, "\n")
	if value == "" {
		return 0
	}
	return strings.Count(value, "\n") + 1
}
