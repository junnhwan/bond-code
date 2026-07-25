package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/command"
)

// ContextCommand shows context-window occupancy from the model's real token
// counts (used / budget / remaining / share), the current session's message
// counts, and project+model. Replaces the old stub that printed project, an id,
// and an almost-always-empty summary string.
func ContextCommand() command.Command {
	return command.Command{
		Name:        "context",
		Description: "Show context window usage breakdown",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "summary") {
				return contextSummary(env), nil
			}
			panel := contextPanel(env)
			return command.Result{Output: renderPanelText(panel), Panel: panel}, nil
		},
	}
}

func contextPanel(env command.Env) *command.Panel {
	var snap app.RuntimeStatus
	if env.StatusProvider != nil {
		snap = env.StatusProvider.StatusSnapshot()
	}
	maxTokens := snap.Context.MaxTokens
	used := snap.Context.UsedTokens
	pct := 0
	if maxTokens > 0 {
		pct = used * 100 / maxTokens
	}
	state := ""
	switch {
	case pct >= 90:
		state = "error"
	case pct >= 70:
		state = "warn"
	}

	window := []command.PanelRow{
		{Key: "budget", Value: fmt.Sprintf("%d tokens", maxTokens)},
	}
	if used > 0 {
		window = append(window,
			command.PanelRow{Key: "used", Value: fmt.Sprintf("%d tokens (%d%%)", used, pct), State: state},
			command.PanelRow{Key: "remaining", Value: fmt.Sprintf("%d tokens", maxTokens-used)},
		)
	} else {
		window = append(window, command.PanelRow{Key: "used", Value: "not measured yet"})
	}
	if summary := strings.TrimSpace(snap.Context.Summary); summary != "" {
		window = append(window, command.PanelRow{Key: "summary", Value: summary})
	}
	sections := []command.PanelSection{{Label: "CONTEXT WINDOW", Rows: window}}
	if rows := contextBreakdownRows(snap.Context.Breakdown); len(rows) > 0 {
		// Leading visual bar (key-less rows) then numeric breakdown — mirrors
		// Claude Code /context composition without a permanent sidebar.
		barRows := contextBreakdownVisualRows(snap.Context.Breakdown)
		breakdown := append(barRows, rows...)
		sections = append(sections, command.PanelSection{Label: "TOKEN BREAKDOWN", Rows: breakdown})
	}

	current := currentSessionStats(env)
	sections = append(sections, command.PanelSection{Label: "THIS SESSION", Rows: []command.PanelRow{
		{Key: "messages", Value: fmt.Sprintf("%d (%d user, %d assistant)", current.totalMessages(), current.userMsgs, current.assistantMsgs)},
		{Key: "tool calls", Value: fmt.Sprintf("%d", current.toolCalls)},
	}})

	sections = append(sections, command.PanelSection{Label: "PROJECT", Rows: []command.PanelRow{
		{Key: "root", Value: orDash(env.ProjectRoot)},
		{Key: "model", Value: orDash(env.Model)},
	}})

	return &command.Panel{Title: "context", Sections: sections}
}

func contextBreakdownRows(b app.ContextBreakdown) []command.PanelRow {
	candidates := []struct {
		key    string
		tokens int
	}{
		{key: "system", tokens: b.SystemTokens},
		{key: "history", tokens: b.ConversationTokens},
		{key: "tool results", tokens: b.ToolResultTokens},
		{key: "context summary", tokens: b.SummaryTokens},
	}
	rows := make([]command.PanelRow, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.tokens <= 0 {
			continue
		}
		rows = append(rows, command.PanelRow{
			Key:   candidate.key,
			Value: fmt.Sprintf("%d tokens", candidate.tokens),
		})
	}
	return rows
}

// contextBreakdownVisualRows builds a plain-text proportional bar + legend for
// the panel. Empty when there is nothing to chart. Keys are blank so the TUI
// panel renderer prints Value as a full-width visual line.
func contextBreakdownVisualRows(b app.ContextBreakdown) []command.PanelRow {
	total := b.SystemTokens + b.ConversationTokens + b.ToolResultTokens + b.SummaryTokens
	if total <= 0 {
		return nil
	}
	const width = 28
	type seg struct {
		n    int
		mark rune
	}
	segs := []seg{
		{b.SystemTokens, 'S'},
		{b.ConversationTokens, 'H'},
		{b.ToolResultTokens, 'T'},
		{b.SummaryTokens, 'C'},
	}
	var bar strings.Builder
	allocated := 0
	last := -1
	for i, s := range segs {
		if s.n > 0 {
			last = i
		}
	}
	for i, s := range segs {
		if s.n <= 0 {
			continue
		}
		w := width * s.n / total
		if i == last {
			w = width - allocated
		}
		if w > 0 {
			bar.WriteString(strings.Repeat(string(s.mark), w))
			allocated += w
		}
	}
	legendParts := make([]string, 0, 4)
	if b.SystemTokens > 0 {
		legendParts = append(legendParts, fmt.Sprintf("S sys %d", b.SystemTokens))
	}
	if b.ConversationTokens > 0 {
		legendParts = append(legendParts, fmt.Sprintf("H hist %d", b.ConversationTokens))
	}
	if b.ToolResultTokens > 0 {
		legendParts = append(legendParts, fmt.Sprintf("T tool %d", b.ToolResultTokens))
	}
	if b.SummaryTokens > 0 {
		legendParts = append(legendParts, fmt.Sprintf("C sum %d", b.SummaryTokens))
	}
	return []command.PanelRow{
		{Value: bar.String()},
		{Value: strings.Join(legendParts, " · ")},
	}
}

func contextSummary(env command.Env) command.Result {
	var snap app.RuntimeStatus
	if env.StatusProvider != nil {
		snap = env.StatusProvider.StatusSnapshot()
	}
	summary := strings.TrimSpace(snap.Context.SummaryText)
	if summary == "" {
		return command.Result{Output: "No saved context summary. Run /compact after a longer session, then use /context summary again."}
	}
	header := "Context summary"
	if created := strings.TrimSpace(snap.Context.Summary); created != "" {
		header += " saved at " + created
	}
	return command.Result{Output: header + "\n\n" + summary}
}
