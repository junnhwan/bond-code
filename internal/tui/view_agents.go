package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// agentListMaxRows caps the multi-line agent roster under the pills strip so the
// dock cannot swallow the whole transcript when many children are active.
const agentListMaxRows = 8

func (m Model) agentBarView() string {
	// Passive strip when children exist; full switcher/list when user is in
	// agent focus modes. Never a permanent empty Agent chrome with no children.
	agents := m.availableAgentIDs()
	if m.focus != FocusAgentBar && m.focus != FocusAgentWindow {
		if len(agents) == 0 {
			return ""
		}
	}
	return m.agentBarViewForWidth(m.width)
}

func (m Model) agentBarViewForWidth(width int) string {
	if width < 1 {
		width = 1
	}
	switch m.focus {
	case FocusAgentBar:
		return m.renderAgentSwitcher(width, true)
	case FocusAgentWindow:
		return m.renderAgentPillsRow(width, true)
	default:
		return m.renderAgentPassiveStrip(width)
	}
}

// renderAgentPassiveStrip is the always-on (when children exist) one-line cue:
// count, active activity, unread, and how to open the switcher.
func (m Model) renderAgentPassiveStrip(width int) string {
	agentIDs := m.availableAgentIDs()
	if len(agentIDs) == 0 {
		// Keep a minimal coordinator-only row for tests that call forWidth
		// without children while focused off the bar.
		label := accentStyle.Render("⬡") + " " + commandStyle.Render("Agent") + " " + agentStyle("Main").Render("Main")
		status := m.conciseCoordinatorAgentState()
		if status != "" {
			label += " " + dimStyle.Render("·") + " " + agentStatusStyle(status).Render(status)
		}
		label += " " + dimStyle.Render("·") + " " + dimStyle.Render("0 unread")
		return fitAgentLine(label, commandStyle.Render("Agent")+" "+agentStyle("Main").Render("Main"), width)
	}

	running, unread, failed, empty := m.agentOutcomeCounts(agentIDs)
	activeName, activeActivity := m.primaryAgentActivity(agentIDs)

	base := accentStyle.Render("⬡") + " " + commandStyle.Render("Agents")
	countLabel := fmt.Sprintf("%d", len(agentIDs))
	if running > 0 {
		countLabel = fmt.Sprintf("%d · %d running", len(agentIDs), running)
	}
	if failed > 0 {
		countLabel += fmt.Sprintf(" · %d failed", failed)
	}
	if empty > 0 {
		countLabel += fmt.Sprintf(" · %d empty", empty)
	}
	summary := base + " " + dimStyle.Render(countLabel)
	if activeName != "" {
		summary += " " + dimStyle.Render("·") + " " + agentStyle(activeName).Render(activeName)
		if activeActivity != "" {
			summary += " " + dimStyle.Render(activeActivity)
		}
	}
	unreadLabel := dimStyle.Render(fmt.Sprintf("%d unread", unread))
	if unread > 0 {
		unreadLabel = accentStyle.Render(fmt.Sprintf("%d unread", unread))
	}
	full := summary + " " + dimStyle.Render("·") + " " + unreadLabel + " " + dimStyle.Render("· click / Ctrl+↑")
	compact := summary + " " + dimStyle.Render("·") + " " + unreadLabel
	minimal := base + " " + dimStyle.Render(fmt.Sprintf("%d", len(agentIDs)))
	return fitAgentLine(full, minimal, width, compact, summary)
}

// renderAgentSwitcher is FocusAgentBar: pills row + multi-line roster list.
func (m Model) renderAgentSwitcher(width int, withHints bool) string {
	pills := m.renderAgentPillsRow(width, false)
	list := m.renderAgentList(width)
	parts := []string{pills}
	if list != "" {
		parts = append(parts, list)
	}
	if withHints {
		hint := dimStyle.Render("↑↓ select · enter open · x cancel · esc back")
		if lipgloss.Width(hint) > width {
			hint = dimStyle.Render("↑↓ · enter · esc")
		}
		parts = append(parts, fitAgentLine(hint, dimStyle.Render("↑↓ · enter"), width))
	}
	return strings.Join(parts, "\n")
}

