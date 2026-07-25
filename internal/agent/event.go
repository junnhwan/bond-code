package agent

import "time"

type EventType string

const (
	EventAgentStarted              EventType = "agent_started"
	EventModelChunk                EventType = "model_chunk"
	EventReasoningChunk            EventType = "reasoning_chunk"
	EventContextUpdated            EventType = "context_updated"
	EventContextMeasured           EventType = "context_measured"
	EventCompactionStarted         EventType = "compaction_started"
	EventCompactionFinished        EventType = "compaction_finished"
	EventToolRequested             EventType = "tool_requested"
	EventToolConfirmationRequested EventType = "tool_confirmation_requested"
	EventToolApproved              EventType = "tool_approved"
	EventToolRejected              EventType = "tool_rejected"
	EventToolResult                EventType = "tool_result"
	EventLoopGuard                 EventType = "loop_guard"
	EventTextDegeneration          EventType = "text_degeneration"
	EventSubagentStarted           EventType = "subagent_started"
	EventSubagentProgress          EventType = "subagent_progress"
	EventSubagentFinished          EventType = "subagent_finished"
	EventSubagentFailed            EventType = "subagent_failed"
	EventSubagentCancelled         EventType = "subagent_cancelled"
	EventSubagentToolCall          EventType = "subagent_tool_call"
	EventSubagentModelChunk        EventType = "subagent_model_chunk"
	EventSubagentReasoningChunk    EventType = "subagent_reasoning_chunk"
	EventAgentFinished             EventType = "agent_finished"
	EventAgentError                EventType = "agent_error"
)

type Event struct {
	Type       EventType
	Message    string
	ToolName   string
	ToolCallID string
	Risk       string
	Input      string
	Output     string
	Error      string
	// ContextTokens / ContextMaxTokens carry the live context-window usage that
	// EventContextUpdated reports, so the UI does not have to parse Message.
	ContextTokens    int
	ContextMaxTokens int
	// MeasuredInputTokens / MeasuredOutputTokens carry the real token counts the
	// model API reported (message_start/message_delta), emitted via
	// EventContextMeasured. Unlike ContextTokens (a chars/3 estimate from the
	// governor), these are authoritative; MeasuredInputTokens is the true
	// context-window occupancy.
	MeasuredInputTokens  int
	MeasuredOutputTokens int
	// MeasuredUsageFinal is true on the final usage event for one model call.
	// Streaming providers may report incremental usage multiple times; consumers
	// that aggregate per-call totals should count only final events.
	MeasuredUsageFinal bool
	Generation         uint64
	CreatedAt          time.Time
}
