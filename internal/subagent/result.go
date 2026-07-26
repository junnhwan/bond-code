package subagent

import (
	"time"

	"github.com/junnhwan/bond-code/internal/llm"
)

// SubagentResult holds the result of a completed subagent execution
type SubagentResult struct {
	TaskID      string
	Description string
	Prompt      string
	AgentType   AgentType
	FinalAnswer string
	Error       string
	Status      string // "completed" / "failed" / "cancelled"
	StartTime   time.Time
	EndTime     time.Time
	Iterations  int
	// ToolCount is how many tool results the child produced. Zero with
	// status=completed is an "empty completion": text-only answer, not applied work.
	ToolCount int
	Metadata  map[string]any
	// Messages carries the child agent.Loop's full message history so a future
	// resume_task_id can continue the child context. It is not part of the
	// model-facing task result (formatTaskResult ignores it).
	Messages []llm.Message
}

// IsEmptyCompletion reports a finished child that never executed tools.
// Callers should treat the summary as unverified text, not applied work.
func (r *SubagentResult) IsEmptyCompletion() bool {
	if r == nil {
		return false
	}
	return r.Status == "completed" && r.ToolCount == 0
}

// DurationMs returns wall-clock runtime in milliseconds when both ends are set.
func (r *SubagentResult) DurationMs() int64 {
	if r == nil || r.StartTime.IsZero() || r.EndTime.IsZero() {
		return 0
	}
	d := r.EndTime.Sub(r.StartTime)
	if d < 0 {
		return 0
	}
	return d.Milliseconds()
}