// renderAgentPillsRow paints Main + each child as compact chips. Selected /
// focused agent is inverse; unread children get a · mark; running ones keep
// their status color on the name.
func (m Model) renderAgentPillsRow(width int, withHints bool) string {
	if width < 1 {
		width = 1
	}
	ids := append([]string{coordinatorAgentID}, m.availableAgentIDs()...)
	selected := m.activeAgentRowID()
	if m.focus == FocusAgentBar && m.agentBarSelected != "" {
		selected = m.agentBarSelected
	}
	if m.focus == FocusAgentWindow && m.focusedTaskID != "" {
		selected = m.focusedTaskID
	}

	prefix := accentStyle.Render("⬡") + " "
	sep := dimStyle.Render(" ")
	var pills []string
	for _, id := range ids {
		pills = append(pills, m.renderAgentPill(id, id == selected))
	}

	// Ensure the selected pill stays visible: prefer a window that includes it.
	line := joinPillsVisible(prefix, sep, pills, ids, selected, width)
	if withHints {
		hint := dimStyle.Render(" · Esc back")
		if m.cfg.CancelSubagent != nil {
			hint = dimStyle.Render(" · Esc back · x cancel")
		}
		candidate := line + hint
		if lipgloss.Width(candidate) <= width {
			line = candidate
		}
	}
	if lipgloss.Width(line) > width {
		line = ansi.Truncate(line, width, "…")
	}
	return line
}

func (m Model) renderAgentPill(id string, selected bool) string {
	name, status, _ := m.agentRowNameStatus(id)
	if name == "" {
		name = "Agent"
	}
	label := name
	if id != coordinatorAgentID {
		if tr := m.subagentTraces[id]; tr != nil && tr.Unread {
			label = name + "·"
		}
	}
	glyph := agentPillGlyph(status)
	text := glyph + label

	style := agentStyle(name)
	if selected {
		style = style.Reverse(true).Bold(true)
	} else if status == "failed" {
		style = errorStyle
	} else if status == "completed" {
		style = dimStyle
	}
	return style.Render(" " + text + " ")
}

func agentPillGlyph(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "working", "":
		return "●"
	case "completed":
		return "✓"
	case "failed":
		return "✗"
	case "cancelled":
		return "⊘"
	case "waiting":
		return "◇"
	default:
		return "○"
	}
}

// joinPillsVisible builds a single pills line that keeps the selected pill on
// screen when the full set does not fit.
func joinPillsVisible(prefix, sep string, pills []string, ids []string, selected string, width int) string {
	if len(pills) == 0 {
		return prefix
	}
	full := prefix + strings.Join(pills, sep)
	if lipgloss.Width(full) <= width {
		return full
	}
	sel := 0
	for i, id := range ids {
		if id == selected {
			sel = i
			break
		}
	}
	// Grow a window around the selection until width is exceeded.
	lo, hi := sel, sel
	line := prefix + pills[sel]
	for lo > 0 || hi < len(pills)-1 {
		expanded := false
		if hi < len(pills)-1 {
			cand := prefix + strings.Join(pills[lo:hi+2], sep)
			if lipgloss.Width(cand) <= width {
				hi++
				line = cand
				expanded = true
			}
		}
		if lo > 0 {
			cand := prefix + strings.Join(pills[lo-1:hi+1], sep)
			if lipgloss.Width(cand) <= width {
				lo--
				line = cand
				expanded = true
			}
		}
		if !expanded {
			break
		}
	}
	if lo > 0 {
		line = dimStyle.Render("…") + strings.TrimPrefix(line, prefix)
		line = prefix + line
	}
	if hi < len(pills)-1 && lipgloss.Width(line)+1 < width {
		line += dimStyle.Render("…")
	}
	if lipgloss.Width(line) > width {
		return ansi.Truncate(line, width, "…")
	}
	return line
}

