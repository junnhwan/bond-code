package builtin

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/command"
)

func StatusCommand() command.Command {
	return command.Command{
		Name:        "status",
		Description: "Show agent status",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			if env.StatusProvider != nil {
				snap := env.StatusProvider.StatusSnapshot()
				return command.Result{Output: renderRuntimeStatus(snap), Panel: statusPanel(snap)}, nil
			}
			fallback := fmt.Sprintf("model: %s\ntools: %d\npermission mode: %s", env.Model, env.ToolCount, env.PermissionMode)
			return command.Result{Output: fallback}, nil
		},
	}
}

func renderRuntimeStatus(s app.RuntimeStatus) string {
	lines := []string{
		"session: " + s.SessionID,
		"model: " + s.Model,
		fmt.Sprintf("tools: %d", s.ToolCount),
		"permission mode: " + s.Permission,
		fmt.Sprintf("context: max=%d %s", s.Context.MaxTokens, strings.TrimSpace(s.Context.Stats)),
		fmt.Sprintf("memory: enabled=%t topics=%d index_chars=%d/%d", s.Memory.Enabled, s.Memory.Topics, s.Memory.Chars, s.Memory.MaxChars),
	}
	for _, row := range contextBreakdownRows(s.Context.Breakdown) {
		lines = append(lines, fmt.Sprintf("context %s: %s", row.Key, row.Value))
	}
	if s.Memory.Error != "" {
		lines = append(lines, "memory error: "+s.Memory.Error)
	}
	planning := strings.TrimSpace(s.Planning.Summary)
	if planning == "" {
		planning = fmt.Sprintf("enabled=%t", s.Planning.Enabled)
	}
	lines = append(lines, "planning: "+planning)
	if s.Planning.Error != "" {
		lines = append(lines, "planning error: "+s.Planning.Error)
	}
	lines = append(lines,
		fmt.Sprintf("subagents: enabled=%t active=%d latest=%s", s.Subagents.Enabled, s.Subagents.Active, s.Subagents.Latest),
		fmt.Sprintf("skills: enabled=%t count=%d root=%s", s.Skills.Enabled, s.Skills.Count, s.Skills.Root),
		fmt.Sprintf("mcp: enabled=%t servers=%d tools=%d errors=%d", s.MCP.Enabled, s.MCP.Servers, s.MCP.Tools, s.MCP.Errors),
	)
	if s.Skills.Error != "" {
		lines = append(lines, "skills error: "+s.Skills.Error)
	}
	if strings.TrimSpace(s.Context.Summary) != "" {
		lines = append(lines, "context summary: "+strings.TrimSpace(s.Context.Summary))
	}
	return strings.Join(lines, "\n")
}

// statusPanel builds a structured panel view of the runtime snapshot for the
// TUI's bordered renderer. The plain-text renderRuntimeStatus above stays the
// canonical Output (logs/snapshots/tests); this is the rich counterpart.
func statusPanel(s app.RuntimeStatus) *command.Panel {
	memState := ""
	if s.Memory.Error != "" {
		memState = "error"
	} else if s.Memory.Enabled && s.Memory.MaxChars > 0 && s.Memory.Chars >= s.Memory.MaxChars {
		memState = "warn"
	}
	mcpState := ""
	if s.MCP.Errors > 0 {
		mcpState = "error"
	}
	skillsState := ""
	if s.Skills.Error != "" {
		skillsState = "error"
	}
	planningState := ""
	if s.Planning.Error != "" {
		planningState = "error"
	}

	planningValue := strings.TrimSpace(s.Planning.Summary)
	if planningValue == "" {
		planningValue = fmt.Sprintf("enabled: %t", s.Planning.Enabled)
	}
	subagentsValue := fmt.Sprintf("enabled: %t, active: %d", s.Subagents.Enabled, s.Subagents.Active)
	if latest := strings.TrimSpace(s.Subagents.Latest); latest != "" {
		subagentsValue += ", latest: " + latest
	}

	memory := []command.PanelRow{
		{Key: "state", Value: enabledLabel(s.Memory.Enabled), State: boolState(s.Memory.Enabled)},
		{Key: "topics", Value: fmt.Sprintf("%d topic files", s.Memory.Topics)},
		{Key: "budget", Value: fmt.Sprintf("%d / %d chars", s.Memory.Chars, s.Memory.MaxChars), State: memState},
	}
	if s.Memory.Error != "" {
		memory = append(memory, command.PanelRow{Key: "error", Value: s.Memory.Error, State: "error"})
	}

	contextRows := []command.PanelRow{
		{Key: "budget", Value: fmt.Sprintf("%d tokens", s.Context.MaxTokens)},
	}
	if s.Context.UsedTokens > 0 {
		pct := 0
		if s.Context.MaxTokens > 0 {
			pct = s.Context.UsedTokens * 100 / s.Context.MaxTokens
		}
		state := ""
		if pct >= 90 {
			state = "error"
		} else if pct >= 70 {
			state = "warn"
		}
		contextRows = append(contextRows, command.PanelRow{Key: "used", Value: fmt.Sprintf("%d tokens (%d%%)", s.Context.UsedTokens, pct), State: state})
	} else {
		contextRows = append(contextRows, command.PanelRow{Key: "used", Value: "not measured yet"})
	}
	if estimate := strings.TrimSpace(s.Context.Stats); estimate != "" {
		contextRows = append(contextRows, command.PanelRow{Key: "estimate", Value: estimate})
	}
	if summary := strings.TrimSpace(s.Context.Summary); summary != "" {
		contextRows = append(contextRows, command.PanelRow{Key: "summary", Value: summary})
	}
	contextRows = append(contextRows, contextBreakdownRows(s.Context.Breakdown)...)

	planning := []command.PanelRow{{Key: "state", Value: planningValue, State: planningState}}
	if s.Planning.Error != "" {
		planning = append(planning, command.PanelRow{Key: "error", Value: s.Planning.Error, State: "error"})
	}

	skills := []command.PanelRow{
		{Key: "state", Value: fmt.Sprintf("enabled: %t, count: %d", s.Skills.Enabled, s.Skills.Count), State: skillsState},
		{Key: "root", Value: orDash(s.Skills.Root)},
	}
	if s.Skills.Error != "" {
		skills = append(skills, command.PanelRow{Key: "error", Value: s.Skills.Error, State: "error"})
	}

	return &command.Panel{
		Title: "status",
		Sections: []command.PanelSection{
			{Label: "RUNTIME", Rows: []command.PanelRow{
				{Key: "session", Value: orDash(s.SessionID)},
				{Key: "model", Value: orDash(s.Model)},
				{Key: "permission", Value: orDash(s.Permission)},
				{Key: "tools", Value: strconv.Itoa(s.ToolCount)},
			}},
			{Label: "CONTEXT", Rows: contextRows},
			{Label: "MEMORY", Rows: memory},
			{Label: "PLANNING", Rows: planning},
			{Label: "SUBAGENTS", Rows: []command.PanelRow{
				{Key: "state", Value: subagentsValue},
			}},
			{Label: "SKILLS", Rows: skills},
			{Label: "MCP", Rows: []command.PanelRow{
				{Key: "state", Value: fmt.Sprintf("enabled: %t, servers: %d, tools: %d", s.MCP.Enabled, s.MCP.Servers, s.MCP.Tools), State: mcpState},
				{Key: "errors", Value: strconv.Itoa(s.MCP.Errors), State: mcpState},
			}},
		},
	}
}

func enabledLabel(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

func boolState(on bool) string {
	if on {
		return "ok"
	}
	return ""
}

func orDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}
