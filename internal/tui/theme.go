package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds the semantic color roles used across the TUI. Values are
// truecolor (#RRGGBB) so the palette stays consistent across terminals; Lip
// Gloss downgrades to the terminal's actual color profile at render time.
//
// Roles map to Grok Build's GrokNight theme (xai-grok-pager-render/theme/groknight.rs):
// neutral gray base + TokyoNight accents — not a purple-tinted "violet night".
type Theme struct {
	Text            lipgloss.Color // primary body text (#e1e1e1)
	TextMuted       lipgloss.Color // secondary info (#c8c8c8)
	Dim             lipgloss.Color // footer, muted chrome (#6c6c6c)
	GrayBright      lipgloss.Color // bright gray for keys (#787878)
	Border          lipgloss.Color // borders / rules (#242424)
	Accent          lipgloss.Color // assistant / running magenta (#bb9af7)
	Success         lipgloss.Color // done / passed (#9ece6a)
	Warning         lipgloss.Color // pending / caution (#e0af68)
	Error           lipgloss.Color // failure (#f7768e)
	Tool            lipgloss.Color // tool call labels (#787878)
	Path            lipgloss.Color // file paths (#ff9e64)
	Command         lipgloss.Color // shell command fragments (#e0af68)
	Running         lipgloss.Color // live cyan cue (#7dcfff)
	BackgroundPanel lipgloss.Color // panel / terminal bg (#0a0a0a)
	BackgroundBase  lipgloss.Color // main surface (#141414)
	Selection       lipgloss.Color // selection / hover bg (#242424)
	Hover           lipgloss.Color // stronger hover (#2c2c2c)
	PromptBorder    lipgloss.Color // idle prompt rule (#323237)
	PromptActive    lipgloss.Color // focused prompt rule (#505058)
}

// DefaultTheme is the real GrokNight palette (neutral gray + TokyoNight accents).
// ApplyAccent mutates only Accent so /theme can recolor brand without a full swap.
var DefaultTheme = Theme{
	Text:            lipgloss.Color("#e1e1e1"),
	TextMuted:       lipgloss.Color("#c8c8c8"),
	Dim:             lipgloss.Color("#6c6c6c"),
	GrayBright:      lipgloss.Color("#787878"),
	Border:          lipgloss.Color("#242424"),
	Accent:          lipgloss.Color("#bb9af7"),
	Success:         lipgloss.Color("#9ece6a"),
	Warning:         lipgloss.Color("#e0af68"),
	Error:           lipgloss.Color("#f7768e"),
	Tool:            lipgloss.Color("#787878"),
	Path:            lipgloss.Color("#ff9e64"),
	Command:         lipgloss.Color("#e0af68"),
	Running:         lipgloss.Color("#7dcfff"),
	BackgroundPanel: lipgloss.Color("#0a0a0a"),
	BackgroundBase:  lipgloss.Color("#141414"),
	Selection:       lipgloss.Color("#242424"),
	Hover:           lipgloss.Color("#2c2c2c"),
	PromptBorder:    lipgloss.Color("#323237"),
	PromptActive:    lipgloss.Color("#505058"),
}

// AccentPreset is a named swap-in for the Accent color role.
type AccentPreset struct {
	Name  string
	Color lipgloss.Color
}

// AccentPresets are the user-selectable accent colors surfaced by /theme.
// "magenta" is GrokNight default (#bb9af7).
var AccentPresets = []AccentPreset{
	{Name: "magenta", Color: lipgloss.Color("#bb9af7")},
	{Name: "peach", Color: lipgloss.Color("#FAB283")},
	{Name: "blue", Color: lipgloss.Color("#7aa2f7")},
	{Name: "green", Color: lipgloss.Color("#9ece6a")},
	{Name: "amber", Color: lipgloss.Color("#e0af68")},
	{Name: "cyan", Color: lipgloss.Color("#7dcfff")},
}

const defaultAccentName = "magenta"

// DefaultAccentName is the preset used when none (or an unknown one) is
// configured.
func DefaultAccentName() string { return defaultAccentName }

// LookupAccentPreset returns the preset with the given name (case-insensitive),
// or nil when unknown.
func LookupAccentPreset(name string) *AccentPreset {
	name = strings.ToLower(strings.TrimSpace(name))
	for i := range AccentPresets {
		if AccentPresets[i].Name == name {
			return &AccentPresets[i]
		}
	}
	return nil
}

// ResolveAccentColor returns the color for a preset name, falling back to the
// default magenta when the name is empty or unrecognized.
func ResolveAccentColor(name string) lipgloss.Color {
	if p := LookupAccentPreset(name); p != nil {
		return p.Color
	}
	return AccentPresets[0].Color
}

// ApplyAccent recolors the Accent role and rebuilds every accent-derived style
// so the change is visible on the next render without a restart.
func ApplyAccent(color lipgloss.Color) {
	DefaultTheme.Accent = color
	rebuildAccentStyles()
}

// rebuildAccentStyles reassigns the package-level styles whose color is the
// Accent role. Lipgloss captures the color at Foreground() call time, so simply
// mutating DefaultTheme is not enough — the styles must be rebuilt.
func rebuildAccentStyles() {
	accentStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Accent).Bold(true)
	commandStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Command).Bold(true)
	breakdownSystemStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Accent)
	pathStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Path)
	// Keep neutral chrome styles in sync when theme package mutates.
	assistantStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Text)
	userStyle = lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted)
	toolStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Tool)
	dimStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Dim)
	busyStyle = lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted).Italic(true)
	suggestionStyle = lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted)
	suggestionSelectedStyle = lipgloss.NewStyle().
		Foreground(DefaultTheme.Text).
		Background(DefaultTheme.Selection).
		Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Success)
	warningStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Warning)
	errorStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Error).Bold(true)
	confirmStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Warning).Bold(true)
}
