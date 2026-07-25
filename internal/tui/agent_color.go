package tui

import "github.com/charmbracelet/lipgloss"

// agentPalette is the deterministic color pool for child agents (research /
// coder / reviewer / orchestrator nodes). Each agent type hashes to a stable
// color so a turn with several children reads as distinct entries at a glance
// (mirrors opencode's agentColor hash). The peach accent is intentionally
// absent so it stays reserved for the main agent.
var agentPalette = []lipgloss.Color{
	DefaultTheme.Tool,
	DefaultTheme.Success,
	DefaultTheme.Warning,
	lipgloss.Color("#C792EA"), // purple
	lipgloss.Color("#56B6C2"), // cyan
	lipgloss.Color("#E5C07B"), // gold
	DefaultTheme.Error,
}

// agentColor returns a stable palette color for an agent-type name. The same
// name always maps to the same color across renders, so a child agent keeps its
// color for the whole turn.
func agentColor(name string) lipgloss.Color {
	if len(agentPalette) == 0 {
		return DefaultTheme.Accent
	}
	h := uint32(0)
	for _, c := range name {
		h = h*31 + uint32(c)
	}
	return agentPalette[int(h)%len(agentPalette)]
}

// agentStyle returns a foreground-only style colored by agent type.
func agentStyle(name string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(agentColor(name))
}
