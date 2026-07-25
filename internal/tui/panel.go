package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/junnhwan/bond-code/internal/command"
)

// renderPanel renders a command.Panel as a titled, sectioned, bordered box.
// Key columns are aligned within the panel; each row's State tints its value.
// Width adapts to the viewport but is clamped so very wide terminals do not
// stretch the box and narrow ones keep a readable floor.
func renderPanel(p *command.Panel, width int) string {
	if p == nil {
		return ""
	}
	inner := width - 4 // border (2) + horizontal padding (2)
	if inner < 40 {
		inner = 40
	}
	if inner > 80 {
		inner = 80
	}

	keyWidth := 0
	for _, sec := range p.Sections {
		for _, row := range sec.Rows {
			if len(row.Key) > keyWidth {
				keyWidth = len(row.Key)
			}
		}
	}
	keyWidth += 2

	labelStyle := lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted).Width(keyWidth)
	valueStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Text)

	var b strings.Builder
	b.WriteString(accentStyle.Render("● " + p.Title))
	b.WriteString("\n\n")
	for si, sec := range p.Sections {
		if si > 0 {
			b.WriteString("\n")
		}
		if sec.Label != "" {
			b.WriteString(sectionLabel(sec.Label))
			b.WriteString("\n")
		}
		// Optional leading visual rows (e.g. context breakdown bar) with no key.
		for _, row := range sec.Rows {
			if strings.TrimSpace(row.Key) == "" {
				if v := strings.TrimSpace(row.Value); v != "" {
					b.WriteString(v)
					b.WriteString("\n")
				}
				continue
			}
			label := labelStyle.Render(row.Key)
			value := panelValue(row.Value, row.State, valueStyle)
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Left, label, value))
			b.WriteString("\n")
		}
	}
	body := strings.TrimRight(b.String(), "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(DefaultTheme.Border).
		Padding(0, 1).
		Width(inner).
		Render(body)
}

func panelValue(value, state string, base lipgloss.Style) string {
	switch state {
	case "ok":
		return successStyle.Render(value)
	case "warn":
		return warningStyle.Render(value)
	case "error":
		return errorStyle.Render(value)
	default:
		return base.Render(value)
	}
}
