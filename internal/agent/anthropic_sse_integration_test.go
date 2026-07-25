package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/tool/builtin"
)

func TestLoopWithAnthropicSSEToolUseInputJSONDeltaReadsCompletePath(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "README.md"), []byte("test readme"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	var requestBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		requestBodies = append(requestBodies, string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if len(requestBodies) == 1 {
			writeSSE(t, w, []string{
				`event: content_block_start`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_read","name":"read_file","input":{}}}`,
				``,
				`event: content_block_delta`,
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"README.md\"}"}}`,
				``,
				`event: content_block_stop`,
				`data: {"type":"content_block_stop","index":0}`,
				``,
				`event: message_stop`,
				`data: {"type":"message_stop"}`,
				``,
			})
			return
		}
		writeSSE(t, w, []string{
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"README summarized"}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		})
	}))
	defer server.Close()
	t.Setenv("ANTHROPIC_SSE_TEST_KEY", "test-key")

	registry := tool.NewRegistry()
	if err := registry.Register(builtin.NewReadFileTool()); err != nil {
		t.Fatal(err)
	}
	client := llm.NewAnthropicCompatibleClient(llm.AnthropicCompatibleConfig{
		BaseURL:   server.URL,
		APIKeyEnv: "ANTHROPIC_SSE_TEST_KEY",
		Model:     "test-model",
		MaxTokens: 64,
	})
	loop := NewLoop(LoopConfig{MaxSteps: 4}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	result, err := loop.Run(context.Background(), "read README")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "README summarized" {
		t.Fatalf("unexpected final answer %q", result.FinalAnswer)
	}
	if len(requestBodies) != 2 {
		t.Fatalf("expected two model calls without hitting max steps, got %d", len(requestBodies))
	}
	if strings.Contains(requestBodies[1], `"input":{}`) {
		t.Fatalf("second request contained empty tool input, body: %s", requestBodies[1])
	}
	if !strings.Contains(requestBodies[1], `"path":"README.md"`) {
		t.Fatalf("expected complete read_file path in second request, body: %s", requestBodies[1])
	}
	if !traceHasReadFileResult(result.Trace.Events) {
		t.Fatalf("expected read_file result event in trace, got %#v", result.Trace.Events)
	}
}

func writeSSE(t *testing.T, w http.ResponseWriter, lines []string) {
	t.Helper()
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			t.Fatal(err)
		}
	}
}

func traceHasReadFileResult(events []Event) bool {
	for _, event := range events {
		if event.Type != EventToolResult || event.ToolName != "read_file" {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(event.Input), &parsed); err != nil {
			return false
		}
		return parsed["path"] == "README.md" && event.Output != ""
	}
	return false
}
