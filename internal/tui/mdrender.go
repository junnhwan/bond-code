package tui

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Terminal markdown renderer (Plan B): goldmark AST → ANSI, Claude/Grok style.
// Replaces whole-document Glamour, which mangled CJK + mixed code on Windows.

var (
	mdHeading = lipgloss.NewStyle().Foreground(DefaultTheme.Accent).Bold(true)
	mdH1      = lipgloss.NewStyle().Foreground(DefaultTheme.Text).Bold(true)
	mdStrong  = lipgloss.NewStyle().Foreground(DefaultTheme.Text).Bold(true)
	mdEmph    = lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted).Italic(true)
	mdCode    = lipgloss.NewStyle().Foreground(DefaultTheme.Command)
	mdLink    = lipgloss.NewStyle().Foreground(DefaultTheme.Running).Underline(true)
	mdQuote   = lipgloss.NewStyle().Foreground(DefaultTheme.Dim).Italic(true)
	mdFence   = lipgloss.NewStyle().Foreground(DefaultTheme.Dim)
	mdBody    = lipgloss.NewStyle().Foreground(DefaultTheme.Text)
	mdBullet  = lipgloss.NewStyle().Foreground(DefaultTheme.Accent)
)

// renderMarkdownTerminal converts Markdown source to terminal ANSI lines of at
// most width display columns. On parse failure it returns plain wrapped text.
func renderMarkdownTerminal(src string, width int) string {
	if width < 20 {
		width = 20
	}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.TrimRight(src, "\n")
	if src == "" {
		return ""
	}
	if !containsMarkdownSyntax(src) {
		return wrapPlainMarkdown(src, width)
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
	source := []byte(src)
	reader := text.NewReader(source)
	doc := md.Parser().Parse(reader)
	var b strings.Builder
	ctx := &mdRenderCtx{
		src:   source,
		width: width,
		out:   &b,
	}
	if err := ctx.renderBlocks(doc); err != nil {
		return wrapPlainMarkdown(src, width)
	}
	out := strings.TrimRight(b.String(), "\n")
	if markdownOutputBroken(out, width) {
		return wrapPlainMarkdown(src, width)
	}
	return out
}

// markdownOutputBroken detects the glamour-class failure mode: lines far over
// width, or dense reverse/pink soup from corrupted ANSI (heuristic).
func markdownOutputBroken(out string, width int) bool {
	lines := strings.Split(out, "\n")
	over := 0
	for _, line := range lines {
		w := ansi.StringWidth(line)
		if w > width+2 {
			over++
		}
		// Extremely long single visual line is always wrong in a TUI.
		if w > width*3 {
			return true
		}
	}
	// More than a third of lines overflow → renderer failed to wrap.
	if len(lines) > 4 && over*3 > len(lines) {
		return true
	}
	return false
}

func wrapPlainMarkdown(src string, width int) string {
	var out []string
	for _, para := range strings.Split(src, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		wrapped := ansi.Wordwrap(para, width, "")
		wrapped = ansi.Hardwrap(wrapped, width, true)
		out = append(out, strings.Split(wrapped, "\n")...)
	}
	return strings.Join(out, "\n")
}

type mdRenderCtx struct {
	src   []byte
	width int
	out   *strings.Builder
	list  int // nest depth
}

func (c *mdRenderCtx) write(s string) { c.out.WriteString(s) }

func (c *mdRenderCtx) writeln(s string) {
	c.out.WriteString(s)
	c.out.WriteByte('\n')
}

func (c *mdRenderCtx) renderBlocks(node ast.Node) error {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if err := c.renderBlock(child); err != nil {
			return err
		}
	}
	return nil
}