// renderAgentList is the multi-line roster under the pills (FocusAgentBar).
func (m Model) renderAgentList(width int) string {
	ids := append([]string{coordinatorAgentID}, m.availableAgentIDs()...)
	if len(ids) == 0 {
		return ""
	}
	selected := m.agentBarSelected
	if selected == "" {
		selected = coordinatorAgentID
	}
	// Keep selection visible when many agents: window around selected.
	start := 0
	if len(ids) > agentListMaxRows {
		selIdx := 0
		for i, id := range ids {
			if id == selected {
				selIdx = i
				break
			}
		}
		start = selIdx - agentListMaxRows/2
		if start < 0 {
			start = 0
		}
		if start+agentListMaxRows > len(ids) {
			start = len(ids) - agentListMaxRows
		}
	}
	end := start + agentListMaxRows
	if end > len(ids) {
		end = len(ids)
	}

	var lines []string
	if start > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  … %d above", start)))
	}
	for _, id := range ids[start:end] {
		lines = append(lines, m.renderAgentListRow(id, id == selected, width))
	}
	if end < len(ids) {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  … %d more", len(ids)-end)))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderAgentListRow(id string, selected bool, width int) string {
	name, status, tool := m.agentRowNameStatus(id)
	activity := m.agentActivityText(id, status, tool)
	cursor := "  "
	if selected {
		cursor = accentStyle.Render("▸ ")
	}
	statusPart := agentStatusStyle(status).Render(status)
	// Fixed-ish name column so status/activity columns line up in the roster.
	nameCol := padOrTrimPlain(name, 12)
	namePart := agentStyle(name).Render(nameCol)
	if selected {
		namePart = agentStyle(name).Bold(true).Render(nameCol)
	}

	unread := ""
	if id != coordinatorAgentID {
		if tr := m.subagentTraces[id]; tr != nil && tr.Unread {
			unread = accentStyle.Render(" · unread")
		}
	}
	elapsed := ""
	if id != coordinatorAgentID {
		if tr := m.subagentTraces[id]; tr != nil {
			if d := tr.elapsed(); d > 0 {
				elapsed = dimStyle.Render(" · " + formatRunElapsed(d))
			}
		}
	}

	row := cursor + namePart + "  " + statusPart
	if activity != "" && activity != status {
		row += " " + dimStyle.Render("· "+activity)
	}
	row += elapsed + unread
	if lipgloss.Width(row) > width {
		return ansi.Truncate(row, width, "…")
	}
	return row
}

func padOrTrimPlain(s string, width int) string {
	s = strings.TrimSpace(s)
	if width < 1 {
		return s
	}
	if runewidth := ansi.StringWidth(s); runewidth > width {
		return ansi.Truncate(s, width, "…")
	}
	return s + strings.Repeat(" ", width-ansi.StringWidth(s))
}

func (m Model) agentRunUnreadCounts(agentIDs []string) (running, unread int) {
	running, unread, _, _ = m.agentOutcomeCounts(agentIDs)
	return running, unread
}

// agentOutcomeCounts aggregates multi-agent observability for the passive strip:
// running / unread / failed / empty-completion.
func (m Model) agentOutcomeCounts(agentIDs []string) (running, unread, failed, empty int) {
	for _, id := range agentIDs {
		tr := m.subagentTraces[id]
		if tr == nil {
			continue
		}
		if tr.Unread {
			unread++
		}
		st := strings.ToLower(strings.TrimSpace(tr.Status))
		switch st {
		case "running", "working", "":
			running++
		case "failed":
			failed++
		case "completed":
			if tr.isEmptyCompletion() {
				empty++
			}
		}
	}
	return running, unread, failed, empty
}

// primaryAgentActivity picks the most relevant child for the passive strip:
// prefer a running one (latest), else the last agent.
func (m Model) primaryAgentActivity(agentIDs []string) (name, activity string) {
	if len(agentIDs) == 0 {
		return "", ""
	}
	pick := agentIDs[len(agentIDs)-1]
	for i := len(agentIDs) - 1; i >= 0; i-- {
		id := agentIDs[i]
		tr := m.subagentTraces[id]
		if tr == nil {
			continue
		}
		st := strings.ToLower(strings.TrimSpace(tr.Status))
		if st == "running" || st == "working" || st == "" {
			pick = id
			break
		}
	}
	n, status, tool := m.agentRowNameStatus(pick)
	return n, m.agentActivityText(pick, status, tool)
}

