package tui

import (
	"fmt"
	"path/filepath"
	"strings"
)

// LiveStatus holds session chrome data refreshed from the runtime (tasks,
// context breakdown, teams, subagent counts). It is not rendered as a side
// column — Grok chrome is single-pane; these fields feed prompt info, todo
// chips, /status panels, and subagent chrome.
type LiveStatus struct {
	SessionID            string
	ProjectRoot          string
	GitBranch            string
	Model                string
	PermissionMode       string
	ToolCount            int
	ContextSummary       string
	MemorySummary        string
	TodoSummary          string
	PlanningSummary      string
	SubagentSummary      string
	MCPStatus            string
	ActiveSubagents      int
	LatestSubagentStatus string
	Tasks                []TaskView
	Breakdown            ContextBreakdownView
	Teams                []TeamView
}

// LiveStatusFromStatus seeds live chrome from the startup Status snapshot.
func LiveStatusFromStatus(status Status) LiveStatus {
	return LiveStatus{
		SessionID:       defaultString(status.SessionID, "local"),
		ProjectRoot:     defaultString(status.ProjectRoot, "."),
		GitBranch:       defaultString(status.GitBranch, "no-git"),
		Model:           defaultString(status.Model, "model"),
		PermissionMode:  defaultString(status.PermissionMode, "confirm"),
		ToolCount:       status.ToolCount,
		ContextSummary:  status.ContextSummary,
		MemorySummary:   status.MemorySummary,
		PlanningSummary: status.PlanningSummary,
		SubagentSummary: status.SubagentSummary,
		MCPStatus:       defaultString(status.MCPStatus, "mcp off"),
		Tasks:           status.Tasks,
		Breakdown:       status.ContextBreakdown,
		Teams:           status.Teams,
	}
}

// LiveStatusWithSubagentTimeline folds subagent blocks in the main timeline
// into active-count / latest-status fields for chrome (agent bar, titles).
func LiveStatusWithSubagentTimeline(state LiveStatus, timeline TimelineState) LiveStatus {
	activeByTask := map[string]string{}
	anonymousStatuses := []string{}
	latestStatus := ""

	for _, turn := range timeline.Turns {
		for _, block := range turn.Blocks {
			if block.Kind != BlockSubagent {
				continue
			}
			status := subagentBlockStatus(block)
			if status == "" {
				status = "running"
			}
			latestStatus = status
			if taskID := subagentBlockField(block, "task"); taskID != "" {
				activeByTask[taskID] = status
				continue
			}
			anonymousStatuses = append(anonymousStatuses, status)
		}
	}

	active := 0
	for _, status := range activeByTask {
		if !isTerminalSubagentStatus(status) {
			active++
		}
	}
	for _, status := range anonymousStatuses {
		if !isTerminalSubagentStatus(status) {
			active++
		}
	}

	state.ActiveSubagents = active
	state.LatestSubagentStatus = latestStatus
	return state
}

func subagentBlockStatus(block Block) string {
	if status := strings.TrimSpace(block.Summary); status != "" {
		return status
	}
	return subagentBlockField(block, "status")
}

func subagentBlockField(block Block, key string) string {
	prefix := strings.ToLower(strings.TrimSpace(key)) + ":"
	for _, line := range strings.Split(block.Body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

func isTerminalSubagentStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func sectionLabel(value string) string {
	return dimStyle.Render("▸ " + value)
}

func projectName(root string) string {
	root = strings.TrimSpace(root)
	if root == "" || root == "." {
		return "."
	}
	base := filepath.Base(root)
	if base == "." || base == string(filepath.Separator) {
		return root
	}
	return base
}

// todoChip summarizes the live checklist for the prompt info line.
// Empty when there is no active list.
func todoChip(tasks []TaskView) string {
	if len(tasks) == 0 {
		return ""
	}
	done := 0
	var active string
	for _, t := range tasks {
		if t.Status == "completed" {
			done++
			continue
		}
		if t.Status == "in_progress" && active == "" {
			active = firstNonEmpty(t.ActiveForm, t.Subject)
		}
	}
	label := fmt.Sprintf("todo %d/%d", done, len(tasks))
	if active != "" {
		label += " · " + truncatePlain(active, 24)
	}
	return label
}
