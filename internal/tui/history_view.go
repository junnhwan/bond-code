package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/session"
)

// historyState backs the ctrl+h session-tree browser. It is pure local view
// state: loading events and rendering the tree never touch the running agent
// (invariant 4). A fork only happens on Enter, and only while the agent is idle.
type historyState struct {
	events     []session.Event
	tree       []*session.TreeNode
	flat       []*session.TreeNode // DFS pre-order of tree, for ↑/↓ navigation
	cursor     int                 // index into flat
	sessionID  string              // the session being browsed
	returnMode Mode                // mode to restore when the modal closes
	loadErr    string
	visible    bool
}

// enterHistory loads the current session's event tree and switches into the
// modal history browser. Failures (no controller, no session, load error) are
// surfaced in-view rather than aborting entry so the user always sees why.
func (m Model) enterHistory() Model {
	controller := m.cfg.SessionHistory
	sessionID := strings.TrimSpace(m.cfg.Status.SessionID)
	hs := historyState{
		sessionID:  sessionID,
		returnMode: modeReturnTarget(m.mode),
		visible:    true,
	}
	if controller == nil || sessionID == "" {
		hs.loadErr = "no session history configured"
	} else {
		events, err := controller.LoadEvents(sessionID)
		if err != nil {
			hs.loadErr = err.Error()
		} else {
			hs.events = events
			hs.tree = session.BuildTree(events)
			hs.flat = flattenTree(hs.tree)
			// Default the cursor to the most recent node; backtracking walks up
			// the tree from here with ↑.
			if len(hs.flat) > 0 {
				hs.cursor = len(hs.flat) - 1
			}
		}
	}
	m.history = hs
	m.mode = ModeHistory
	return m
}

func (m Model) closeHistoryOverlay() Model {
	if !m.history.visible {
		return m
	}
	m.history.visible = false
	m.mode = m.history.restoreMode()
	return m
}

// handleHistoryKey owns the keyboard while the browser is visible. It is a
// modal overlay, so any key it does not explicitly handle is swallowed (returns
// handled) to keep it from reaching the composer or the normal/plan toggle.
func (m Model) handleHistoryKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if !m.history.visible {
		return m, nil, false
	}
	switch msg.String() {
	case "esc", "u", "q", "ctrl+h":
		// Exit without touching the session.
		return m.closeHistoryOverlay(), nil, true
	case "up", "k":
		m.history = m.history.moveCursor(-1)
		return m, nil, true
	case "down", "j":
		m.history = m.history.moveCursor(1)
		return m, nil, true
	case "enter":
		if m.agent.Busy {
			// Invariant 4: the session must not change while the agent runs.
			// Browsing stays read-only until the turn finishes.
			return m, nil, true
		}
		return m.forkAndResumeFromCursor()
	}
	return m, nil, true
}

// forkAndResumeFromCursor forks the browsed session at the cursor node onto a
// new branch and resets the TUI onto it: the timeline is rebuilt from the path
// seed, the session id tracks the fork, and mode drops back to normal so the
// user can type the forked line of thought. The agent is not started — there
// is no new prompt to run yet.
func (m Model) forkAndResumeFromCursor() (Model, tea.Cmd, bool) {
	controller := m.cfg.SessionHistory
	eventID := m.history.cursorEventID()
	if controller == nil || eventID == "" {
		return m, nil, true
	}
	newID, seed, err := controller.ResumeFromEvent(m.history.sessionID, eventID)
	if err != nil {
		m.history.loadErr = err.Error()
		return m, nil, true
	}
	m.timeline = SeedTimeline(seed)
	m.scroll = 0
	m.scrollPaused = false
	m = m.clearNewOutputBelow()
	m.invalidateMarkdownCache()
	m = m.setActiveSessionID(newID)
	m.mode = m.history.restoreMode()
	m.history = historyState{sessionID: newID, returnMode: m.mode}
	m = m.resetSessionViewState()
	return m, nil, true
}

func (h historyState) cursorEventID() string {
	if h.cursor < 0 || h.cursor >= len(h.flat) {
		return ""
	}
	return h.flat[h.cursor].Event.EventID
}

func (h historyState) moveCursor(delta int) historyState {
	if len(h.flat) == 0 {
		return h
	}
	h.cursor = clampInt(h.cursor+delta, 0, len(h.flat)-1)
	return h
}

func modeReturnTarget(mode Mode) Mode {
	if mode == ModeHistory {
		return ModeNormal
	}
	return mode
}

func (h historyState) restoreMode() Mode {
	if h.returnMode == "" || h.returnMode == ModeHistory {
		return ModeNormal
	}
	return h.returnMode
}

