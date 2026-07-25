package tui

import (
	"fmt"
	"strings"
)

func (m Model) renderWelcomeScreenWidth(height, width int) string {
	active := m.welcomeMenuActive
	if m.hover.kind == mouseHitWelcomeMenu {
		active = m.hover.index
	}
	return RenderWelcomeChrome(WelcomeChromeInput{
		Width:      width,
		Height:     height,
		Project:    m.cfg.Status.ProjectRoot,
		Branch:     m.cfg.Status.GitBranch,
		Version:    "v1.0.0",
		Model:      m.cfg.Status.Model,
		ShowPrompt: false, // live ❯ lives in the dock composer
		ActiveMenu: active,
		AnimFrame:  m.animFrame,
	})
}

func shortPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "."
	}
	if path == "." {
		return path
	}
	project := projectName(path)
	if project == "." || project == "" {
		return path
	}
	return project
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (m Model) renderSuggestions() string {
	return m.renderSuggestionsForWidth(m.width)
}

func (m Model) renderSuggestionsForWidth(width int) string {
	return m.renderSuggestionsForWidthHeight(width, 0)
}

func (m Model) renderSuggestionsForWidthHeight(width int, maxHeight int) string {
	if m.composer.Suggestions == nil || !m.composer.Suggestions.IsVisible() {
		return ""
	}
	if width < 1 {
		width = 1
	}

	filter := m.getCommandFilter()
	visible := m.composer.Suggestions.GetVisible(filter)
	if len(visible) == 0 {
		return ""
	}

	maxVisible := 5
	if maxHeight > 0 && maxHeight < maxVisible {
		maxVisible = maxHeight
	}
	if maxVisible < 1 {
		return ""
	}
	selectedIdx := m.composer.Suggestions.GetSelectedIndex()
	windowStart := 0
	if len(visible) > maxVisible {
		if selectedIdx < 0 {
			selectedIdx = 0
		}
		if selectedIdx >= len(visible) {
			selectedIdx = len(visible) - 1
		}
		windowStart = selectedIdx - maxVisible + 1
		if windowStart < 0 {
			windowStart = 0
		}
		if maxStart := len(visible) - maxVisible; windowStart > maxStart {
			windowStart = maxStart
		}
		visible = visible[windowStart : windowStart+maxVisible]
	}

	var lines []string
	for i, sug := range visible {
		icon := "  "
		style := suggestionStyle

		if windowStart+i == selectedIdx {
			icon = "▶ "
			style = suggestionSelectedStyle
		}

		prefix := sug.Prefix
		if prefix == "" {
			prefix = m.composer.Suggestions.CurrentPrefix()
		}
		line := fmt.Sprintf("%s%-14s", icon, prefix+sug.Name)
		if sug.Description != "" {
			line = line + " " + sug.Description
		}
		lines = append(lines, style.Render(truncatePlain(line, width)))
	}
	return strings.Join(lines, "\n")
}
