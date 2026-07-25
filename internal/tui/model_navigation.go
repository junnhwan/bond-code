package tui

import (
	"strings"
)

func (m Model) followBottom() Model {
	m.scroll = 0
	m.scrollPaused = false
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

// pushSessionHistory appends id to the visited-session stack, truncating any
// forward entries. A repeat of the current id is a no-op.
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
