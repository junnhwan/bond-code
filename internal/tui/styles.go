package tui

import "github.com/charmbracelet/lipgloss"

var (
	// composerBoxStyle is a light frame for the prompt area. The chrome shell
	// no longer uses a heavy rounded box; padding keeps the ❯ line readable.
	composerBoxStyle = lipgloss.NewStyle().
				Padding(0, 0)

	accentStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Accent).
			Bold(true)

	assistantStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Text)

	userStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.TextMuted)

	toolStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Tool)

	pathStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Path)

	confirmStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Warning).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Success)

	warningStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Warning)

	errorStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Error).
			Bold(true)

	commandStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Command).
			Bold(true)

	busyStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.TextMuted).
			Italic(true)

	suggestionStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.TextMuted)

	suggestionSelectedStyle = lipgloss.NewStyle().
				Foreground(DefaultTheme.Text).
				Background(DefaultTheme.Selection).
				Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Dim)

	diffAddStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Success)

	diffRemoveStyle = lipgloss.NewStyle().
			Foreground(DefaultTheme.Error)

	// searchHighlightStyle marks every search match inside the transcript while
	// the search dock is open. Bold + reverse so it stands out from surrounding
	// styled text regardless of the line's own colors.
	searchHighlightStyle = lipgloss.NewStyle().
				Bold(true).
				Reverse(true)
)