// renderHistoryView renders the browser as a full-screen modal: a header with
// the browsed session id and agent state, the event tree with the cursor
// highlighted and off-path siblings collapsed to a BranchSummary, and a keymap
// footer.
func (m Model) renderHistoryView() string {
	width := max(1, m.width)
	header := " " + m.historyHeader()
	sep := strings.Repeat("─", clampInt(width-2, 1, 80))
	footer := m.historyFooter()
	bodyLines := m.historyBodyLines()

	if m.height > 0 {
		const fixedLines = 3 // header, separator, footer
		if m.height <= fixedLines {
			return renderVisibleLinesWidth([]string{header, sep, footer}, m.height, 0, width)
		}
		bodyH := m.height - fixedLines
		bodyLines = windowLinesAroundMarker(bodyLines, "▶", bodyH)
		body := renderVisibleLinesWidth(bodyLines, bodyH, 0, width)
		lines := []string{header, sep}
		lines = append(lines, strings.Split(body, "\n")...)
		lines = append(lines, footer)
		return renderVisibleLinesWidth(lines, m.height, 0, width)
	}

	lines := []string{header, sep}
	lines = append(lines, bodyLines...)
	lines = append(lines, "", footer)
	return fitViewToTerminal(strings.Join(lines, "\n"), width)
}

func (m Model) historyBodyLines() []string {
	var b strings.Builder
	h := m.history
	switch {
	case h.loadErr != "":
		fmt.Fprintf(&b, "  error: %s\n", h.loadErr)
	case len(h.flat) == 0:
		b.WriteString("  (this session has no events yet)\n")
	default:
		onPath := pathEventSet(h.tree, h.cursorEventID())
		for _, root := range h.tree {
			renderHistoryNode(&b, root, 0, h.cursorEventID(), onPath)
		}
	}
	body := strings.TrimRight(b.String(), "\n")
	if body == "" {
		return []string{""}
	}
	return strings.Split(body, "\n")
}

func windowLinesAroundMarker(lines []string, marker string, maxVisible int) []string {
	if maxVisible < 1 || len(lines) <= maxVisible {
		return lines
	}
	idx := -1
	for i, line := range lines {
		if strings.Contains(line, marker) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return lines[len(lines)-maxVisible:]
	}
	start := idx - maxVisible/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(lines) - maxVisible; start > maxStart {
		start = maxStart
	}
	return lines[start : start+maxVisible]
}

func (m Model) historyHeader() string {
	busy := ""
	if m.agent.Busy {
		busy = "  · agent running (read-only)"
	}
	return fmt.Sprintf("session timeline  ·  %s%s", m.history.sessionID, busy)
}

func (m Model) historyFooter() string {
	return " ↑/↓ move  ·  enter fork-resume from cursor  ·  esc/u exit"
}

// renderHistoryNode prints one tree node indented by depth, marking the cursor.
// Children on the cursor's root→cursor path are expanded; off-path sibling
// subtrees collapse to a rule-based BranchSummary so the tree stays scannable
// while still showing what each abandoned branch did.
func renderHistoryNode(b *strings.Builder, node *session.TreeNode, depth int, cursorID string, onPath map[string]bool) {
	prefix := strings.Repeat("  ", depth)
	marker := "·"
	if node.Event.EventID == cursorID {
		marker = "▶"
	}
	line := strings.TrimSpace(fmt.Sprintf("%s %s %s", prefix, marker, describeHistoryEvent(node.Event)))
	if max := 118; len(line) > max {
		line = line[:max-3] + "..."
	}
	b.WriteString(line)
	b.WriteString("\n")
	for _, child := range node.Children {
		if onPath[child.Event.EventID] {
			renderHistoryNode(b, child, depth+1, cursorID, onPath)
			continue
		}
		summary := strings.TrimRight(session.BranchSummary(collectEvents(child)), "\n")
		b.WriteString(indentLines(summary, depth+1))
		b.WriteString("\n")
	}
}

func describeHistoryEvent(e session.Event) string {
	switch {
	case e.Message != nil:
		content := strings.ReplaceAll(strings.TrimSpace(e.Message.Content), "\n", " ")
		if content == "" {
			content = "(empty)"
		}
		return fmt.Sprintf("[%s] %s", e.Message.Role, truncForDisplay(content, 60))
	case e.ToolCall != nil:
		return fmt.Sprintf("[tool:%s] %s", e.ToolCall.Name, truncForDisplay(e.ToolCall.Output, 60))
	case e.AgentEvent != nil:
		return fmt.Sprintf("[%s] %s", e.AgentEvent.Type, truncForDisplay(e.AgentEvent.Message, 60))
	default:
		return fmt.Sprintf("[%s]", e.Type)
	}
}

func flattenTree(roots []*session.TreeNode) []*session.TreeNode {
	var out []*session.TreeNode
	var walk func(*session.TreeNode)
	walk = func(n *session.TreeNode) {
		if n == nil {
			return
		}
		out = append(out, n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return out
}

func collectEvents(node *session.TreeNode) []session.Event {
	var events []session.Event
	var walk func(*session.TreeNode)
	walk = func(n *session.TreeNode) {
		if n == nil {
			return
		}
		events = append(events, n.Event)
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(node)
	return events
}

func pathEventSet(roots []*session.TreeNode, eventID string) map[string]bool {
	set := make(map[string]bool)
	for _, n := range session.PathTo(roots, eventID) {
		set[n.Event.EventID] = true
	}
	return set
}

func indentLines(s string, depth int) string {
	if s == "" {
		return ""
	}
	pad := strings.Repeat("  ", depth)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n")
}

func truncForDisplay(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
