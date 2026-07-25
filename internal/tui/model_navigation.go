package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) followBottom() Model {
	m.scroll = 0
	m.scrollPaused = false
	// Dropping to the bottom ends turn navigation: a subsequent Enter submits
	// the composer instead of re-opening the message menu.
	m.navTurnIdx = -1
	m = m.clearNewOutputBelow()
	return m
}

func (m Model) markNewOutputBelow() Model {
	if m.scrollPaused && m.scroll > 0 {
		m.newOutputBelow = true
		m.newOutputCount++
	}
	return m
}

func (m Model) markNewOutputNotice(notice string) Model {
	if m.scrollPaused && m.scroll > 0 && strings.TrimSpace(notice) != "" {
		m.newOutputBelow = true
		m.newOutputNotice = strings.TrimSpace(notice)
	}
	return m
}

func (m Model) clearNewOutputBelow() Model {
	m.newOutputBelow = false
	m.newOutputCount = 0
	m.newOutputNotice = ""
	return m
}

func (m Model) resetSessionViewState() Model {
	m.agent.LiveStream = nil
	m.agent.LiveDetail = ""
	m.agent.TerminalHandled = false
	m.agent.RunGeneration++
	m.focus = FocusComposer
	m.focusedTaskID = ""
	m.agentBarSelected = ""
	m = m.resetSubagentTraces()
	return m
}

func (m Model) setActiveSessionID(sessionID string) Model {
	m.cfg.Status.SessionID = sessionID
	m.cfg.CommandEnv.SessionID = sessionID
	m.live.SessionID = defaultString(sessionID, "local")
	return m
}

func (m Model) scrollBy(delta int) Model {
	m.scroll += delta
	m = m.clampScroll(m.currentLayout())
	if m.scroll > 0 {
		m.scrollPaused = true
		return m
	}
	m.scrollPaused = false
	m = m.clearNewOutputBelow()
	return m
}

// navigateTurn jumps the viewport to the previous (delta<0) or next (delta>0)
// user turn so a long session can be walked prompt-by-prompt (alt+ctrl+p/n).
// scroll=0 is the bottom (newest); pinning the target turn's first line at the
// viewport top means scroll = maxScroll - target. navTurnIdx remembers the last
// jump so repeated presses walk through turns; -1 means "not navigating yet".
func (m Model) navigateTurn(delta int) Model {
	layout := m.currentLayout()
	lines, starts := m.renderTimelineLines(layout.TimelineW)
	if len(starts) == 0 {
		return m
	}
	idx := m.navTurnIdx
	if idx < 0 || idx >= len(starts) {
		if delta < 0 {
			idx = len(starts) - 1 // from the bottom, "previous" = most recent turn
		} else {
			idx = 0
		}
	} else {
		idx += delta
	}
	if idx < 0 || idx >= len(starts) {
		return m
	}
	m.navTurnIdx = idx
	maxScroll := len(lines) - layout.TimelineH
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := maxScroll - starts[idx]
	if scroll < 0 {
		scroll = 0
	}
	m.scroll = scroll
	m = m.clampScroll(layout)
	m.scrollPaused = true
	return m
}

// reloadSessionView rebuilds the timeline onto newID and restores that
// session's remembered scroll offset. It assumes the app layer has already
// switched (the /resume command path calls SwitchSession before signalling),
// so it only reloads the TUI view. Pure view reload — history management is
// the caller's job.
func (m Model) reloadSessionView(newID string) Model {
	seed := m.cfg.ReloadSessionSeed(newID)
	m.timeline = SeedTimeline(seed)
	if prev, ok := m.sessionScrolls[newID]; ok {
		m.scroll = prev
	} else {
		m.scroll = 0
	}
	m = m.clampScroll(m.currentLayout())
	m.scrollPaused = m.scroll > 0
	m = m.clearNewOutputBelow()
	m.invalidateMarkdownCache()
	m = m.setActiveSessionID(newID)
	m = m.resetSessionViewState()
	return m
}

// switchSessionFull is the browser-style back/forward path: it switches the app
// onto targetID (bypassing the slash-command path) and reloads the view,
// remembering the outgoing scroll and restoring the target's.
func (m Model) switchSessionFull(targetID string) (Model, error) {
	if m.cfg.CommandEnv.SwitchSession == nil || m.cfg.ReloadSessionSeed == nil {
		return m, fmt.Errorf("session switching unavailable in this mode")
	}
	if cur := m.cfg.Status.SessionID; cur != "" {
		m.sessionScrolls[cur] = m.scroll
	}
	if err := m.cfg.CommandEnv.SwitchSession(targetID); err != nil {
		return m, err
	}
	return m.reloadSessionView(targetID), nil
}

// pushSessionHistory appends id to the back/forward stack, truncating any
// forward entries (browser semantics: a new navigation drops the forward
// history). A repeat of the current id is a no-op.
func (m Model) pushSessionHistory(id string) Model {
	if id == "" {
		return m
	}
	if m.sessionHistIdx < len(m.sessionHistory)-1 {
		m.sessionHistory = m.sessionHistory[:m.sessionHistIdx+1]
	}
	if len(m.sessionHistory) > 0 && m.sessionHistory[len(m.sessionHistory)-1] == id {
		return m
	}
	m.sessionHistory = append(m.sessionHistory, id)
	m.sessionHistIdx = len(m.sessionHistory) - 1
	return m
}

// navigateSession moves through the visited-session stack (Alt+Left/Right). It
// does not push — back/forward only walks the existing stack, preserving
// forward entries so Alt+Right after Alt+Left still reaches the session you
// left. A failed switch (busy / unavailable) leaves the index and view as-is.
func (m Model) navigateSession(delta int) (Model, tea.Cmd) {
	newIdx := m.sessionHistIdx + delta
	if newIdx < 0 || newIdx >= len(m.sessionHistory) {
		return m, nil
	}
	target := m.sessionHistory[newIdx]
	if target == m.cfg.Status.SessionID {
		m.sessionHistIdx = newIdx
		return m, nil
	}
	next, err := m.switchSessionFull(target)
	if err != nil {
		// Surface the failure instead of swallowing it: Alt+←/→ otherwise looks
		// dead when switching is unavailable (headless config) or the session
		// store rejects the target.
		return m.pushToast("could not switch session: "+err.Error(), toastWarn), nil
	}
	next.sessionHistIdx = newIdx
	return next, nil
}

func pageStep(height int) int {
	if height <= 6 {
		return 3
	}
	return height / 2
}

func isTimelineScrollKey(key string) bool {
	switch key {
	case "pgup", "pgdown", "home", "end", "ctrl+g", "alt+ctrl+b", "alt+ctrl+f", "alt+ctrl+y", "alt+ctrl+e", "alt+ctrl+u", "alt+ctrl+d", "alt+ctrl+g":
		return true
	default:
		return false
	}
}

func halfPageStep(height int) int {
	step := height / 4
	if step < 1 {
		return 1
	}
	return step
}
