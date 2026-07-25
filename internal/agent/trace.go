package agent

import "time"

type Trace struct {
	Events []Event
}

func (t *Trace) Add(eventType EventType, message string, toolName string) {
	t.AddEvent(Event{Type: eventType, Message: message, ToolName: toolName})
}

func (t *Trace) AddEvent(event Event) Event {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	t.Events = append(t.Events, event)
	return event
}
