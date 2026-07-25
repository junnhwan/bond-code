package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const coordinatorAgentID = "__coordinator__"

// handleFocusKey drives the Agent switcher/window focus model. Ctrl+↑ enters
// the switcher; Up/Down select the coordinator or a child, Enter opens its
// transcript, and Esc steps back. It must run before the main key switch so these
// keys are not swallowed by the textarea (which owns arrows in composer focus).
func (m Model) handleFocusKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	key := msg.String()
	switch m.focus {
	case FocusAgentBar:
		switch key {
		case "esc", "ctrl+up":
			m.focus = FocusComposer
			return m, nil, true
		case "left", "h", "up", "k":
			m.agentBarSelected = m.moveAgentInBar(-1)
			return m, nil, true
		case "right", "l", "down", "j":
			m.agentBarSelected = m.moveAgentInBar(1)
			return m, nil, true
		case "enter":
			if m.agentBarSelected == coordinatorAgentID {
				m.focus = FocusComposer
			} else if m.agentBarSelected != "" {
				m = m.enterAgentWindow(m.agentBarSelected)
			}
			return m, nil, true
		case "x":
			if m.agentBarSelected != "" && m.agentBarSelected != coordinatorAgentID {
				m = m.cancelSelectedSubagent(m.agentBarSelected)
			}
			return m, nil, true
		}
	case FocusAgentWindow:
		if key == "esc" {
			m = m.exitAgentWindow()
			return m, nil, true
		}
		if key == "x" && m.focusedTaskID != "" {
			m = m.cancelSelectedSubagent(m.focusedTaskID)
			return m, nil, true
		}
		if m.cfg.SendSubagentInput != nil {
			if key == "enter" {
				return m.submitAgentInput()
			}
			return m, nil, false
		}
	default: // composer
		if key == "ctrl+up" {
			agents := m.conversationAgents()
			if len(agents) > 0 {
				m.focus = FocusAgentBar
				m.agentBarSelected = agents[len(agents)-1]
			}
			return m, nil, true
		}
	}
	// Scrollback focus owns navigation keys; Tab/Space/letters/fold keys must
	// reach the main router for Simple-mode focus + selection (not swallowed).
	if m.focus == FocusScrollback {
		switch msg.String() {
		case "tab", " ", "enter", "up", "down", "left", "right",
			"esc", "ctrl+u", "ctrl+d", "pgup", "pgdown":
			return m, nil, false
		}
		// Printable runes: letter auto-focus + insert (Grok Simple mode).
		if len(msg.Runes) > 0 {
			return m, nil, false
		}
	}
	// Agent window only reuses the composer when child input is wired.
	// Without SendSubagentInput, swallow editing keys so the parent draft is safe.
	if m.focus == FocusAgentWindow && m.cfg.SendSubagentInput != nil {
		return m, nil, false
	}
	if m.focus != FocusComposer && m.shouldConsumeComposerInputKey(msg) {
		return m, nil, true
	}
	return m, nil, false
}

// cancelSelectedSubagent asks the runtime to interrupt one child agent by task
// ID (best-effort). The child's synchronous RunTask returns a cancelled result
// on its next iteration check, which flows back through EventSubagentFailed
// and updates the trace/window automatically — so this only fires the cancel
// and does not mutate local trace state. No-op when CancelSubagent is unset.
func (m Model) cancelSelectedSubagent(taskID string) Model {
	if m.cfg.CancelSubagent == nil {
		return m
	}
	m.cfg.CancelSubagent(taskID)
	return m
}

func (m Model) shouldConsumeComposerInputKey(msg tea.KeyMsg) bool {
	if len(msg.Runes) > 0 {
		return true
	}
	switch msg.String() {
	case "enter", "tab", "shift+tab", "backspace", "delete", "alt+enter", "alt+m":
		return true
	case "ctrl+c":
		return !m.agent.Busy && m.inputValue() != ""
	case "ctrl+d", "ctrl+h":
		return m.inputValue() != ""
	default:
		return false
	}
}

