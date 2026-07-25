package contextx

import (
	"fmt"
	"sort"
	"strings"

	"github.com/junnhwan/bond-code/internal/llm"
)

// Cleared tool-result placeholder (Claude Code time-based microcompact style).
const toolResultClearedMessage = "[Old tool result content cleared]"

// Tools whose results are safe to micro-clear when old (read-heavy / ephemeral output).
var compactableTools = map[string]bool{
	"read_file":   true,
	"list_dir":    true,
	"search_text": true,
	"run_command": true,
	"web_search":  true,
	"web_fetch":   true,
}

// governToolResults applies Claude Code-style tool-result pressure relief:
// 1) micro-clear old compactable results (keep last N)
// 2) spill/truncate individual oversized results
// 3) spill largest results in the latest tool turn when aggregate budget is exceeded
func governToolResults(messages []Message, cfg GovernorConfig) (out []Message, microCleared, spilled int) {
	out = microClearToolResults(messages, cfg)
	microCleared = countChangedToolResults(messages, out)
	budgeted := applyToolResultBudget(out, cfg)
	spilled = countChangedToolResults(out, budgeted)
	return budgeted, microCleared, spilled
}

func microClearToolResults(messages []Message, cfg GovernorConfig) []Message {
	toolIndices := make([]int, 0)
	for i, msg := range messages {
		if msg.Role == llm.RoleTool && compactableTools[msg.ToolName] {
			toolIndices = append(toolIndices, i)
		}
	}
	if len(toolIndices) <= cfg.MicroCompactKeepRecent {
		return messages
	}
	clearSet := make(map[int]bool, len(toolIndices)-cfg.MicroCompactKeepRecent)
	for i := 0; i < len(toolIndices)-cfg.MicroCompactKeepRecent; i++ {
		clearSet[toolIndices[i]] = true
	}
	out := make([]Message, len(messages))
	copy(out, messages)
	for i := range out {
		if !clearSet[i] {
			continue
		}
		msg := out[i]
		if len(msg.Content) <= cfg.MicroCompactMinChars {
			continue
		}
		if isAlreadyClearedOrSpilled(msg.Content) {
			continue
		}
		out[i] = Message{
			Role:       msg.Role,
			Content:    toolResultClearedMessage,
			ToolCallID: msg.ToolCallID,
			ToolName:   msg.ToolName,
		}
	}
	return out
}

func applyToolResultBudget(messages []Message, cfg GovernorConfig) []Message {
	out := make([]Message, len(messages))
	copy(out, messages)
	for i, msg := range out {
		if msg.Role != llm.RoleTool {
			continue
		}
		if isAlreadyClearedOrSpilled(msg.Content) {
			continue
		}
		if len(msg.Content) > cfg.ToolResultBudget {
			out[i] = budgetOneToolResult(msg, cfg)
		}
	}
	return applyToolResultTurnBudget(out, cfg)
}

func budgetOneToolResult(msg Message, cfg GovernorConfig) Message {
	if cfg.ToolResultStore == nil {
		truncated := msg.Content
		if len(truncated) > cfg.ToolResultBudget {
			truncated = truncated[:cfg.ToolResultBudget]
		}
		suffix := fmt.Sprintf("\n[... truncated %d chars by context manager]", len(msg.Content)-len(truncated))
		return Message{
			Role:       msg.Role,
			Content:    truncated + suffix,
			ToolCallID: msg.ToolCallID,
			ToolName:   msg.ToolName,
		}
	}
	return spillToolResult(msg, cfg)
}

func applyToolResultTurnBudget(messages []Message, cfg GovernorConfig) []Message {
	if cfg.ToolResultTurnBudget <= 0 || cfg.ToolResultStore == nil {
		return messages
	}
	latest := latestToolTurnCallIDs(messages)
	if len(latest) == 0 {
		return messages
	}
	total := 0
	candidates := []int{}
	for i, msg := range messages {
		if msg.Role != llm.RoleTool || !latest[msg.ToolCallID] {
			continue
		}
		total += len(msg.Content)
		if !isAlreadyClearedOrSpilled(msg.Content) {
			candidates = append(candidates, i)
		}
	}
	if total <= cfg.ToolResultTurnBudget {
		return messages
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		li, ri := candidates[i], candidates[j]
		if len(messages[li].Content) == len(messages[ri].Content) {
			return li < ri
		}
		return len(messages[li].Content) > len(messages[ri].Content)
	})
	out := make([]Message, len(messages))
	copy(out, messages)
	for _, idx := range candidates {
		before := len(out[idx].Content)
		out[idx] = spillToolResult(out[idx], cfg)
		total += len(out[idx].Content) - before
		if total <= cfg.ToolResultTurnBudget {
			break
		}
	}
	return out
}

func latestToolTurnCallIDs(messages []Message) map[string]bool {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != llm.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		ids := make(map[string]bool, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			ids[call.ID] = true
		}
		return ids
	}
	return nil
}

func spillToolResult(msg Message, cfg GovernorConfig) Message {
	displayPath, err := cfg.ToolResultStore.Save(msg.ToolCallID, msg.Content)
	if err != nil {
		return Message{
			Role:       msg.Role,
			Content:    fmt.Sprintf("[tool result truncated by context manager]\nFull output could not be saved: %v", err),
			ToolCallID: msg.ToolCallID,
			ToolName:   msg.ToolName,
		}
	}
	previewChars := cfg.ToolResultPreviewChars
	if previewChars > len(msg.Content) {
		previewChars = len(msg.Content)
	}
	preview := msg.Content[:previewChars]
	return Message{
		Role: msg.Role,
		Content: fmt.Sprintf(
			"<persisted-output>\nOutput too large (%d chars). Full output saved to: %s\n\nPreview:\n%s\n</persisted-output>",
			len(msg.Content),
			displayPath,
			preview,
		),
		ToolCallID: msg.ToolCallID,
		ToolName:   msg.ToolName,
	}
}

func isAlreadyClearedOrSpilled(content string) bool {
	if content == toolResultClearedMessage {
		return true
	}
	if strings.HasPrefix(content, "<persisted-output>") {
		return true
	}
	return strings.Contains(content, "[tool result truncated by context manager]") &&
		strings.Contains(content, "Full output saved to:")
}

func isSpilledToolResult(content string) bool {
	return isAlreadyClearedOrSpilled(content) && content != toolResultClearedMessage
}

func countChangedToolResults(before, after []Message) int {
	limit := len(before)
	if len(after) < limit {
		limit = len(after)
	}
	changed := 0
	for i := 0; i < limit; i++ {
		if before[i].Role == llm.RoleTool && after[i].Role == llm.RoleTool && before[i].Content != after[i].Content {
			changed++
		}
	}
	return changed
}