// agentActivityText is the concise secondary label: tool name while running,
// tool count when done, empty-completion cue, etc.
func (m Model) agentActivityText(id, status, tool string) string {
	if id == coordinatorAgentID {
		return ""
	}
	tr := m.subagentTraces[id]
	st := strings.ToLower(strings.TrimSpace(status))
	if st == "" {
		st = "running"
	}
	switch st {
	case "running", "working":
		if tool != "" {
			return "↳ " + tool
		}
		return ""
	case "completed":
		if tr != nil && tr.isEmptyCompletion() {
			return "no tools · empty?"
		}
		if tr != nil {
			if n := tr.toolCount(); n > 0 {
				return fmt.Sprintf("%d tools", n)
			}
		}
		return ""
	case "failed":
		if tool != "" {
			return tool
		}
		return ""
	case "cancelled":
		return "stopped"
	default:
		if tool != "" {
			return "↳ " + tool
		}
		return ""
	}
}

func fitAgentLine(full, minimal string, width int, fallbacks ...string) string {
	candidates := make([]string, 0, 2+len(fallbacks))
	candidates = append(candidates, full)
	candidates = append(candidates, fallbacks...)
	candidates = append(candidates, minimal)
	for _, c := range candidates {
		if c != "" && lipgloss.Width(c) <= width {
			return c
		}
	}
	return ansi.Truncate(minimal, width, "")
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
	if status == "" {
		status = "running"
	}
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
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "working":
		return accentStyle
	case "completed":
		return successStyle
	case "failed":
		return errorStyle
	case "cancelled":
		return warningStyle
	case "waiting":
		return warningStyle
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
	status := strings.TrimSpace(trace.Status)
	if status == "" {
		status = "running"
	}
	name := firstNonEmpty(strings.TrimSpace(trace.AgentType), strings.TrimSpace(trace.Title), "subagent")
	tool := latestToolName(trace)
	activity := m.agentActivityText(m.focusedTaskID, status, tool)

	header := accentStyle.Render("↩ ") + agentStyle(name).Render(name)
	header += " " + agentStatusStyle(status).Render(status)
	if activity != "" && activity != status {
		header += " " + dimStyle.Render("· "+activity)
	}
	if n := trace.toolCount(); n > 0 {
		header += " " + dimStyle.Render(fmt.Sprintf("· %d tools", n))
	}
	if d := trace.elapsed(); d > 0 {
		header += " " + dimStyle.Render("· "+formatRunElapsed(d))
	}
	header += " " + dimStyle.Render("· Esc back")
	if m.cfg.CancelSubagent != nil && (status == "running" || status == "working") {
		header += " " + dimStyle.Render("· x cancel")
	}
	if lipgloss.Width(header) > width {
		header = ansi.Truncate(header, width, "…")
	}
	lines := []string{header}

	if title := strings.TrimSpace(trace.Title); title != "" && title != name {
		lines = append(lines, dimStyle.Render("task: ")+dimStyle.Render(truncatePlain(title, max(10, width-8))))
	}
	if trace.Prompt != "" {
		lines = append(lines, dimStyle.Render("prompt: ")+dimStyle.Render(truncatePlain(trace.Prompt, max(10, width-10))))
	}
	if trace.isEmptyCompletion() {
		warn := warningStyle.Render("⚠ completed with no tool calls — result may be a plan only, not applied work")
		if lipgloss.Width(warn) > width {
			warn = ansi.Truncate(warn, width, "…")
		}
		lines = append(lines, warn)
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
		resultLabel := "result:"
		if trace.isEmptyCompletion() {
			resultLabel = "result (unverified):"
			lines = append(lines, warningStyle.Render(resultLabel))
		} else if status == "failed" {
			lines = append(lines, errorStyle.Render("error:"))
		} else {
			lines = append(lines, successStyle.Render(resultLabel))
		}
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

// selectedSubagentID returns the task ID of the currently selected scrollback
// entry when it is a subagent block; otherwise empty.
func (m Model) selectedSubagentID() string {
	entry, ok := m.selectedScrollEntry()
	if !ok || entry.blockIdx < 0 {
		return ""
	}
	if entry.turnIdx < 0 || entry.turnIdx >= len(m.timeline.Turns) {
		return ""
	}
	blocks := m.timeline.Turns[entry.turnIdx].Blocks
	if entry.blockIdx >= len(blocks) {
		return ""
	}
	block := blocks[entry.blockIdx]
	if block.Kind != BlockSubagent {
		return ""
	}
	return strings.TrimSpace(block.ID)
}