func (c *mdRenderCtx) renderBlock(node ast.Node) error {
	switch n := node.(type) {
	case *ast.Heading:
		var inner strings.Builder
		c.renderInlines(&inner, n)
		text := strings.TrimSpace(inner.String())
		style := mdHeading
		if n.Level == 1 {
			style = mdH1
		}
		c.writeWrappedStyled(style.Render(text))
		c.writeln("")
	case *ast.Paragraph:
		var inner strings.Builder
		c.renderInlines(&inner, n)
		c.writeWrappedMixed(inner.String())
		c.writeln("")
	case *ast.TextBlock:
		// Tight list items use TextBlock instead of Paragraph.
		var inner strings.Builder
		c.renderInlines(&inner, n)
		text := strings.TrimSpace(inner.String())
		if text != "" {
			c.writeWrappedMixed(text)
		}
	case *ast.Blockquote:
		var inner strings.Builder
		sub := &mdRenderCtx{src: c.src, width: max(10, c.width-2), out: &inner, list: c.list}
		_ = sub.renderBlocks(n)
		for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
			c.writeln(mdQuote.Render("│ ") + line)
		}
		c.writeln("")
	case *ast.List:
		c.list++
		i := 1
		if n.Start != 0 {
			i = n.Start
		}
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			li, ok := item.(*ast.ListItem)
			if !ok {
				continue
			}
			bullet := "• "
			if n.IsOrdered() {
				bullet = fmt.Sprintf("%d. ", i)
				i++
			}
			var inner strings.Builder
			sub := &mdRenderCtx{src: c.src, width: max(10, c.width-lipgloss.Width(bullet)), out: &inner, list: c.list}
			_ = sub.renderBlocks(li)
			body := strings.TrimRight(inner.String(), "\n")
			lines := strings.Split(body, "\n")
			if len(lines) == 0 {
				lines = []string{""}
			}
			c.writeln(mdBullet.Render(bullet) + lines[0])
			pad := strings.Repeat(" ", lipgloss.Width(bullet))
			for _, line := range lines[1:] {
				c.writeln(pad + line)
			}
		}
		c.list--
		c.writeln("")
	case *ast.FencedCodeBlock:
		lang := string(n.Language(c.src))
		var code bytes.Buffer
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			code.Write(line.Value(c.src))
		}
		c.writeCodeBlock(lang, code.String())
		c.writeln("")
	case *ast.CodeBlock:
		var code bytes.Buffer
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			code.Write(line.Value(c.src))
		}
		c.writeCodeBlock("", code.String())
		c.writeln("")
	case *ast.ThematicBreak:
		c.writeln(mdFence.Render(strings.Repeat("─", min(c.width, 40))))
		c.writeln("")
	case *east.Table:
		c.renderTable(n)
	case *ast.HTMLBlock:
		// Skip raw HTML noise from models.
	default:
		if n.Type() == ast.TypeBlock {
			_ = c.renderBlocks(n)
		}
	}
	return nil
}

func (c *mdRenderCtx) renderTable(table *east.Table) {
	// Flatten rows to "cell · cell" — width-safe, no glamour table chrome.
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []string
		switch r := row.(type) {
		case *east.TableHeader:
			for cell := r.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells = append(cells, c.cellText(cell))
			}
		case *east.TableRow:
			for cell := r.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells = append(cells, c.cellText(cell))
			}
		default:
			continue
		}
		if len(cells) == 0 {
			continue
		}
		line := strings.Join(cells, " · ")
		c.writeWrappedMixed(mdStrong.Render(line))
	}
	c.writeln("")
}

func (c *mdRenderCtx) cellText(node ast.Node) string {
	var inner strings.Builder
	c.renderInlinesOnly(&inner, node)
	return strings.TrimSpace(ansi.Strip(inner.String()))
}

func (c *mdRenderCtx) renderInlines(b *strings.Builder, node ast.Node) {
	ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		// Skip the root itself (already handling children).
		if n == node {
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *ast.Text:
			segment := t.Segment
			b.Write(segment.Value(c.src))
			if t.HardLineBreak() {
				b.WriteByte('\n')
			} else if t.SoftLineBreak() {
				b.WriteByte(' ')
			}
			return ast.WalkContinue, nil
		case *ast.CodeSpan:
			// CodeSpan stores content in child Text nodes.
			var inner bytes.Buffer
			for ch := t.FirstChild(); ch != nil; ch = ch.NextSibling() {
				if txt, ok := ch.(*ast.Text); ok {
					inner.Write(txt.Segment.Value(c.src))
				}
			}
			b.WriteString(mdCode.Render(inner.String()))
			return ast.WalkSkipChildren, nil
		case *ast.Emphasis:
			var inner strings.Builder
			c.renderInlinesOnly(&inner, t)
			if t.Level >= 2 {
				b.WriteString(mdStrong.Render(inner.String()))
			} else {
				b.WriteString(mdEmph.Render(inner.String()))
			}
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			var inner strings.Builder
			c.renderInlinesOnly(&inner, t)
			label := inner.String()
			if label == "" {
				label = string(t.Destination)
			}
			b.WriteString(mdLink.Render(label))
			return ast.WalkSkipChildren, nil
		case *ast.AutoLink:
			b.WriteString(mdLink.Render(string(t.URL(c.src))))
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		case *ast.String:
			b.Write(t.Value)
			return ast.WalkContinue, nil
		}
		return ast.WalkContinue, nil
	})
}

