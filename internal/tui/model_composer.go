package tui

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/contextx"
)

func (m Model) SetInput(value string) Model {
	m.composer = m.composer.setValue(value)
	return m
}

func (m Model) clearInput() Model {
	m.composer = m.composer.clear()
	return m
}
func (m Model) Submit(ctx context.Context) (Model, tea.Cmd) {
	// Phase 5C.1: expandedValue joins the typed prompt with any collapsed paste
	// payloads so the model receives the full pasted content, not the markers.
	prompt := strings.TrimSpace(m.composer.expandedValue())
	if prompt == "" {
		return m, nil
	}
	if isExitSlashCommand(prompt) {
		if m.agent.Busy {
			m = m.cancelRunningAgent()
		}
		m = m.clearInput()
		return m, tea.Quit
	}
	if isRetrySlashCommand(prompt) {
		return m.retryLatestFailedTurn()
	}
	m = m.rememberPrompt(prompt)
	m.composer = m.composer.clear()
	m = m.followBottom()
	// While the agent is busy, park plain prompts in a queue rendered above the
	// composer instead of entering the timeline; they start automatically when the
	// current turn finishes. /commands still run immediately so /status etc. work
	// mid-turn.
	if m.agent.Busy && m.cfg.Chat != nil && !strings.HasPrefix(prompt, "/") {
		m.agent.QueuedPrompts = append(m.agent.QueuedPrompts, prompt)
		return m, nil
	}
	if strings.HasPrefix(prompt, "/") {
		if m.agent.Busy {
			return m.runCommand(ctx, prompt)
		}
		m = m.beginUserTurn(prompt)
		return m.runCommand(ctx, prompt)
	}
	m = m.beginUserTurn(prompt)
	if m.cfg.Chat == nil {
		m.timeline = m.timeline.AppendBlock(BlockError, "error", "agent is not configured")
		return m.markNewOutputBelow(), nil
	}
	agentPrompt := contextx.ExpandPathMentions(prompt, m.cfg.Status.ProjectRoot)
	return m, func() tea.Msg {
		return runAgentMsg{prompt: agentPrompt}
	}
}

func (m Model) retryLatestFailedTurn() (Model, tea.Cmd) {
	if m.agent.Busy {
		body := "cannot retry while agent is running"
		m = m.clearInput()
		m.timeline = m.timeline.AppendBlock(BlockError, "/retry", body)
		return m.markNewOutputBelow(), nil
	}
	prompt := m.latestFailedTurnPrompt()
	if strings.TrimSpace(prompt) == "" {
		body := "no failed turn to retry"
		m = m.clearInput()
		m.timeline = m.timeline.AppendBlock(BlockError, "/retry", body)
		return m.markNewOutputBelow(), nil
	}
	m = m.clearInput()
	m = m.followBottom()
	m = m.beginUserTurn(prompt)
	if m.cfg.Chat == nil {
		body := "agent is not configured"
		m.timeline = m.timeline.AppendBlock(BlockError, "error", body)
		return m.markNewOutputBelow(), nil
	}
	agentPrompt := contextx.ExpandPathMentions(prompt, m.cfg.Status.ProjectRoot)
	return m, func() tea.Msg {
		return runAgentMsg{prompt: agentPrompt}
	}
}

func (m Model) latestFailedTurnPrompt() string {
	for i := len(m.timeline.Turns) - 1; i >= 0; i-- {
		turn := m.timeline.Turns[i]
		if isRetryableFailedTurn(turn) {
			return strings.TrimSpace(turn.User.Body)
		}
	}
	return ""
}

func isRetryableFailedTurn(turn Turn) bool {
	if turn.Run.State == "failed" || turn.Run.State == "cancelled" {
		return strings.TrimSpace(turn.User.Body) != ""
	}
	for _, block := range turn.Blocks {
		if block.Kind == BlockError {
			return strings.TrimSpace(turn.User.Body) != ""
		}
		if block.Tool != nil && block.Tool.Status == ToolFailed {
			return strings.TrimSpace(turn.User.Body) != ""
		}
	}
	return false
}

// beginUserTurn opens a new committed timeline turn. Shared by interactive
// Submit and the queue drain so a queued prompt looks identical to one typed
// after the previous turn finished.
func (m Model) beginUserTurn(prompt string) Model {
	m.timeline = m.timeline.StartUserTurn(prompt)
	return m.markNewOutputBelow()
}

// Bubble Tea 1.x cannot encode a generic Shift modifier on Enter. Alt+Enter is
// representable but often dropped by Windows Terminal. Ctrl+J is the reliable
// cross-platform newline (classic terminal line-feed).
func isComposerNewlineKey(key string) bool {
	switch key {
	case "shift+enter", "alt+enter", "ctrl+j", "ctrl+enter":
		return true
	default:
		return false
	}
}

func isModeCycleKey(key string) bool {
	return key == "shift+tab" || strings.EqualFold(key, "alt+m")
}

func (m Model) insertNewline() Model {
	m.composer = m.composer.insertNewline()
	return m
}

func (m Model) cycleMode() Model {
	if m.agent.Pending != nil {
		return m
	}
	m.mode = m.mode.Toggle()
	if m.cfg.PlanMode != nil {
		m.cfg.PlanMode.SetPlanMode(m.mode.IsPlan())
	}
	return m
}

