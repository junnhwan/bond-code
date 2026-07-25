package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// paintGrokNightSurface fills the composed terminal canvas with a uniform
// GrokNight near-black background (#0a0a0a). Gray is for text/accents only —
// never a second fill color on the trailing half of each line.
//
// bodyRows is accepted for call-site compatibility but ignored: splitting body
// (#141414) vs chrome (#0a0a0a) made "text then gray pad" stripes worse.
func paintGrokNightSurface(view string, width, height, bodyRows int) string {
	_ = bodyRows
	if width < 1 {
		width = 1
	}
	if height < 1 {
		return ""
	}
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	// Single surface color: pure GrokNight terminal black.
	bg := DefaultTheme.BackgroundPanel
	out := make([]string, height)
	for i, line := range lines {
		out[i] = paintSurfaceLine(line, width, bg)
	}
	return strings.Join(out, "\n")
}

// paintSurfaceLine forces a continuous background across the full row.
//
// lipgloss Width+Background.Render(line) only keeps the bg until the first
// embedded SGR reset in styled text, then paints the trailing pad — which
// looks like "glyphs on default, gray/black tail". We reinject the surface
// background after every reset and pad the remainder with the same bg.
func paintSurfaceLine(line string, width int, bg lipgloss.Color) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	open, close := backgroundOpenClose(bg)
	if open == "" {
		// Profile emitted no sequence; still pad for geometry.
		pad := width - lipgloss.Width(line)
		if pad > 0 {
			return line + strings.Repeat(" ", pad)
		}
		return line
	}
	visual := lipgloss.Width(line)
	painted := reinjectBackground(line, open)
	pad := width - visual
	if pad < 0 {
		pad = 0
	}
	var b strings.Builder
	b.Grow(len(open) + len(painted) + pad + len(close) + 8)
	b.WriteString(open)
	b.WriteString(painted)
	if pad > 0 {
		// Pad while bg is active (open already reinjected through content).
		b.WriteString(strings.Repeat(" ", pad))
	}
	b.WriteString(close)
	return b.String()
}

// backgroundOpenClose extracts the SGR open/close pair lipgloss uses for bg.
func backgroundOpenClose(bg lipgloss.Color) (open, close string) {
	// Render a single marker so we can split the style wrapper from content.
	const marker = "\u0001"
	wrapped := lipgloss.NewStyle().Background(bg).Render(marker)
	if i := strings.Index(wrapped, marker); i >= 0 {
		open = wrapped[:i]
		close = wrapped[i+len(marker):]
		return open, close
	}
	return "", ""
}

// reinjectBackground re-applies open after every full SGR reset so later
// glyphs keep the surface bg (styled segments often emit \x1b[0m).
func reinjectBackground(s, open string) string {
	if s == "" || open == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(open)*4)
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) {
				c := s[j]
				j++
				if c >= 0x40 && c <= 0x7e {
					break
				}
			}
			seq := s[i:j]
			b.WriteString(seq)
			// Full reset (CSI 0 m / CSI m) clears background — put it back.
			if j > i && s[j-1] == 'm' && isResetSGRParams(s[i+2:j-1]) {
				b.WriteString(open)
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// isResetSGRParams reports CSI parameters that fully reset SGR (empty or 0).
func isResetSGRParams(params string) bool {
	if params == "" || params == "0" {
		return true
	}
	// Rare forms like "0;0"
	for _, p := range strings.Split(params, ";") {
		if p != "" && p != "0" {
			return false
		}
	}
	return true
}

// formatThemePanel builds the structured /theme listing: dark row chrome,
// accent swatches, and an active marker — not a flat "accents: a, b, c" dump.
func formatThemePanel(active string) string {
	active = strings.ToLower(strings.TrimSpace(active))
	if active == "" {
		active = DefaultAccentName()
	}
	// Panel chrome stays on pure black; active row uses Selection gray only
	// as a deliberate accent, not as the default surface fill.
	title := lipgloss.NewStyle().
		Foreground(DefaultTheme.Text).
		Background(DefaultTheme.BackgroundPanel).
		Bold(true).
		Render(" Theme · GrokNight ")
	hint := dimStyle.Render("usage: /theme <name>")
	rows := make([]string, 0, len(AccentPresets)+2)
	rows = append(rows, title)
	for _, p := range AccentPresets {
		swatch := lipgloss.NewStyle().
			Foreground(p.Color).
			Background(DefaultTheme.BackgroundPanel).
			Render("●")
		name := p.Name
		if p.Name == active {
			label := " ▸ " + swatch + "  " + name + "  · active "
			rows = append(rows, lipgloss.NewStyle().
				Foreground(DefaultTheme.Text).
				Background(DefaultTheme.Selection).
				Bold(true).
				Render(label))
			continue
		}
		label := "   " + swatch + "  " + name + " "
		rows = append(rows, lipgloss.NewStyle().
			Foreground(DefaultTheme.TextMuted).
			Background(DefaultTheme.BackgroundPanel).
			Render(label))
	}
	rows = append(rows, hint)
	return strings.Join(rows, "\n")
}
