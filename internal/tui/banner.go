package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Banner uses resting gray; shimmer recolors per-glyph toward text primary
	// (matches Grok logo sheen language without copying braille assets).
	bannerStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Dim).
			Bold(true).
			Align(lipgloss.Center)

	bannerDimStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Dim).
			Align(lipgloss.Center)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Dim).
			Align(lipgloss.Center)

	versionStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Dim).
			Align(lipgloss.Center)

	// Grok menu: bold primary labels; selected row uses bg_highlight.
	welcomeMenuStyle = lipgloss.NewStyle().
				Foreground(DefaultTheme.Text).
				Bold(true)

	welcomeMenuActiveStyle = lipgloss.NewStyle().
				Foreground(DefaultTheme.Text).
				Background(DefaultTheme.Selection).
				Bold(true)

	welcomeMenuKeyStyle = lipgloss.NewStyle().
				Foreground(DefaultTheme.GrayBright)

	welcomeMenuKeyActiveStyle = lipgloss.NewStyle().
					Foreground(DefaultTheme.GrayBright).
					Background(DefaultTheme.Selection)
)

// Product identity under the icon mark (icon = logo; these are captions).
const bondProductName = "Bond Code"
const bondSlogan = "terminal coding agent"

// RenderBanner renders the Bond welcome brand: linked-ring icon + caption.
func RenderBanner(width int, version string) string {
	var parts []string
	parts = append(parts, renderBondWordmark(width)...)
	if version == "" {
		version = "v1.0.0"
	}
	parts = append(parts, versionStyle.Width(max(1, width)).Render(version))
	parts = append(parts, "")
	return strings.Join(parts, "\n")
}

// renderBondWordmark returns brand lines for the given width.
// frame drives a Grok-style sheen across the braille mark (0 = static).
func renderBondWordmark(width int) []string {
	return renderBondWordmarkFrame(width, 0)
}

func renderBondWordmarkFrame(width, frame int) []string {
	w := max(1, width)
	center := func(s string) string {
		plainW := lipgloss.Width(s)
		if plainW >= w {
			return s
		}
		pad := (w - plainW) / 2
		return strings.Repeat(" ", pad) + s
	}

	// Icon first: two linked rings in braille (the Bond mark).
	logo := bondLogoForWidth(w)
	lines := strings.Split(logo, "\n")
	// Drop trailing empty / all-blank braille rows for tighter vertical rhythm.
	for len(lines) > 0 {
		last := strings.TrimRight(lines[len(lines)-1], " \t\u2800")
		if last != "" {
			break
		}
		lines = lines[:len(lines)-1]
	}
	// No extra rule above the mark: welcome chrome already draws one under the
	// location bar; a second hairline on the brand just feels redundant.
	out := make([]string, 0, len(lines)+3)
	for i, line := range lines {
		out = append(out, center(shimmerLine(line, i, len(lines), frame)))
	}
	// Name the product under the mark so welcome is still clearly Bond Code.
	// Icon remains the logo; text is identity caption, not the mark itself.
	if w >= 12 {
		out = append(out, center(shimmerWord(bondProductName, len(lines), len(lines)+1, frame)))
	}
	if w >= 20 {
		out = append(out, subtitleStyle.Width(w).Render(bondSlogan))
	}
	return out
}

// RenderWelcomeMenu lists New / Resume / Status (label left, key right).
func RenderWelcomeMenu(width int) string {
	return renderWelcomeMenuColumn(width, 0)
}

// RenderWelcomeTopBar shows project left, version right.
func RenderWelcomeTopBar(width int, projectRoot, version string) string {
	return renderWelcomeLocationBar(width, projectRoot, "", version)
}

// RenderWelcomeTopBarWithBranch includes git branch.
func RenderWelcomeTopBarWithBranch(width int, projectRoot, branch, version string) string {
	return renderWelcomeLocationBar(width, projectRoot, branch, version)
}

func normalizeBannerBlock(value string) string {
	rawLines := strings.Split(strings.Trim(value, "\n"), "\n")
	lines := make([]string, 0, len(rawLines))
	maxWidth := 0
	for _, line := range rawLines {
		line = strings.TrimRight(line, " ")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
		if width := lipgloss.Width(line); width > maxWidth {
			maxWidth = width
		}
	}
	for i, line := range lines {
		pad := maxWidth - lipgloss.Width(line)
		if pad > 0 {
			lines[i] = line + strings.Repeat(" ", pad)
		}
	}
	return strings.Join(lines, "\n")
}

// RenderWelcomeMessage is short discoverability copy under the menu.
// Only incremental hints: how to start, full command list via /help, and @path.
// Resume/status live in the menu above — do not re-list them here.
func RenderWelcomeMessage(width int) string {
	helpLines := []string{
		"Press enter to chat \u00b7 type /help for all commands",
		"Attach context with @path or @path:12-40",
	}
	style := lipgloss.NewStyle().
		Foreground(DefaultTheme.Dim).
		Align(lipgloss.Center).
		Width(max(1, width))
	var lines []string
	for _, line := range helpLines {
		lines = append(lines, style.Render(line))
	}
	return strings.Join(lines, "\n")
}
