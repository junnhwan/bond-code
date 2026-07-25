package app

import (
	"fmt"
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/session"
)

func (a *App) appendSessionAgentEvent(event agent.Event) error {
	if err := a.appendRawAgentEvent(event); err != nil {
		return err
	}
	switch event.Type {
	case agent.EventAgentFinished:
		if event.Message == "" {
			return nil
		}
		return a.appendSessionMessage(session.RoleAssistant, event.Message, event.CreatedAt)
	case agent.EventToolResult:
		return a.appendSessionToolCall(event)
	default:
		return nil
	}
}

func (a *App) appendRawAgentEvent(event agent.Event) error {
	if a.Sessions == nil || a.SessionID == "" {
		return nil
	}
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	return a.Sessions.Append(session.Event{
		SessionID: a.SessionID,
		Type:      "agent_event",
		AgentEvent: &session.AgentEvent{
			Type:       string(event.Type),
			Message:    event.Message,
			ToolName:   event.ToolName,
			ToolCallID: event.ToolCallID,
			Risk:       event.Risk,
			Input:      event.Input,
			Output:     event.Output,
			Error:      event.Error,
			CreatedAt:  createdAt,
		},
		CreatedAt: createdAt,
	})
}

func (a *App) appendSessionMessage(role session.Role, content string, createdAt time.Time) error {
	if a.Sessions == nil || a.SessionID == "" {
		return nil
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	return a.Sessions.Append(session.Event{
		SessionID: a.SessionID,
		Type:      "message",
		Message: &session.Message{
			Role:      role,
			Content:   content,
			CreatedAt: createdAt,
		},
		CreatedAt: createdAt,
	})
}

func (a *App) appendSessionToolCall(event agent.Event) error {
	if a.Sessions == nil || a.SessionID == "" {
		return nil
	}
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	return a.Sessions.Append(session.Event{
		SessionID: a.SessionID,
		Type:      string(event.Type),
		ToolCall: &session.ToolCall{
			ID:        event.ToolCallID,
			Name:      event.ToolName,
			Input:     event.Input,
			Risk:      event.Risk,
			Approved:  event.Error == "",
			Output:    event.Output,
			Error:     event.Error,
			CreatedAt: createdAt,
		},
		CreatedAt: createdAt,
	})
}

func newSessionID() string {
	return fmt.Sprintf("session-%s", time.Now().UTC().Format("20060102-150405.000000000"))
}
