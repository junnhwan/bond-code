package contextx

import (
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/llm"
)

// Max chars of each tool result when serializing for summarization (Pi default 2000).
const toolResultSerializeMaxChars = 2000

// SerializeConversation renders messages as labeled prose so the summarizer
// does not treat them as a live chat to continue (Pi serializeConversation).
func SerializeConversation(messages []Message) string {
	var parts []string
	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleSystem:
			continue
		case llm.RoleUser:
			if text := strings.TrimSpace(msg.Content); text != "" {
				parts = append(parts, "[User]: "+text)
			}
		case llm.RoleAssistant:
			if text := strings.TrimSpace(msg.Content); text != "" {
				parts = append(parts, "[Assistant]: "+text)
			}
			if len(msg.ToolCalls) > 0 {
				calls := make([]string, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					arg := strings.TrimSpace(tc.Arguments)
					if arg == "" {
						calls = append(calls, tc.Name+"()")
					} else {
						calls = append(calls, fmt.Sprintf("%s(%s)", tc.Name, truncateForSummary(arg, 400)))
					}
				}
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(calls, "; "))
			}
		case llm.RoleTool:
			name := msg.ToolName
			if name == "" {
				name = "tool"
			}
			body := truncateForSummary(strings.TrimSpace(msg.Content), toolResultSerializeMaxChars)
			if body == "" {
				continue
			}
			parts = append(parts, fmt.Sprintf("[Tool result %s]: %s", name, body))
		}
	}
	return strings.Join(parts, "\n\n")
}

func truncateForSummary(text string, maxChars int) string {
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	omitted := len(text) - maxChars
	return text[:maxChars] + fmt.Sprintf("\n\n[... %d more characters truncated]", omitted)
}
