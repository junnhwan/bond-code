package subagent

import "time"

type EventType string

const (
	EventStarted  EventType = "subagent_started"
	EventProgress EventType = "subagent_progress"
	EventFinished EventType = "subagent_finished"
	EventFailed   EventType = "subagent_failed"
	// EventToolCall describes one tool invocation by a child agent: emitted
	// before execution (ToolStatus "running") and after it finishes
	// ("done"/"failed"), carrying the tool name, input and output so the TUI can
	// render the child's tool stream inside its own window.
	EventToolCall        EventType = "subagent_tool_call"
	EventTranscriptChunk EventType = "subagent_transcript_chunk"
)

type Event struct {
	Type           EventType
	TaskID         string
	Description    string
	AgentType      AgentType
	Status         string
	Message        string
	Error          string
	Iterations     int
	CreatedAt      time.Time
	Generation     uint64
	TranscriptKind string
	// Tool* fields are populated only when Type == EventToolCall. They describe
	// one tool invocation by the child agent; ToolStatus is "running" before
	// execution and "done"/"failed" after.
	ToolName   string
	ToolInput  string
	ToolOutput string
	ToolStatus string
	ToolError  string
}

type EventSink func(Event)
