package subagent

import (
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/llm"
)

const legacyStepBudgetFallbackPrefix = "step budget reached after tool use; returning the latest available tool result"

// emptyCompletionNotice is injected into parent-facing task results when a child
// finishes without any tool use so the main agent does not treat status=completed
// as "work landed on disk". Keep it short so batch truncation still preserves
// child XML structure.
const emptyCompletionNotice = "⚠ empty completion: no tools executed — treat summary as unverified text/plan only, not applied work."

// rateLimitNotice is appended when a child fails after provider 429 exhaustion.
const rateLimitNotice = "Rate limited by the model provider. Wait before retrying, reduce parallel subagents, or lower request rate."

// validateUsableFinalAnswer is the final status boundary for child agents. It
// deliberately rejects known transport/fallback artifacts even if an upstream
// loop accidentally reports nil error, so they cannot be persisted as a
// completed AgentTask.
func validateUsableFinalAnswer(answer string) error {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return fmt.Errorf("subagent final answer is unusable: empty answer")
	}
	lower := strings.ToLower(answer)
	if startsWithToolProtocol(answer) {
		return fmt.Errorf("subagent final answer is unusable: contains tool-protocol markup instead of an answer")
	}
	if strings.HasPrefix(lower, legacyStepBudgetFallbackPrefix) {
		return fmt.Errorf("subagent final answer is unusable: contains legacy raw-tool fallback")
	}
	return nil
}

func startsWithToolProtocol(answer string) bool {
	candidate := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(answer, "\r\n", "\n")))
	if strings.HasPrefix(candidate, "```") {
		if newline := strings.IndexByte(candidate, '\n'); newline >= 0 {
			candidate = strings.TrimSpace(candidate[newline+1:])
		}
	}
	for _, marker := range []string{"<tool_call", "<arg_key", "<arg_value", "<function_calls", "<invoke"} {
		if strings.HasPrefix(candidate, marker) {
			return true
		}
	}
	return false
}

// countToolResults counts RoleTool messages (one result per executed tool call).
func countToolResults(messages []llm.Message) int {
	n := 0
	for _, msg := range messages {
		if msg.Role == llm.RoleTool {
			n++
		}
	}
	return n
}

// isRateLimitText reports provider rate-limit failures from error strings
// (APIError.Error() form and common gateway wording).
func isRateLimitText(errText string) bool {
	lower := strings.ToLower(strings.TrimSpace(errText))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "429") {
		return true
	}
	return strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "请求过于频繁") ||
		strings.Contains(lower, "1分钟内最多请求")
}

// annotateResultMetadata fills ToolCount + Metadata observability fields used by
// the task tool envelope, parent model, and TUI consumers.
func annotateResultMetadata(result *SubagentResult) {
	if result == nil {
		return
	}
	if result.ToolCount == 0 && len(result.Messages) > 0 {
		result.ToolCount = countToolResults(result.Messages)
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["tool_count"] = result.ToolCount
	result.Metadata["iterations"] = result.Iterations
	if ms := result.DurationMs(); ms > 0 {
		result.Metadata["duration_ms"] = ms
	}
	if result.IsEmptyCompletion() {
		result.Metadata["empty_completion"] = true
	}
	if result.Status == "failed" && isRateLimitText(result.Error) {
		result.Metadata["rate_limited"] = true
	}
}
