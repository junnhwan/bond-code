package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) agentBarView() string {
	// Grok-like chrome: never a permanent multi-agent switcher as primary path.
	// Only show when the user is actively in agent focus modes.
	if m.focus != FocusAgentBar && m.focus != FocusAgentWindow {
		return ""
	}
	return m.agentBarViewForWidth(m.width)
}

func (m Model) agentBarViewForWidth(width int) string {
	if width < 1 {
		width = 1
	}

	activeID := m.activeAgentRowID()
	name, status, currentTool := m.agentRowNameStatus(activeID)
	label := commandStyle.Render("Agent") + " " + agentStyle(name).Render(name)
	base := accentStyle.Render("⬡") + " " + label
	agentIDs := m.availableAgentIDs()
	unread := m.totalAgentUnread(agentIDs)
	unreadLabel := dimStyle.Render(fmt.Sprintf("%d unread", unread))
	if unread > 0 {
		unreadLabel = accentStyle.Render(fmt.Sprintf("%d unread", unread))
	}

	withStatus := base
	if status != "" {
		withStatus += " " + dimStyle.Render("·") + " " + agentStatusStyle(status).Render(status)
	}
	summary := withStatus + " " + dimStyle.Render("·") + " " + unreadLabel
	full := summary
	if currentTool != "" {
		full += " " + dimStyle.Render("· ↳ "+currentTool)
	}
	switch m.focus {
	case FocusAgentBar:
		full += " " + dimStyle.Render("· ↑↓ select · Enter open · Esc back")
	case FocusAgentWindow:
		full += " " + dimStyle.Render("· Esc back")
	default:
		if len(agentIDs) > 0 {
			full += " " + dimStyle.Render("· Ctrl+↑ switch")
		}
	}

	for _, candidate := range []string{full, summary, withStatus, label} {
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return ansi.Truncate(label, width, "")
}

func (m Model) activeAgentRowID() string {
	switch m.focus {
	case FocusAgentWindow:
		if m.focusedTaskID != "" {
			return m.focusedTaskID
		}
	case FocusAgentBar:
		if m.agentBarSelected != "" {
			return m.agentBarSelected
		}
	}
	return coordinatorAgentID
}

func (m Model) agentRowNameStatus(id string) (name, status, currentTool string) {
	if id == coordinatorAgentID {
		return "Main", m.conciseCoordinatorAgentState(), ""
	}
	trace := m.subagentTraces[id]
	if trace == nil {
		return firstNonEmpty(strings.TrimSpace(id), "Agent"), "", ""
	}
	name = firstNonEmpty(strings.TrimSpace(trace.AgentType), strings.TrimSpace(trace.Title), "Agent")
	status = strings.TrimSpace(trace.Status)
	currentTool = latestToolName(trace)
	return name, status, currentTool
}

// agentIDsCache is a pointer on Model so View's value receiver can reuse the
// canonical Agent index across streaming and spinner frames. Timeline IDs are
// invalidated by committed TimelineState.Version changes; trace-only IDs use a
// separate membership version because trace content changes must stay cheap.
type agentIDsCache struct {
	initialized            bool
	timelineVersion        int
	traceMembershipVersion uint64
	traceCount             int
	ids                    []string
}

// availableAgentIDs is the canonical ordered set used by Agent availability,
// navigation, hints, and unread accounting. Timeline order stays stable for
// committed child rows; trace-only children follow in task-ID order so map
// iteration cannot make switcher selection nondeterministic.
func (m Model) availableAgentIDs() []string {
	cache := m.agentIDsCache
	if cache != nil && cache.initialized &&
		cache.timelineVersion == m.timeline.Version &&
		cache.traceMembershipVersion == m.traceMembershipVersion &&
		cache.traceCount == len(m.subagentTraces) {
		return cache.ids
	}

	var ids []string
	seen := make(map[string]struct{})
	for _, turn := range m.timeline.Turns {
		for _, block := range turn.Blocks {
			if block.Kind != BlockSubagent || block.ID == "" {
				continue
			}
			if _, ok := seen[block.ID]; ok {
				continue
			}
			seen[block.ID] = struct{}{}
			ids = append(ids, block.ID)
		}
	}

	traceOnly := make([]string, 0, len(m.subagentTraces))
	for id := range m.subagentTraces {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		traceOnly = append(traceOnly, id)
	}
	sort.Strings(traceOnly)
	ids = append(ids, traceOnly...)
	if cache != nil {
		cache.initialized = true
		cache.timelineVersion = m.timeline.Version
		cache.traceMembershipVersion = m.traceMembershipVersion
		cache.traceCount = len(m.subagentTraces)
		cache.ids = ids
	}
	return ids
}

// setSubagentTrace is the only production insertion path for trace membership.
// Existing trace updates do not invalidate the Agent index because IDs are the
// only trace data represented there.
func (m Model) setSubagentTrace(taskID string, trace *AgentTrace) Model {
	if m.subagentTraces == nil {
		m.subagentTraces = make(map[string]*AgentTrace)
	}
	if _, exists := m.subagentTraces[taskID]; !exists {
		m.traceMembershipVersion++
	}
	m.subagentTraces[taskID] = trace
	return m
}

func (m Model) resetSubagentTraces() Model {
	m.subagentTraces = make(map[string]*AgentTrace)
	m.traceMembershipVersion++
	return m
}

func (m Model) totalAgentUnread(agentIDs []string) int {
	unread := 0
	for _, id := range agentIDs {
		if trace := m.subagentTraces[id]; trace != nil && trace.Unread {
			unread++
		}
	}
	return unread
}

// agentBarNameStatus splits the bar label into its agent-type name (colored by
// agentStyle for stable per-type distinction) and lifecycle status (colored by
// agentStatusStyle), so the caller can render each with its own style.
func agentBarNameStatus(trace *AgentTrace, id string) (name, status, currentTool string) {
	if trace == nil {
		return id, "running", ""
	}
	name = trace.AgentType
	if name == "" {
		name = "agent"
	}
	status = trace.Status
	if status == "" {
		status = "running"
	}
	// Current tool = most recent tool block; prefer a running one so the bar
	// shows what each parallel child is doing right now (Phase 5B.1).
	currentTool = latestToolName(trace)
	return
}

// latestToolName returns the child's most recent tool name, preferring a running
// tool so the agent bar reflects the live activity of each parallel child
// rather than a stale completed one.
func latestToolName(trace *AgentTrace) string {
	if trace == nil {
		return ""
	}
	var running, last string
	for _, b := range trace.Blocks {
		if b.Tool != nil && b.Tool.Name != "" {
			last = b.Tool.Name
			if b.Tool.Status == ToolRunning {
				running = b.Tool.Name
			}
		}
	}
	if running != "" {
		return running
	}
	return last
}

func agentStatusStyle(status string) lipgloss.Style {
	switch status {
	case "running":
		return accentStyle
	case "completed":
		return successStyle
	case "failed":
		return errorStyle
	}
	return dimStyle
}

// agentWindowView renders a child agent's trace in place of the main timeline
// when its window is focused: a header (title + status + Esc hint), the prompt,
// the tool-call stream (reusing the tool activity renderer), and the final
// answer rendered as markdown.
func (m Model) agentWindowView(height, width int) string {
	return renderVisibleLinesWidth(m.agentWindowLines(width), height, m.scroll, width)
}

func (m Model) agentWindowLines(width int) []string {
	trace := m.subagentTraces[m.focusedTaskID]
	if trace == nil {
		return []string{dimStyle.Render("(no trace available for this agent)")}
	}
	status := trace.Status
	if status == "" {
		status = "running"
	}
	header := accentStyle.Render("↩ ") + commandStyle.Render(firstNonEmpty(trace.Title, "subagent"))
	header += " " + agentStatusStyle(status).Render(status)
	header += " " + dimStyle.Render("· Esc back")
	lines := []string{header}
	if trace.Prompt != "" {
		lines = append(lines, dimStyle.Render("prompt: ")+dimStyle.Render(truncatePlain(trace.Prompt, max(10, width-10))))
	}
	for _, block := range trace.Blocks {
		rendered := m.renderBlock(block, width)
		if rendered == "" {
			continue
		}
		if isToolActivityBlock(block) {
			rendered = indentRenderedBlock(rendered, "  ")
		}
		lines = append(lines, strings.Split(rendered, "\n")...)
	}
	if live := trace.LiveStream; live != nil {
		if body := live.visibleBody(); body != "" {
			lines = append(lines, strings.Split(body, "\n")...)
		}
	}
	if answer := strings.TrimSpace(trace.FinalAnswer); answer != "" {
		lines = append(lines, "")
		lines = append(lines, successStyle.Render("result:"))
		lines = append(lines, strings.Split(m.renderCachedMarkdownForWidth(trace.TaskID+"-final", answer, width), "\n")...)
	}
	return lines
}

func indentRenderedBlock(value, prefix string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// preferredSubagentID picks a child for Enter-to-fullscreen from scrollback:
// prefer a running trace, else the last available agent ID.
func (m Model) preferredSubagentID() string {
	ids := m.availableAgentIDs()
	if len(ids) == 0 {
		return ""
	}
	for i := len(ids) - 1; i >= 0; i-- {
		id := ids[i]
		if tr := m.subagentTraces[id]; tr != nil {
			st := strings.ToLower(strings.TrimSpace(tr.Status))
			if st == "running" || st == "working" || st == "" {
				return id
			}
		}
	}
	return ids[len(ids)-1]
}
