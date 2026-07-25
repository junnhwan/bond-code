package tui

import tea "github.com/charmbracelet/bubbletea"

// applyComposerFocus keeps the bubbles textarea Focused state aligned with
// Model.focus. Grok Simple mode: when scrollback owns focus the prompt is
// Blur'd (no blink). Space/Tab/click focus without inserting; a printable
// letter auto-focuses the prompt and inserts that character (see update.go).
//
// Returns a Blink cmd only on a true unfocused→focused transition so ordinary
// key handlers do not spam non-nil cmds (tests and submit paths rely on that).
func (m Model) applyComposerFocus() (Model, tea.Cmd) {
	// Prompt owns the cursor for the main composer, and for agent-window drafts
	// only when child input is wired. Scrollback / bar stay Blur'd so the cursor
	// does not blink when typing is not live.
	wantFocus := m.focus == FocusComposer ||
		(m.focus == FocusAgentWindow && m.cfg.SendSubagentInput != nil)
	// Permission / question / reverse-search takeover: prompt is not the owner.
	if m.agent.Pending != nil || m.question != nil || m.search.Active {
		wantFocus = false
	}
	if wantFocus {
		if !m.composer.Input.Focused() {
			return m, m.composer.Input.Focus()
		}
		return m, nil
	}
	if m.composer.Input.Focused() {
		m.composer.Input.Blur()
	}
	return m, nil
}

// withFocus sets Model.focus and syncs the textarea cursor immediately.
func (m Model) withFocus(f Focus) (Model, tea.Cmd) {
	m.focus = f
	return m.applyComposerFocus()
}
