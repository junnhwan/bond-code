package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/contextx"
)

// The message context menu (Phase 1.2/1.3) turns the read-only transcript into
// an interactive history: after alt+ctrl+p/n jumps the viewport to a turn,
// Enter opens a small action menu on that turn — copy the prompt, drop it back
// into the composer for editing, re-run it, or branch the session from there via
// the timeline browser.
//
// It is the second user of the generic menu overlay, after the command palette
// proved the overlay + action-closure pattern. Menu items are built per-open
// from the focused turn's state, so disabled hints (agent busy, no chat
// configured, empty prompt) reflect the live situation rather than a stale rule.

// openMessageMenuForTurn builds and opens the action menu for the turn at
// turnIdx. It is a no-op (returns the model unchanged) when the index is out of
// range, so a stale navTurnIdx after compaction or a session switch cannot crash.
func (m Model) openMessageMenuForTurn(turnIdx int) Model {
	if turnIdx < 0 || turnIdx >= len(m.timeline.Turns) {
		return m
	}
	turn := m.timeline.Turns[turnIdx]
	prompt := strings.TrimSpace(turn.User.Body)
	subtitle := truncatePlain(prompt, 80)
	if subtitle == "" {
		subtitle = "(empty prompt)"
	}
	busy := m.agent.Busy
	empty := prompt == ""
	canRerun := !busy && !empty && m.cfg.Chat != nil
	canFork := !busy && m.cfg.SessionHistory != nil && !empty

	items := []menuItem{
		{
			label:    "Copy prompt",
			shortcut: "y",
			disabled: empty,
			run: func(mm Model) (Model, tea.Cmd) {
				return mm.copyTextToClipboard(prompt), nil
			},
		},
		{
			label:    "Edit in composer",
			shortcut: "e",
			disabled: empty,
			hint:     "load the prompt back to edit",
			run: func(mm Model) (Model, tea.Cmd) {
				mm = mm.SetInput(prompt)
				mm = mm.followBottom()
				mm.navTurnIdx = -1
				return mm, nil
			},
		},
		{
			label:    "Re-run prompt",
			shortcut: "r",
			disabled: !canRerun,
			hint:     rerunHint(busy, empty, m.cfg.Chat != nil),
			run: func(mm Model) (Model, tea.Cmd) {
				mm = mm.followBottom()
				mm.navTurnIdx = -1
				mm = mm.beginUserTurn(prompt)
				agentPrompt := contextx.ExpandPathMentions(prompt, mm.cfg.Status.ProjectRoot)
				return mm, func() tea.Msg { return runAgentMsg{prompt: agentPrompt} }
			},
		},
		{
			label:    "Fork from here",
			shortcut: "f",
			disabled: !canFork,
			hint:     forkHint(busy, m.cfg.SessionHistory != nil),
			run: func(mm Model) (Model, tea.Cmd) {
				// Open the timeline browser (the proven fork-resume path) so the
				// user picks the exact branch point with full history context.
				return mm.enterHistory(), nil
			},
		},
	}
	return m.openMenu("Message actions", subtitle, items)
}

// copyTextToClipboard copies arbitrary text and surfaces the result as a toast,
// reusing the same clipboard path as /copy.
func (m Model) copyTextToClipboard(text string) Model {
	if err := copyToClipboard(text); err == nil {
		return m.pushToast("copied to clipboard", toastSuccess)
	}
	return m.pushToast("clipboard unavailable", toastWarn)
}

func rerunHint(busy, empty, hasChat bool) string {
	switch {
	case busy:
		return "agent is running"
	case empty:
		return "empty prompt"
	case !hasChat:
		return "agent not configured"
	}
	return ""
}

func forkHint(busy, hasHistory bool) string {
	switch {
	case busy:
		return "agent is running"
	case !hasHistory:
		return "no session history"
	}
	return "branch via timeline browser"
}
