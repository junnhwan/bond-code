package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

const markdownRendererStyle = "bondcode-dark-code"

// MarkdownRenderer handles rendering Markdown with syntax highlighting
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
	width    int
	style    string
}

// NewMarkdownRenderer creates a new Markdown renderer with the given width
func NewMarkdownRenderer(width int) (*MarkdownRenderer, error) {
	renderer, err := newTermMarkdownRenderer(width, markdownRendererStyle)
	if err != nil {
		return nil, err
	}

	return &MarkdownRenderer{
		renderer: renderer,
		width:    width,
		style:    markdownRendererStyle,
	}, nil
}

// Render renders Markdown text to formatted terminal output
func (r *MarkdownRenderer) Render(markdown string) (string, error) {
	if r.renderer == nil || !containsMarkdownSyntax(markdown) {
		return markdown, nil // Keep plain text unstyled and searchable
	}

	markdown = degradeNarrowMarkdownTables(markdown, r.width)
	rendered, err := r.renderer.Render(markdown)
	if err != nil {
		return markdown, err // Fallback to plain text on error
	}

	return strings.TrimRight(rendered, "\n"), nil
}

// UpdateWidth updates the renderer's word wrap width
func (r *MarkdownRenderer) UpdateWidth(width int) error {
	if width == r.width {
		return nil
	}

	renderer, err := newTermMarkdownRenderer(width, r.style)
	if err != nil {
		return err
	}

	r.renderer = renderer
	r.width = width
	return nil
}

func newTermMarkdownRenderer(width int, style string) (*glamour.TermRenderer, error) {
	if strings.TrimSpace(style) == "" {
		style = markdownRendererStyle
	}
	styleConfig := markdownStyleConfig(style)
	return glamour.NewTermRenderer(
		glamour.WithStyles(styleConfig),
		glamour.WithWordWrap(width),
	)
}

func markdownStyleConfig(style string) ansi.StyleConfig {
	cfg := styles.DarkStyleConfig
	zero := uint(0)
	one := uint(1)
	quoteToken := "│ "
	// Anchor markdown body text to GrokNight primary (#e1e1e1).
	cfg.Document.StylePrimitive = ansi.StylePrimitive{
		Color: stringPtr("#e1e1e1"),
	}
	cfg.Document.Margin = &zero
	cfg.Document.BlockPrefix = ""
	cfg.Document.BlockSuffix = ""
	cfg.Paragraph.Margin = &zero
	// Real headings: no raw "## " prefixes (Grok-style rendered titles).
	// Color + bold distinguish level; BlockSuffix keeps spacing after titles.
	cfg.Heading.BlockSuffix = "\n"
	cfg.Heading.StylePrimitive = ansi.StylePrimitive{
		Color:  stringPtr("#bb9af7"),
		Bold:   boolPtr(true),
		Prefix: "",
	}
	cfg.H1.Prefix = ""
	cfg.H1.StylePrimitive = ansi.StylePrimitive{
		Color:  stringPtr("#e1e1e1"),
		Bold:   boolPtr(true),
		Prefix: "",
	}
	cfg.H2.Prefix = ""
	cfg.H2.StylePrimitive = ansi.StylePrimitive{
		Color:  stringPtr("#bb9af7"),
		Bold:   boolPtr(true),
		Prefix: "",
	}
	cfg.H3.Prefix = ""
	cfg.H3.StylePrimitive = ansi.StylePrimitive{
		Color:  stringPtr("#c8c8c8"),
		Bold:   boolPtr(true),
		Prefix: "",
	}
	cfg.H4.Prefix = ""
	cfg.H5.Prefix = ""
	cfg.H6.Prefix = ""
	cfg.BlockQuote.Indent = &one
	cfg.BlockQuote.IndentToken = &quoteToken
	cfg.List.LevelIndent = 2
	cfg.CodeBlock.Margin = &zero
	if style != markdownRendererStyle {
		return styles.NoTTYStyleConfig
	}
	return cfg
}

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }

type markdownCacheEntry struct {
	width    int
	body     string
	rendered string
}

// renderCachedMarkdownForWidth renders markdown for a block, memoizing the result by
// block ID so a streaming body only re-renders when its content changes.
// The cache is a map (a reference type), so writes made from View's value
// receiver persist across frames without round-tripping through Update.
func (m Model) renderCachedMarkdownForWidth(blockID, body string, width int) string {
	if body == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}
	if m.markdownRenderer == nil {
		return body
	}
	if err := m.markdownRenderer.UpdateWidth(width); err != nil {
		return body
	}
	if m.markdownCache != nil {
		if entry, ok := m.markdownCache[blockID]; ok && entry.width == width && entry.body == body {
			return entry.rendered
		}
	}
	rendered, err := m.markdownRenderer.Render(body)
	if err != nil {
		return body
	}
	if m.markdownCache != nil {
		m.markdownCache[blockID] = markdownCacheEntry{width: width, body: body, rendered: rendered}
	}
	return rendered
}

// invalidateMarkdownCache drops cached renders, e.g. when the render width changes.
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
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "|") {
			return true
		}
	}
	return strings.Contains(markdown, "**") || strings.Contains(markdown, "` ") || strings.Contains(markdown, "](")
}
