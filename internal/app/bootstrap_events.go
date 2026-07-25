package app

import (
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/subagent"
)

func newSubagentEventSink(currentApp func() *App) subagent.EventSink {
	return func(event subagent.Event) {
		application := currentApp()
		if application == nil {
			return
		}
		converted := agent.Event{
			Type:       mapSubagentEventType(event.Type),
			Message:    firstNonEmpty(event.Message, event.Description, event.Status),
			ToolName:   string(event.AgentType),
			ToolCallID: event.TaskID,
			Output:     event.Message,
			Error:      event.Error,
			Generation: event.Generation,
			CreatedAt:  event.CreatedAt,
		}
		if converted.CreatedAt.IsZero() {
			converted.CreatedAt = time.Now()
		}
		if event.Type == subagent.EventTranscriptChunk && event.TranscriptKind == "reasoning" {
			converted.Type = agent.EventSubagentReasoningChunk
		}
		if event.Type == subagent.EventToolCall {
			converted.ToolName = event.ToolName
			converted.Input = event.ToolInput
			converted.Output = event.ToolOutput
			converted.Error = event.ToolError
			converted.Message = firstNonEmpty(event.ToolStatus, event.Message)
		}
		application.recordSubagentEvent(converted)
	}
}

func mapSubagentEventType(eventType subagent.EventType) agent.EventType {
	switch eventType {
	case subagent.EventStarted:
		return agent.EventSubagentStarted
	case subagent.EventProgress:
		return agent.EventSubagentProgress
	case subagent.EventFinished:
		return agent.EventSubagentFinished
	case subagent.EventFailed:
		return agent.EventSubagentFailed
	case subagent.EventToolCall:
		return agent.EventSubagentToolCall
	case subagent.EventTranscriptChunk:
		return agent.EventSubagentModelChunk
	default:
		return agent.EventSubagentProgress
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
