package builtin

import (
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/session"
)

// sessionStats is a compact summary of one session's recorded event log, used by
// /session and /context to show real counts instead of an opaque id.
type sessionStats struct {
	id            string
	userMsgs      int
	assistantMsgs int
	toolCalls     int
}

func (s sessionStats) totalMessages() int {
	return s.userMsgs + s.assistantMsgs
}

// summarizeEvents counts messages and tool results across a session's event log.
func summarizeEvents(events []session.Event) sessionStats {
	var st sessionStats
	for _, e := range events {
		if e.Type == "message" && e.Message != nil {
			switch e.Message.Role {
			case session.RoleUser:
				st.userMsgs++
			case session.RoleAssistant:
				st.assistantMsgs++
			}
		}
		if e.Type == "tool_result" && e.ToolCall != nil {
			st.toolCalls++
		}
	}
	return st
}

// renderPanelText flattens a panel back to plain "key: value" lines, used as the
// canonical Output (logs/snapshots) alongside the TUI's bordered Panel render.
func renderPanelText(p *command.Panel) string {
	var lines []string
	for _, sec := range p.Sections {
		for _, row := range sec.Rows {
			lines = append(lines, fmt.Sprintf("%s: %s", row.Key, row.Value))
		}
	}
	return strings.Join(lines, "\n")
}

func currentSessionStats(env command.Env) sessionStats {
	st := sessionStats{id: env.SessionID}
	if env.Sessions == nil || env.SessionID == "" {
		return st
	}
	events, err := env.Sessions.Load(env.SessionID)
	if err != nil {
		return st
	}
	st = summarizeEvents(events)
	st.id = env.SessionID
	return st
}