func (m Model) handleLeaderKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	key := strings.ToLower(strings.TrimSpace(msg.String()))
	if key == "ctrl+x" {
		return m, nil
	}
	m.leaderPending = false
	m.whichKeyVisible = false
	switch key {
	case "esc", "ctrl+c":
		return m, nil
	case "q":
		if m.agent.Busy {
			m = m.cancelRunningAgent()
		}
		return m, tea.Quit
	case "t":
		// Thinking is the highest-value density toggle, so it gets a direct
		// leader binding; timestamps and tool-details live in the palette.
		return m.toggleThinking(), nil
	case "p":
		// Park the current draft, or pop a stashed one when the composer is empty.
		return m.stashLeaderAction(), nil
	case "e":
		// Hand the draft to $EDITOR; tea.ExecProcess pauses the TUI while it runs.
		return m.openExternalEditor()
	case "d":
		// Full-screen change review: what the agent wrote/edited this session,
		// with a git working-tree source toggle. Kept out of the empty-input
		// gate so it works even with a draft in the composer.
		return m.openDiffViewer(), nil
	case "g":
		if strings.TrimSpace(m.inputValue()) == "" && m.cfg.SessionHistory != nil {
			return m.enterHistory(), nil
		}
		return m, nil
	case "n":
		if strings.TrimSpace(m.inputValue()) == "" {
			return m.runCommand(m.cfg.Context, "/new")
		}
		return m, nil
	case "c":
		return m.runCommand(m.cfg.Context, "/compact")
	case "l":
		// Prefer the rich session-manager overlay when wired; fall back to
		// /resume, which itself opens the overlay in the TUI or prints the
		// text list in headless / test models.
		if m.cfg.SessionManager != nil {
			return m.openSessionManager(), nil
		}
		return m.runCommand(m.cfg.Context, "/resume")
	case "s":
		return m.runCommand(m.cfg.Context, "/status")
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// <leader>N quick-switches to the Nth session by pin-then-recency.
		return m.quickSwitchSession(int(key[0] - '0'))
	default:
		return m, nil
	}
}

func (m Model) openHelpShortcut() (Model, tea.Cmd) {
	if m.composer.Suggestions != nil {
		m.composer.Suggestions.Hide()
	}
	if m.agent.Busy {
		return m.runCommand(m.cfg.Context, "/help")
	}
	m = m.clearInput()
	m = m.beginUserTurn("/help")
	return m.runCommand(m.cfg.Context, "/help")
}

// conversationAgents returns every available subagent task ID in canonical
// switcher order, including trace-only children that have not committed a
// timeline status block yet.
func (m Model) conversationAgents() []string {
	return m.availableAgentIDs()
}

// moveAgentInBar shifts the bar selection by dir (+1/-1), wrapping and falling
// back to the latest agent when the current selection is stale (e.g. the turn
// advanced and the old taskID is gone).
func (m Model) moveAgentInBar(dir int) string {
	agents := append([]string{coordinatorAgentID}, m.conversationAgents()...)
	if len(agents) == 1 {
		return ""
	}
	idx := -1
	for i, id := range agents {
		if id == m.agentBarSelected {
			idx = i
			break
		}
	}
	if idx < 0 {
		return agents[len(agents)-1]
	}
	idx += dir
	if idx < 0 {
		idx = len(agents) - 1
	}
	if idx >= len(agents) {
		idx = 0
	}
	return agents[idx]
}

func (m Model) toggleLatestToolBlock() Model {
	for turnIdx := len(m.timeline.Turns) - 1; turnIdx >= 0; turnIdx-- {
		turn := m.timeline.Turns[turnIdx]
		for blockIdx := len(turn.Blocks) - 1; blockIdx >= 0; blockIdx-- {
			tool := turn.Blocks[blockIdx].Tool
			if tool == nil || !shouldCollapseToolOutput(tool.Output) {
				continue
			}
			m.timeline = m.timeline.setToolCollapsed(turnIdx, blockIdx, !tool.Collapsed)
			return m.clampScroll(m.currentLayout())
		}
	}
	return m
}

func (m Model) enterAgentWindow(taskID string) Model {
	m.coordinatorDraft = m.inputValue()
	m.focus = FocusAgentWindow
	m.focusedTaskID = taskID
	m.scroll = 0
	if trace := m.subagentTraces[taskID]; trace != nil {
		trace.Unread = false
		m.composer = m.composer.setValue(trace.Draft)
	} else {
		m.composer = m.composer.setValue("")
	}
	return m
}
func (m Model) exitAgentWindow() Model {
	if trace := m.subagentTraces[m.focusedTaskID]; trace != nil {
		trace.Draft = m.inputValue()
	}
	m.composer = m.composer.setValue(m.coordinatorDraft)
	m.coordinatorDraft = ""
	m.focus = FocusAgentBar
	m.scroll = 0
	m.scrollPaused = false
	return m.clearNewOutputBelow()
}
func (m Model) submitAgentInput() (Model, tea.Cmd, bool) {
	input := strings.TrimSpace(m.inputValue())
	if input == "" || m.focusedTaskID == "" {
		return m, nil, true
	}
	taskID := m.focusedTaskID
	trace := m.subagentTraces[taskID]
	if trace == nil {
		trace = &AgentTrace{TaskID: taskID, Status: "running"}
		m = m.setSubagentTrace(taskID, trace)
	}
	blockID := trace.nextBlockID()
	trace.Blocks = append(trace.Blocks, Block{ID: blockID, Kind: BlockCommand, Title: "you", Body: input})
	trace.Draft = ""
	m.composer = m.composer.setValue("")
	send := m.cfg.SendSubagentInput
	return m, func() tea.Msg {
		return subagentInputResultMsg{taskID: taskID, blockID: blockID, input: input, err: send(taskID, input)}
	}, true
}
