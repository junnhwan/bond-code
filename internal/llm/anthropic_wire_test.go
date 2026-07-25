package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAnthropicWireClientSendsSystemAsStringAndStreamsText(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		requestBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer server.Close()
	t.Setenv("WIRE_TEST_KEY", "test-key")

	client := NewAnthropicCompatibleClient(AnthropicCompatibleConfig{
		BaseURL:   server.URL,
		APIKeyEnv: "WIRE_TEST_KEY",
		Model:     "test-model",
		MaxTokens: 32,
	})

	chunks, errs := client.Stream(context.Background(), []Message{
		{Role: RoleSystem, Content: "system prompt"},
		{Role: RoleUser, Content: "hello"},
	}, nil)
	var output string
	for chunk := range chunks {
		output += chunk.Content
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if output != "hello" {
		t.Fatalf("expected streamed hello, got %q", output)
	}
	if !strings.Contains(requestBody, `"system":"system prompt"`) {
		t.Fatalf("expected system as string, got body %s", requestBody)
	}
	if os.Getenv("WIRE_TEST_KEY") == "" {
		t.Fatal("test key unexpectedly unset")
	}
}

func TestAnthropicWireRequestIncludesAssistantToolUseBeforeToolResult(t *testing.T) {
	req, err := buildAnthropicRequest("test-model", 32, 0, false, 0, []Message{
		{Role: RoleUser, Content: "read"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{
			ID:        "call-read",
			Name:      "read_file",
			Arguments: `{"path":"README.md"}`,
		}}},
		{Role: RoleTool, Content: "file content", ToolCallID: "call-read", ToolName: "read_file"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, `"type":"tool_use"`) {
		t.Fatalf("expected assistant tool_use in request body, got %s", got)
	}
	if !strings.Contains(got, `"id":"call-read"`) || !strings.Contains(got, `"name":"read_file"`) {
		t.Fatalf("expected tool_use id and name in request body, got %s", got)
	}
	if !strings.Contains(got, `"type":"tool_result"`) || !strings.Contains(got, `"tool_use_id":"call-read"`) {
		t.Fatalf("expected matching tool_result in request body, got %s", got)
	}
	if strings.Index(got, `"type":"tool_use"`) > strings.Index(got, `"type":"tool_result"`) {
		t.Fatalf("expected tool_use before tool_result, got %s", got)
	}
}

func TestAnthropicWireRequestGroupsConsecutiveToolResultsInOrder(t *testing.T) {
	req, err := buildAnthropicRequest("test-model", 32, 0, false, 0, []Message{
		{Role: RoleUser, Content: "inspect"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "call-read", Name: "read_file", Arguments: `{"path":"README.md"}`},
			{ID: "call-list", Name: "list_dir", Arguments: `{"path":"."}`},
		}},
		{Role: RoleTool, Content: "file content", ToolCallID: "call-read", ToolName: "read_file"},
		{Role: RoleTool, Content: "dir content", ToolCallID: "call-list", ToolName: "list_dir"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	messages, ok := req["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected messages shape: %#v", req["messages"])
	}
	if len(messages) != 3 {
		t.Fatalf("expected user, assistant, grouped tool-result user messages, got %#v", messages)
	}
	assistantContent, ok := messages[1]["content"].([]map[string]any)
	if !ok || len(assistantContent) != 2 {
		t.Fatalf("expected assistant message with two tool_use blocks, got %#v", messages[1])
	}
	toolContent, ok := messages[2]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected grouped tool_result content blocks, got %#v", messages[2])
	}
	if len(toolContent) != 2 {
		t.Fatalf("expected two grouped tool_result blocks, got %#v", toolContent)
	}
	if toolContent[0]["tool_use_id"] != "call-read" || toolContent[1]["tool_use_id"] != "call-list" {
		t.Fatalf("tool_result blocks out of order: %#v", toolContent)
	}
}

func TestAnthropicSSEEmitsToolCallAfterInputJSONDeltaCompletes(t *testing.T) {
	sse := strings.Join([]string{
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_read","name":"read_file","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"README.md\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
	}, "\n")

	chunks := make(chan Chunk, 8)
	if err := parseAnthropicSSE(context.Background(), strings.NewReader(sse), chunks); err != nil {
		t.Fatal(err)
	}
	close(chunks)

	var toolCalls []ToolCall
	for chunk := range chunks {
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected one complete tool call emitted at content_block_stop, got %#v", toolCalls)
	}
	got := toolCalls[0]
	if got.ID != "toolu_read" || got.Name != "read_file" || got.Arguments != `{"path":"README.md"}` {
		t.Fatalf("unexpected tool call: %#v", got)
	}
	if got.Arguments == "{}" {
		t.Fatalf("tool call was emitted before input_json_delta completed: %#v", got)
	}
}

func TestAnthropicSSETracksToolUseByIndex(t *testing.T) {
	sse := strings.Join([]string{
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_read","name":"read_file","input":{}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_list","name":"list_dir","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\".\"}"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"README.md\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
	}, "\n")

	chunks := make(chan Chunk, 8)
	if err := parseAnthropicSSE(context.Background(), strings.NewReader(sse), chunks); err != nil {
		t.Fatal(err)
	}
	close(chunks)

	var toolCalls []ToolCall
	for chunk := range chunks {
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}
	}
	if len(toolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %#v", toolCalls)
	}
	if toolCalls[0].ID != "toolu_read" || toolCalls[0].Name != "read_file" || toolCalls[0].Arguments != `{"path":"README.md"}` {
		t.Fatalf("unexpected first tool call: %#v", toolCalls[0])
	}
	if toolCalls[1].ID != "toolu_list" || toolCalls[1].Name != "list_dir" || toolCalls[1].Arguments != `{"path":"."}` {
		t.Fatalf("unexpected second tool call: %#v", toolCalls[1])
	}
}

