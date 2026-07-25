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
	Metadata    map[string]any
	// Messages carries the child agent.Loop's full message history so a future
	// resume_task_id can continue the child context. It is not part of the
	// model-facing task result (formatTaskResult ignores it).
	Messages []llm.Message
}
