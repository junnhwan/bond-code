package subagent

import (
	"fmt"
	"strings"
)

const legacyStepBudgetFallbackPrefix = "step budget reached after tool use; returning the latest available tool result"

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
