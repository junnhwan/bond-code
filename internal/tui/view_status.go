package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) renderTurnRunStatus(turn Turn) string {
	run := turn.Run
	if run.State == "" && (m.agent.Pending != nil || m.question != nil) && isLatestTurn(m.timeline, turn.ID) {
		run.State = "waiting"
		run.Detail = m.currentAgentDetail()
		run.StartedAt = firstTime(turn.StartedAt, time.Now())
	}
	if run.State == "" && m.agent.Busy && isLatestTurn(m.timeline, turn.ID) {
		run.State = "working"
		run.Detail = m.currentAgentDetail()
		run.StartedAt = firstTime(turn.StartedAt, time.Now())
	}
	if run.State == "" {
		return ""
	}
	if isLatestTurn(m.timeline, turn.ID) && (m.agent.Busy || m.agent.Pending != nil || m.question != nil) {
		if detail := strings.TrimSpace(m.agent.LiveDetail); detail != "" {
			run.Detail = detail
		}
	}

	// Interrupted: the agent stopped without reaching a terminal state (loop
	// guard, max steps, panic, or a cancelled run). Without this the stale
	// "working" line keeps ticking up elapsed time and the user has no signal
	// the turn actually stopped — it just looks hung.
	if isLatestTurn(m.timeline, turn.ID) && !m.agent.Busy && !m.hasPendingDock() && !isTerminalRunState(run.State) {
		interruptedAt := firstTime(run.StartedAt, turn.StartedAt)
		if interruptedAt.IsZero() {
			interruptedAt = time.Now()
		}
		detail := strings.TrimSpace(run.Detail)
		if detail == "" {
			detail = "stopped"
		}
		return warningStyle.Render(fmt.Sprintf("⚠ interrupted · %s · %s", detail, formatRunElapsed(time.Since(interruptedAt))))
	}

	startedAt := firstTime(run.StartedAt, turn.StartedAt)
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	endedAt := firstTime(run.EndedAt, turn.EndedAt)
	elapsed := time.Since(startedAt)
	if !endedAt.IsZero() {
		elapsed = endedAt.Sub(startedAt)
	}
	if elapsed < 0 {
		elapsed = 0
	}

	detail := strings.TrimSpace(run.Detail)
	switch run.State {
	case "done":
		return dimStyle.Render("✓ done · " + formatRunElapsed(elapsed))
	case "failed":
		return errorStyle.Render("✗ failed · " + formatRunElapsed(elapsed))
	case "cancelled":
		return warningStyle.Render("⊘ cancelled · " + formatRunElapsed(elapsed))
	case "waiting":
		label := "waiting"
		if detail != "" {
			label = detail
		}
		return warningStyle.Render("◆ "+label) + dimStyle.Render(" · "+formatRunElapsed(elapsed))
	}

	// working / other active states → Grok turn-status row (braille + wave + timers)
	activity := detail
	if activity == "" {
		activity = "working"
	}
	right := formatRunElapsed(elapsed)
	if ctx := m.contextLabel(); ctx != "" {
		right += "  " + ctx
	}
	stopHover := m.hover.kind == mouseHitStop
	return FormatTurnStatusRowWithStop(m.animFrame, activity, right, 120, stopHover)
}

// renderTurnStatusLine is the dock-level turn status (idle → empty).
// Appears between scrollback and prompt only while busy / waiting.
func (m Model) renderTurnStatusLine(width int) string {
	if width < 1 {
		width = 1
	}
	if !m.agent.Busy && m.agent.Pending == nil && m.question == nil {
		return ""
	}
	stopHover := m.hover.kind == mouseHitStop
	if len(m.timeline.Turns) == 0 {
		if m.agent.Busy {
			return FormatTurnStatusRowWithStop(m.animFrame, m.currentAgentDetail(), "", width, stopHover)
		}
		if m.agent.Pending != nil {
			label := m.currentAgentDetail()
			// Grok waiting diamond: pulse accent toward dim.
			diamond := "◆ "
			if animPulseOn(m.animFrame) {
				return truncateStyled(accentStyle.Render(diamond)+busyStyle.Render(label), width)
			}
			return truncateStyled(dimStyle.Render(diamond)+busyStyle.Render(label), width)
		}
		return ""
	}
	latest := m.timeline.Turns[len(m.timeline.Turns)-1]
	if m.agent.Pending != nil || m.question != nil {
		// Waiting diamond row above the permission/question panel.
		return truncateStyled(m.renderTurnRunStatus(latest), width)
	}
	if !m.agent.Busy {
		return truncateStyled(m.renderTurnRunStatus(latest), width)
	}
	// Busy: compose Grok turn-status from live activity + elapsed + [stop].
	activity := m.currentAgentDetail()
	if d := strings.TrimSpace(m.agent.LiveDetail); d != "" {
		activity = d
	} else if d := strings.TrimSpace(latest.Run.Detail); d != "" {
		activity = d
	}
	startedAt := firstTime(latest.Run.StartedAt, latest.StartedAt)
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	right := formatRunElapsed(time.Since(startedAt))
	if ctx := m.contextLabel(); ctx != "" {
		right += "  " + ctx
	}
	return FormatTurnStatusRowWithStop(m.animFrame, activity, right, width, stopHover)
}