func TestBuildAnthropicRequestEnablesThinkingAndOmitsTemperature(t *testing.T) {
	req, err := buildAnthropicRequest("glm-5.1", 4096, 0.7, true, 2048, []Message{
		{Role: RoleUser, Content: "hi"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	for _, want := range []string{`"thinking"`, `"type":"enabled"`, `"budget_tokens":2048`} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %s in thinking-enabled body, got %s", want, got)
		}
	}
	if strings.Contains(got, `"temperature"`) {
		t.Fatalf("temperature must be omitted when thinking is on, got %s", got)
	}
}

func TestBuildAnthropicRequestClampsThinkingBudget(t *testing.T) {
	req, err := buildAnthropicRequest("glm-5.1", 4096, 0, true, 100, []Message{
		{Role: RoleUser, Content: "hi"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	// budget below the protocol floor (1024) is raised to the default.
	if !strings.Contains(string(body), `"budget_tokens":4096`) {
		t.Fatalf("expected clamped budget_tokens 4096, got %s", body)
	}
}

func TestBuildAnthropicRequestOmitsThinkingWhenDisabled(t *testing.T) {
	req, err := buildAnthropicRequest("glm-5.1", 4096, 0.7, false, 0, []Message{
		{Role: RoleUser, Content: "hi"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if strings.Contains(got, `"thinking"`) {
		t.Fatalf("expected no thinking block when disabled, got %s", got)
	}
	if !strings.Contains(got, `"temperature":0.7`) {
		t.Fatalf("expected temperature preserved when thinking off, got %s", got)
	}
}

func TestApplyCacheBreakpointsMarksSystemAndLastTool(t *testing.T) {
	req := map[string]any{
		"system": "system prompt",
		"tools": []map[string]any{
			{"name": "read_file", "input_schema": map[string]any{"type": "object"}},
			{"name": "list_dir", "input_schema": map[string]any{"type": "object"}},
		},
	}
	applyCacheBreakpoints(req)

	// System becomes a content-block array with an ephemeral breakpoint.
	sysBlocks, ok := req["system"].([]map[string]any)
	if !ok || len(sysBlocks) != 1 {
		t.Fatalf("expected system as single content-block array, got %#v", req["system"])
	}
	if sysBlocks[0]["cache_control"] == nil {
		t.Fatalf("expected cache_control on system block, got %#v", sysBlocks[0])
	}
	// Only the last tool carries cache_control.
	tools, ok := req["tools"].([]map[string]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("expected two tools, got %#v", req["tools"])
	}
	if tools[0]["cache_control"] != nil {
		t.Fatalf("first tool must not carry cache_control, got %#v", tools[0])
	}
	if tools[1]["cache_control"] == nil {
		t.Fatalf("expected cache_control on last tool, got %#v", tools[1])
	}
}

func TestApplyCacheBreakpointsIsIdempotentAndSafeOnEmpty(t *testing.T) {
	// No system / no tools: must not panic and must not synthesize keys.
	req := map[string]any{"model": "x"}
	applyCacheBreakpoints(req)
	if _, ok := req["system"]; ok {
		t.Fatalf("did not expect a system key to appear, got %#v", req["system"])
	}

	// Re-applying to an already-cached request must not double-wrap or duplicate.
	cached := map[string]any{
		"system": []map[string]any{{"type": "text", "text": "s", "cache_control": map[string]any{"type": "ephemeral"}}},
		"tools":  []map[string]any{{"name": "a", "cache_control": map[string]any{"type": "ephemeral"}}},
	}
	applyCacheBreakpoints(cached)
	sysBlocks := cached["system"].([]map[string]any)
	if len(sysBlocks) != 1 {
		t.Fatalf("idempotent re-apply must not wrap again, got %#v", cached["system"])
	}
}