func (c *mdRenderCtx) renderInlinesOnly(b *strings.Builder, node ast.Node) {
	for ch := node.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch t := ch.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(c.src))
			if t.HardLineBreak() {
				b.WriteByte('\n')
			} else if t.SoftLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.CodeSpan:
			var inner bytes.Buffer
			for c2 := t.FirstChild(); c2 != nil; c2 = c2.NextSibling() {
				if txt, ok := c2.(*ast.Text); ok {
					inner.Write(txt.Segment.Value(c.src))
				}
			}
			b.WriteString(inner.String())
		case *ast.String:
			b.Write(t.Value)
		default:
			c.renderInlinesOnly(b, ch)
		}
	}
}

func (c *mdRenderCtx) writeWrappedStyled(styled string) {
	// Strip to wrap, then re-apply is lossy; for headings keep single-style wrap.
	plain := ansi.Strip(styled)
	wrapped := ansi.Wordwrap(plain, c.width, "")
	wrapped = ansi.Hardwrap(wrapped, c.width, true)
	// Re-color whole lines with body/heading already applied to input — if input
	// was fully styled one color, re-style each visual line the same way is hard.
	// Headings are short; write as-is if fits, else plain wrap + style each line.
	if ansi.StringWidth(styled) <= c.width {
		c.writeln(styled)
		return
	}
	for _, line := range strings.Split(wrapped, "\n") {
		// Best-effort: style plain wrapped lines as body (heading color lost on wrap).
		c.writeln(mdHeading.Render(line))
	}
}

func (c *mdRenderCtx) writeWrappedMixed(s string) {
	// s may contain ANSI from inline code/strong. Wrap using open-source safe wrap.
	wrapped := ansi.Wordwrap(s, c.width, "")
	wrapped = ansi.Hardwrap(wrapped, c.width, true)
	for i, line := range strings.Split(wrapped, "\n") {
		if i > 0 {
			c.out.WriteByte('\n')
		}
		// Ensure base foreground if line is plain.
		if !strings.Contains(line, "\x1b[") {
			c.out.WriteString(mdBody.Render(line))
		} else {
			c.out.WriteString(line)
		}
	}
	c.out.WriteByte('\n')
}

func (c *mdRenderCtx) writeCodeBlock(lang, code string) {
	code = strings.TrimRight(code, "\n")
	lang = strings.TrimSpace(lang)
	c.writeln(mdFence.Render("```" + lang))

	highlighted := false
	if lang != "" && looksLikeCode(code) {
		if out, ok := highlightCode(lang, code, c.width); ok {
			for _, line := range strings.Split(out, "\n") {
				// Truncate safely; never re-hardwrap chroma output aggressively.
				if ansi.StringWidth(line) > c.width {
					line = ansi.Truncate(line, c.width, "")
				}
				c.writeln(line)
			}
			highlighted = true
		}
	}
	if !highlighted {
		for _, line := range strings.Split(code, "\n") {
			if ansi.StringWidth(line) > c.width {
				// Plain hard wrap for long lines (no ANSI yet).
				w := ansi.Hardwrap(line, c.width, true)
				for _, part := range strings.Split(w, "\n") {
					c.writeln(mdCode.Render(part))
				}
			} else {
				c.writeln(mdCode.Render(line))
			}
		}
	}
	c.writeln(mdFence.Render("```"))
}

// looksLikeCode rejects Chinese-heavy prose dumped into a fenced block — chroma
// on that content is what produced the pink soup in user screenshots.
func looksLikeCode(code string) bool {
	if code == "" {
		return false
	}
	var runes, cjk int
	for _, r := range code {
		runes++
		if r >= 0x4E00 && r <= 0x9FFF {
			cjk++
		}
	}
	if runes == 0 {
		return false
	}
	// More than ~35% CJK → treat as prose, not source.
	if cjk*100/runes > 35 {
		return false
	}
	return true
}

func highlightCode(lang, code string, width int) (string, bool) {
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		return "", false
	}
	lexer = chroma.Coalesce(lexer)
	style := styles.Get("tokyonight-storm")
	if style == nil {
		style = styles.Get("monokai")
	}
	if style == nil {
		style = styles.Fallback
	}
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return "", false
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, it); err != nil {
		return "", false
	}
	_ = width
	return strings.TrimRight(buf.String(), "\n"), true
}