func (m Model) rememberPrompt(prompt string) Model {
	before := m.promptHistory()
	m.composer = m.composer.rememberPrompt(prompt)
	if !samePromptHistory(before, m.composer.History) {
		_ = savePromptHistory(m.cfg.PromptHistoryPath, m.composer.History)
	}
	return m
}

func (m Model) canUsePromptHistory() bool {
	return m.composer.canUseHistory()
}

func (m Model) previousHistory() Model {
	m.composer = m.composer.previousHistory()
	return m
}

func (m Model) nextHistory() Model {
	m.composer = m.composer.nextHistory()
	return m
}

// updateSuggestions shows or hides suggestions based on current input
func (m Model) updateSuggestions() Model {
	value := m.inputValue()
	if filter, _, ok := activeFileMentionToken(value); ok && m.composer.Suggestions != nil {
		items := FileMentionSuggestions(m.cfg.Status.ProjectRoot, filter)
		m.composer.Suggestions.ShowFiles(filter, items)
		return m
	}
	m.composer = m.composer.updateSuggestions()
	return m
}

// getCommandFilter extracts the filter text from current input (without the /)
func (m Model) getCommandFilter() string {
	if m.composer.Suggestions != nil && m.composer.Suggestions.CurrentPrefix() == "@" {
		if filter, _, ok := activeFileMentionToken(m.inputValue()); ok {
			return filter
		}
		return ""
	}
	return m.composer.commandFilter()
}

func (m Model) completeSelectedSuggestion(filter, selected string) Model {
	if m.composer.Suggestions != nil && m.composer.Suggestions.CurrentPrefix() == "@" {
		if _, start, ok := activeFileMentionToken(m.inputValue()); ok {
			value := m.inputValue()
			m.composer = m.composer.setValue(value[:start] + formatFileMentionCompletion(selected) + " ")
			m.composer.HistoryIndex = -1
			m.composer.HistoryDraft = ""
			return m
		}
	}
	m.composer = m.composer.setValue(m.composer.Suggestions.GetSelectedCompletion(filter))
	m.composer.HistoryIndex = -1
	m.composer.HistoryDraft = ""
	return m
}

// slashSuggestionAutoSubmits mirrors Claude Code's applyCommandSuggestion:
// Enter always fills `/name `, but only auto-runs commands that do not take
// free-text arguments. Skills and custom prompt templates keep the draft open
// so the user can append args / a prompt, then press Enter again to submit.
func slashSuggestionAutoSubmits(item Suggestion, registry *command.Registry) bool {
	switch strings.ToLower(strings.TrimSpace(item.Source)) {
	case "skill", "custom":
		return false
	}
	if registry != nil {
		if cmd, ok := registry.Get(item.Name); ok && strings.TrimSpace(cmd.PromptTemplate) != "" {
			return false
		}
	}
	return true
}

func formatFileMentionCompletion(path string) string {
	path = strings.TrimSpace(path)
	if strings.ContainsAny(path, " \t\r\n<>") {
		path = strings.NewReplacer("<", "", ">", "").Replace(path)
		return "@<" + path + ">"
	}
	return "@" + path
}

func activeFileMentionToken(value string) (filter string, start int, ok bool) {
	at := strings.LastIndex(value, "@")
	if at < 0 || !isFileMentionBoundary(value[:at]) {
		return "", 0, false
	}
	start = at
	token := value[start:]
	if strings.HasPrefix(token, "@<") {
		body := strings.TrimPrefix(token, "@<")
		if strings.Contains(body, ">") {
			return "", 0, false
		}
		return body, start, true
	}
	if strings.IndexFunc(token, unicode.IsSpace) >= 0 {
		return "", 0, false
	}
	return strings.TrimPrefix(token, "@"), start, true
}

func isFileMentionBoundary(prefix string) bool {
	if prefix == "" {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(prefix)
	return unicode.IsSpace(r) || strings.ContainsRune(`"'([{<,:;`, r)
}

// extractFileMentions returns every completed @path / @<path with spaces> token
// in value. The token currently being typed (no trailing separator) is included
// too — the attachment-chip row surfaces every mention in the draft, completed
// or not, so the user sees exactly what context will expand on submit. The
// boundary rule mirrors activeFileMentionToken so the two agree on what counts
// as a mention.
func extractFileMentions(value string) []string {
	var out []string
	runes := []rune(value)
	separators := `"'([{<,:;>`
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			continue
		}
		prevOk := i == 0
		if i > 0 {
			p := runes[i-1]
			prevOk = unicode.IsSpace(p) || strings.ContainsRune(separators, p)
		}
		if !prevOk {
			continue
		}
		if i+1 < len(runes) && runes[i+1] == '<' {
			j := i + 2
			for j < len(runes) && runes[j] != '>' {
				j++
			}
			if j < len(runes) && j > i+2 {
				out = append(out, string(runes[i+2:j]))
				i = j
				continue
			}
		}
		j := i + 1
		for j < len(runes) && !unicode.IsSpace(runes[j]) && !strings.ContainsRune(separators, runes[j]) {
			j++
		}
		if j > i+1 {
			out = append(out, string(runes[i+1:j]))
			i = j - 1
		}
	}
	return out
}

func isEmptySuggestionPrompt(value string) bool {
	value = strings.TrimSpace(value)
	return value == "/" || value == "@"
}
