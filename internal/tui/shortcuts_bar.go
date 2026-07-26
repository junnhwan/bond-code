package tui

// shortcutsBarLine builds a context-sensitive hint row for the chrome footer.
// Format follows Grok Build: `key:label` pairs separated by two spaces.
func (m Model) shortcutsBarLine(width int) string {
	return FormatShortcutsBar(m.shortcutHintItems(), width)
}

func (m Model) shortcutHintItems() []HintItem {
	if m.agent.ConfirmEnteringReject {
		return []HintItem{
			{Key: "enter", Label: "submit", Pinned: true},
			{Key: "esc", Label: "back", Pinned: true},
		}
	}
	if m.agent.Pending != nil {
		return []HintItem{
			{Key: "↑↓", Label: "select", Pinned: true},
			{Key: "enter", Label: "confirm", Pinned: true},
			{Key: "y", Label: "allow"},
			{Key: "n", Label: "reject"},
			{Key: "esc", Label: "reject"},
		}
	}
	if m.question != nil {
		return []HintItem{
			{Key: "↑↓", Label: "select", Pinned: true},
			{Key: "enter", Label: "submit", Pinned: true},
			{Key: "esc", Label: "cancel"},
		}
	}
	if m.focus == FocusAgentWindow {
		return []HintItem{{Key: "esc", Label: "back", Pinned: true}}
	}
	if m.focus == FocusAgentBar {
		return []HintItem{
			{Key: "↑↓", Label: "select", Pinned: true},
			{Key: "enter", Label: "open", Pinned: true},
			{Key: "esc", Label: "back"},
		}
	}
	if m.agent.Busy {
		return []HintItem{
			{Key: "esc", Label: "cancel", Pinned: true},
			{Key: "ctrl+c", Label: "interrupt", Pinned: true},
			{Key: "enter", Label: "queue"},
		}
	}
	if m.composer.Suggestions != nil && m.composer.Suggestions.IsVisible() {
		return []HintItem{
			{Key: "↑↓", Label: "select", Pinned: true},
			{Key: "tab", Label: "accept", Pinned: true},
			{Key: "esc", Label: "dismiss"},
		}
	}
	if m.focus == FocusScrollback {
		items := []HintItem{
			{Key: "tab", Label: "prompt", Pinned: true},
			{Key: "space", Label: "prompt"},
			{Key: "ctrl+u/d", Label: "scroll"},
			{Key: "esc", Label: "prompt"},
		}
		if m.mouseEnabled {
			items = append(items, HintItem{Key: "shift+drag", Label: "select"})
		}
		return items
	}
	// Idle prompt focus — Simple-mode-aligned defaults.
	hints := []HintItem{
		{Key: "enter", Label: "send", Pinned: true},
		// Ctrl+J is the reliable newline on Windows (Shift+Enter often missing).
		{Key: "ctrl+j", Label: "newline", Pinned: true},
		{Key: "tab", Label: "focus", Pinned: true},
	}
	if len(m.availableAgentIDs()) > 0 {
		hints = append(hints, HintItem{Key: "ctrl+↑", Label: "agents"})
	}
	if m.mouseEnabled {
		hints = append(hints, HintItem{Key: "shift+drag", Label: "select"})
		hints = append(hints, HintItem{Key: "/mouse", Label: "mouse off"})
	} else {
		hints = append(hints, HintItem{Key: "/mouse", Label: "mouse"})
	}
	hints = append(hints, HintItem{Key: "/help", Label: "commands"})
	return hints
}

// shortcutHints returns plain "key label" strings for tests that still join
// with " · ". Prefer shortcutHintItems + FormatShortcutsBar for rendering.
func (m Model) shortcutHints() []string {
	items := m.shortcutHintItems()
	out := make([]string, 0, len(items))
	for _, h := range items {
		if h.Label == "" {
			out = append(out, h.Key)
			continue
		}
		out = append(out, h.Key+" "+h.Label)
	}
	return out
}
