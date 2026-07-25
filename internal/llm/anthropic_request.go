package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

func buildAnthropicRequest(model string, maxTokens int, temperature float32, thinkingEnabled bool, thinkingBudget int, messages []Message, tools []ToolSpec) (map[string]any, error) {
	req := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"stream":     true,
	}
	if thinkingEnabled {
		// Extended thinking uses the Anthropic "thinking_delta" stream. The
		// protocol requires temperature == 1 (or omitted) while thinking is on,
		// so we skip the temperature field entirely in this mode rather than
		// letting a configured value trigger a 400.
		budget := thinkingBudget
		if budget < 1024 {
			budget = 4096
		}
		req["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": budget,
		}
	} else if temperature > 0 {
		req["temperature"] = temperature
	}

	var systemParts []string
	var wireMessages []map[string]any
	var pendingToolResults []map[string]any
	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		wireMessages = append(wireMessages, map[string]any{
			"role":    "user",
			"content": pendingToolResults,
		})
		pendingToolResults = nil
	}
	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			flushToolResults()
			if msg.Content != "" {
				systemParts = append(systemParts, msg.Content)
			}
		case RoleUser:
			flushToolResults()
			wireMessages = append(wireMessages, map[string]any{"role": "user", "content": msg.Content})
		case RoleAssistant:
			flushToolResults()
			var content []map[string]any
			if msg.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": msg.Content})
			}
			for _, call := range msg.ToolCalls {
				var input any = map[string]any{}
				if strings.TrimSpace(call.Arguments) != "" {
					if err := json.Unmarshal([]byte(call.Arguments), &input); err != nil {
						return nil, fmt.Errorf("decode tool call arguments for %s: %w", call.Name, err)
					}
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    call.ID,
					"name":  call.Name,
					"input": input,
				})
			}
			if len(content) == 1 && msg.Content != "" && len(msg.ToolCalls) == 0 {
				wireMessages = append(wireMessages, map[string]any{"role": "assistant", "content": msg.Content})
			} else if len(content) > 0 {
				wireMessages = append(wireMessages, map[string]any{"role": "assistant", "content": content})
			}
		case RoleTool:
			pendingToolResults = append(pendingToolResults, map[string]any{
				"type":        "tool_result",
				"tool_use_id": msg.ToolCallID,
				"content":     msg.Content,
			})
		}
	}
	flushToolResults()
	if len(systemParts) > 0 {
		req["system"] = strings.Join(systemParts, "\n\n")
	}
	req["messages"] = wireMessages
	if len(tools) > 0 {
		wireTools := make([]map[string]any, 0, len(tools))
		for _, spec := range tools {
			wireTools = append(wireTools, map[string]any{
				"name":         spec.Name,
				"description":  spec.Description,
				"input_schema": spec.Schema,
			})
		}
		req["tools"] = wireTools
	}
	return req, nil
}

// applyCacheBreakpoints injects Anthropic prompt-cache breakpoints into an
// already-built request map: the system prompt (as a content-block array with
// an ephemeral breakpoint) and the last tool definition. These are the two
// largest fully-stable prefixes, so they cache reliably. The system breakpoint
// only pays off once volatile sections are moved out of the system prompt
// (roadmap A2/A3); the tool breakpoint caches immediately regardless.
//
// This is a post-processing step on buildAnthropicRequest's output so the wire
// builder stays a pure function with a stable signature.
func applyCacheBreakpoints(req map[string]any) {
	if sys, ok := req["system"].(string); ok && sys != "" {
		req["system"] = []map[string]any{{
			"type":          "text",
			"text":          sys,
			"cache_control": map[string]any{"type": "ephemeral"},
		}}
	}
	if tools, ok := req["tools"].([]map[string]any); ok && len(tools) > 0 {
		last := tools[len(tools)-1]
		if _, has := last["cache_control"]; !has {
			last["cache_control"] = map[string]any{"type": "ephemeral"}
		}
	}
}