func (m Model) hasPendingDock() bool {
	if m.agent.Pending != nil || m.question != nil {
		return true
	}
	if latest := m.latestToolBlock(); latest != nil && latest.Status == ToolPending {
		return true
	}
	return false
}

func (m Model) latestRunState() string {
	if len(m.timeline.Turns) == 0 {
		return ""
	}
	return m.timeline.Turns[len(m.timeline.Turns)-1].Run.State
}

// conciseCoordinatorAgentState returns only lifecycle bookkeeping suitable for
// the persistent Agent row. It deliberately excludes live stream content,
// dynamic detail, elapsed time, and spinner state.
func (m Model) conciseCoordinatorAgentState() string {
	if m.agent.Pending != nil || m.question != nil {
		return "waiting"
	}
	if m.agent.Busy {
		return "running"
	}
	state := strings.TrimSpace(m.latestRunState())
	if state == "working" {
		return "running"
	}
	return state
}

func (m Model) currentAgentDetail() string {
	if m.agent.Pending != nil {
		return "confirm " + m.agent.Pending.ToolName
	}
	if m.question != nil {
		return "question"
	}
	if latest := m.latestToolBlock(); latest != nil {
		switch latest.Status {
		case ToolPending:
			return "confirm " + latest.Name
		case ToolRunning:
			return "tool: " + latest.Name
		}
	}
	return "thinking"
}

func isLatestTurn(timeline TimelineState, id string) bool {
	return len(timeline.Turns) > 0 && timeline.Turns[len(timeline.Turns)-1].ID == id
}

// isTerminalRunState reports whether a turn's run status is final (done /
// failed / cancelled). A non-terminal state left behind once the agent is no
// longer busy means the turn was interrupted rather than finished.
func isTerminalRunState(state string) bool {
	switch state {
	case "done", "failed", "cancelled":
		return true
	}
	return false
}

func formatRunElapsed(duration time.Duration) string {
	total := int(duration.Truncate(time.Second).Seconds())
	if total < 0 {
		total = 0
	}
	hours := total / 3600
	minutes := (total % 3600) / 60
	seconds := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func renderVisibleLines(lines []string, height int, scroll int) string {
	if height < 1 {
		height = 1
	}
	if len(lines) > height {
		maxStart := len(lines) - height
		if scroll > maxStart {
			scroll = maxStart
		}
		start := maxStart - scroll
		if start < 0 {
			start = 0
		}
		if start > maxStart {
			start = maxStart
		}
		lines = lines[start : start+height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func renderVisibleLinesWidth(lines []string, height int, scroll int, width int) string {
	if width < 1 {
		width = 1
	}
	rendered := renderVisibleLines(lines, height, scroll)
	out := strings.Split(rendered, "\n")
	for i, line := range out {
		out[i] = fitStyledLine(line, width)
	}
	return strings.Join(out, "\n")
}

func fitStyledLine(line string, width int) string {
	if width < 1 {
		width = 1
	}
	lineWidth := lipgloss.Width(line)
	if lineWidth == width {
		return line
	}
	if lineWidth > width {
		return ansi.Truncate(line, width, "")
	}
	return line + strings.Repeat(" ", width-lineWidth)
}

func fitViewToTerminal(view string, width int) string {
	if width < 1 {
		width = 1
	}
	if view == "" {
		return ""
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		lines[i] = fitStyledLine(line, width)
	}
	return strings.Join(lines, "\n")
}

func fitRenderedBlockHeight(value string, maxHeight int, width int) string {
	if value == "" {
		return ""
	}
	if maxHeight < 1 {
		maxHeight = 1
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= maxHeight {
		return value
	}
	if maxHeight == 1 {
		return truncateStyled(lines[0], width)
	}
	if maxHeight == 2 {
		return strings.Join([]string{
			truncateStyled(lines[0], width),
			truncateStyled(lines[len(lines)-1], width),
		}, "\n")
	}
	tailCount := 2
	if maxHeight == 3 {
		return strings.Join([]string{
			truncateStyled(lines[0], width),
			truncateStyled(lines[len(lines)-2], width),
			truncateStyled(lines[len(lines)-1], width),
		}, "\n")
	}
	headCount := maxHeight - tailCount - 1
	hidden := len(lines) - headCount - tailCount
	ellipsis := dimStyle.Render(fmt.Sprintf("... +%d more lines", hidden))
	out := make([]string, 0, maxHeight)
	for _, line := range lines[:headCount] {
		out = append(out, truncateStyled(line, width))
	}
	out = append(out, truncateStyled(ellipsis, width))
	for _, line := range lines[len(lines)-tailCount:] {
		out = append(out, truncateStyled(line, width))
	}
	return strings.Join(out, "\n")
}
