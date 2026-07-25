package contextx

import "github.com/junnhwan/bond-code/internal/llm"

// ensureIntegrity enforces tool_use ↔ tool_result pairing:
// drop orphan tool results, then backfill missing results with a synthetic envelope.
// This is the hard invariant layer (not a product "feature" layer).
func ensureIntegrity(messages []Message) []Message {
	return backfillMissingToolResults(dropOrphanToolResults(messages))
}

func dropOrphanToolResults(messages []Message) []Message {
	validIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == llm.RoleAssistant {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					validIDs[tc.ID] = true
				}
			}
		}
	}
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == llm.RoleTool {
			if validIDs[msg.ToolCallID] {
				out = append(out, msg)
			}
			continue
		}
		out = append(out, msg)
	}
	return out
}

func backfillMissingToolResults(messages []Message) []Message {
	received := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == llm.RoleTool {
			received[msg.ToolCallID] = true
		}
	}
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, msg)
		if msg.Role != llm.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if received[tc.ID] {
				continue
			}
			out = append(out, Message{
				Role:       llm.RoleTool,
				Content:    "[tool result missing, filled by context manager]",
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
			})
			received[tc.ID] = true
		}
	}
	return out
}
