package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// MarkdownRenderer turns assistant Markdown into terminal ANSI.
// Plan B: goldmark + selective chroma (see mdrender.go), not whole-doc Glamour.
type MarkdownRenderer struct {
	width int
}

// NewMarkdownRenderer creates a renderer bound to the given wrap width.
func NewMarkdownRenderer(width int) (*MarkdownRenderer, error) {
	if width < 20 {
		width = 20
	}
	return &MarkdownRenderer{width: width}, nil
}

// Render renders Markdown text to formatted terminal output.
// P0: never return glamour-style corrupted lines — broken output falls back to
// plain wrapped source so the transcript stays readable.
func (r *MarkdownRenderer) Render(markdown string) (string, error) {
	if r == nil {
		return markdown, nil
	}
	width := r.width
	if width < 20 {
		width = 20
	}
	markdown = degradeNarrowMarkdownTables(markdown, width)
	out := renderMarkdownTerminal(markdown, width)
	if markdownOutputBroken(out, width) {
		return wrapPlainMarkdown(markdown, width), nil
	}
	// Final safety: clamp every line with ANSI-safe truncate (no re-hardwrap).
	return clampMarkdownLines(out, width), nil
}

// UpdateWidth updates the renderer's word wrap width.
func (r *MarkdownRenderer) UpdateWidth(width int) error {
	if width < 20 {
		width = 20
	}
	r.width = width
	return nil
}

func clampMarkdownLines(out string, width int) string {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	return strings.Join(lines, "\n")
}

type markdownCacheEntry struct {
	width    int
	body     string
	rendered string
}

// renderCachedMarkdownForWidth renders markdown for a block, memoizing by
// block ID so a streaming body only re-renders when content changes.
func (m Model) renderCachedMarkdownForWidth(blockID, body string, width int) string {
	if body == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}
	if m.markdownRenderer == nil {
		return wrapPlainMarkdown(body, width)
	}
	if err := m.markdownRenderer.UpdateWidth(width); err != nil {
		return wrapPlainMarkdown(body, width)
	}
	if m.markdownCache != nil {
		if entry, ok := m.markdownCache[blockID]; ok && entry.width == width && entry.body == body {
			return entry.rendered
		}
	}
	rendered, err := m.markdownRenderer.Render(body)
	if err != nil {
		return wrapPlainMarkdown(body, width)
	}
	if m.markdownCache != nil {
		m.markdownCache[blockID] = markdownCacheEntry{width: width, body: body, rendered: rendered}
	}
	return rendered
}

// invalidateMarkdownCache drops cached renders (width / theme / density change).
func (m Model) invalidateMarkdownCache() {
	for key := range m.markdownCache {
		delete(m.markdownCache, key)
	}
	if m.timelineLinesCache != nil {
		m.timelineLinesCache.blockLines = nil
	}
}

func degradeNarrowMarkdownTables(markdown string, width int) string {
	if width >= 40 {
		return markdown
	}
	lines := strings.Split(markdown, "\n")
	var out []string
	for i := 0; i < len(lines); {
		if i+1 >= len(lines) || !isMarkdownTableRow(lines[i]) || !isMarkdownTableSeparator(lines[i+1]) {
			out = append(out, lines[i])
			i++
			continue
		}
		headers := markdownTableCells(lines[i])
		i += 2
		for i < len(lines) && isMarkdownTableRow(lines[i]) {
			cells := markdownTableCells(lines[i])
			parts := make([]string, 0, len(cells))
			for column, cell := range cells {
				if column < len(headers) && headers[column] != "" {
					parts = append(parts, "**"+headers[column]+":** "+cell)
				} else {
					parts = append(parts, cell)
				}
			}
			out = append(out, "- "+strings.Join(parts, "; "))
			i++
		}
	}
	return strings.Join(out, "\n")
}

func isMarkdownTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") && len(markdownTableCells(line)) > 1
}

func isMarkdownTableSeparator(line string) bool {
	if !isMarkdownTableRow(line) {
		return false
	}
	for _, cell := range markdownTableCells(line) {
		cell = strings.TrimSpace(cell)
		cell = strings.TrimPrefix(strings.TrimSuffix(cell, ":"), ":")
		if len(cell) < 3 || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func markdownTableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(strings.TrimSuffix(line, "|"), "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func containsMarkdownSyntax(markdown string) bool {
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, ">") ||
			strings.HasPrefix(trimmed, "- ") ||
			strings.HasPrefix(trimmed, "* ") ||
			strings.HasPrefix(trimmed, "```") ||
			strings.HasPrefix(trimmed, "|") {
			return true
		}
		// Ordered list "1. "
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			dot := strings.IndexByte(trimmed, '.')
			if dot > 0 && dot+1 < len(trimmed) && trimmed[dot+1] == ' ' {
				return true
			}
		}
	}
	return strings.Contains(markdown, "**") ||
		strings.Contains(markdown, "`") ||
		strings.Contains(markdown, "](") ||
		strings.Contains(markdown, "__")
}
