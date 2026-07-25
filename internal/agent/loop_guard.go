package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/junnhwan/bond-code/internal/llm"
)

const (
	repeatedToolCallGuardTemplate = "loop_guard: repeated identical tool call blocked after %d attempts with the same result. Explain the result to the user or choose a different action."
	tooManyToolCallsGuardMessage  = "loop_guard: too many tool calls in one step; this call was not executed."
)

// loopGuard detects no-progress tool loops. A call only counts as a repeat after
// it has executed and produced the same model-visible result as the immediately
// preceding identical call. A different call or a changed result starts a new
// progress epoch, which permits normal test-edit-test and read-edit-read flows.
//
// Check and RecordResult are intentionally separate: Check runs before tool
// execution to preserve the tool-use/result pairing rules, while RecordResult
// advances the guard only after a real result exists.
type loopGuard struct {
	cfg LoopConfig

	lastCallFingerprint   string
	lastResultFingerprint string
	consecutiveNoProgress int
}

type loopGuardDecision struct {
	Guarded bool
	Stop    bool
	Output  string
}

func newLoopGuard(cfg LoopConfig) *loopGuard {
	return &loopGuard{cfg: cfg}
}

func (g *loopGuard) Check(call llm.ToolCall, callIndex int) loopGuardDecision {
	if callIndex >= g.cfg.MaxToolCallsPerStep {
		return loopGuardDecision{Guarded: true, Output: tooManyToolCallsGuardMessage}
	}
	fingerprint := toolCallFingerprint(call)
	if fingerprint == g.lastCallFingerprint && g.consecutiveNoProgress >= g.cfg.MaxRepeatedToolCalls {
		return loopGuardDecision{
			Guarded: true,
			Stop:    true,
			Output:  fmt.Sprintf(repeatedToolCallGuardTemplate, g.cfg.MaxRepeatedToolCalls),
		}
	}
	return loopGuardDecision{}
}

// RecordResult records one actually executed tool call in model-visible order.
// result must be the same envelope content appended as the tool message.
func (g *loopGuard) RecordResult(call llm.ToolCall, result string) {
	callFingerprint := toolCallFingerprint(call)
	resultFingerprint := textFingerprint(result)
	if callFingerprint == g.lastCallFingerprint && resultFingerprint == g.lastResultFingerprint {
		g.consecutiveNoProgress++
		return
	}
	g.lastCallFingerprint = callFingerprint
	g.lastResultFingerprint = resultFingerprint
	g.consecutiveNoProgress = 1
}

func textFingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func toolCallFingerprint(call llm.ToolCall) string {
	args := call.Arguments
	var obj map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &obj); err == nil && obj != nil {
		if canonical, err := json.Marshal(obj); err == nil {
			args = string(canonical)
		}
	}
	return call.Name + "\n" + args
}
