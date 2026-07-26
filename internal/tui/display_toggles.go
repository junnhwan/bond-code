package tui

// Display-density toggles (Phase 2.3). Independent, persisted switches that
// trade information density against clutter on long sessions.
//
//   - showThinking     — Claude Code-aligned: default OFF hides *committed*
//                        thinking in scrollback. Live thinking is a single
//                        fixed dock line ("thinking · …") so scrollback does
//                        not jitter; On (Ctrl+T) paints full thinking text
//                        (history + multi-line live). Ctrl+O does not flip this.
//   - showTimestamps   — annotate each turn with its start/end wall-clock times.
//                        Default off; useful when reviewing a replay, noisy live.
//   - showToolDetails  — render completed tool-activity lines. Default ON so
//                        existing behavior is unchanged; turning it off collapses
//                        a long session's wall of green tool lines to just the
//                        running/failed ones still in flight.

func (m Model) toggleThinking() Model {
	m.showThinking = !m.showThinking
	m.invalidateMarkdownCache()
	m = m.persistPreferences()
	return m.pushToast(toggleLabel("thinking blocks", m.showThinking), toastInfo)
}

func (m Model) toggleTimestamps() Model {
	m.showTimestamps = !m.showTimestamps
	m.invalidateMarkdownCache()
	m = m.persistPreferences()
	return m.pushToast(toggleLabel("timestamps", m.showTimestamps), toastInfo)
}

func (m Model) toggleToolDetails() Model {
	m.showToolDetails = !m.showToolDetails
	m.invalidateMarkdownCache()
	m = m.persistPreferences()
	return m.pushToast(toggleLabel("completed tool details", m.showToolDetails), toastInfo)
}

// toggleScrollbar flips the transcript scrollbar visibility. Default off; on,
// it adds a right-edge track + thumb for a visual sense of scroll position.
func (m Model) toggleScrollbar() Model {
	m.showScrollbar = !m.showScrollbar
	m = m.persistPreferences()
	return m.pushToast(toggleLabel("scrollbar", m.showScrollbar), toastInfo)
}

func toggleLabel(name string, on bool) string {
	if on {
		return "showing " + name
	}
	return "hiding " + name
}
