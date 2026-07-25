package llm

import (
	"context"
	"strings"
	"testing"
)

// TestAnthropicWireParsesUsage verifies message_start.input_tokens (plus cache
// creation/read) and message_delta.output_tokens are decoded into Chunk.Usage,
// since that is the only source of real context-window occupancy.
func TestAnthropicWireParsesUsage(t *testing.T) {
	parser := newAnthropicSSEParser()
	chunks := make(chan Chunk, 4)

	start := `{"type":"message_start","message":{"usage":{"input_tokens":1200,"cache_creation_input_tokens":300,"cache_read_input_tokens":500}}}`
	if err := parser.emitEvent(context.Background(), start, chunks); err != nil {
		t.Fatalf("message_start: %v", err)
	}
	delta := `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}`
	if err := parser.emitEvent(context.Background(), delta, chunks); err != nil {
		t.Fatalf("message_delta: %v", err)
	}
	close(chunks)

	var input, output int
	for chunk := range chunks {
		if chunk.Usage == nil {
			continue
		}
		if chunk.Usage.InputTokens > 0 {
			input = chunk.Usage.InputTokens
		}
		if chunk.Usage.OutputTokens > 0 {
			output = chunk.Usage.OutputTokens
		}
	}
	// input_tokens + cache_creation + cache_read = 1200 + 300 + 500
	if input != 2000 {
		t.Fatalf("expected measured input 2000, got %d", input)
	}
	if output != 42 {
		t.Fatalf("expected measured output 42, got %d", output)
	}
}

// TestAnthropicWireGLMDeltaCarriesRealInput reproduces GLM's /api/anthropic
// gateway, which streams input_tokens=0 in message_start and only surfaces the
// real counts in message_delta. The parser must adopt the delta's input tokens,
// otherwise /status would permanently show "not measured yet".
func TestAnthropicWireGLMDeltaCarriesRealInput(t *testing.T) {
	parser := newAnthropicSSEParser()
	chunks := make(chan Chunk, 4)

	start := `{"type":"message_start","message":{"usage":{"input_tokens":0,"output_tokens":0}}}`
	if err := parser.emitEvent(context.Background(), start, chunks); err != nil {
		t.Fatalf("message_start: %v", err)
	}
	delta := `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":7,"output_tokens":16,"cache_read_input_tokens":0}}`
	if err := parser.emitEvent(context.Background(), delta, chunks); err != nil {
		t.Fatalf("message_delta: %v", err)
	}
	close(chunks)

	var input, output int
	for chunk := range chunks {
		if chunk.Usage == nil {
			continue
		}
		if chunk.Usage.InputTokens > 0 {
			input = chunk.Usage.InputTokens
		}
		if chunk.Usage.OutputTokens > 0 {
			output = chunk.Usage.OutputTokens
		}
	}
	if input != 7 {
		t.Fatalf("expected measured input 7 from delta, got %d", input)
	}
	if output != 16 {
		t.Fatalf("expected measured output 16 from delta, got %d", output)
	}
}

// TestAnthropicWireCapturesStopReasonOnDone reproduces a max_tokens-truncated
// stream: thinking consumed the entire output budget, so no text/tool_use block
// was emitted. The parser must still surface delta.stop_reason on the terminal
// Done chunk so the agent loop can tell truncation from a genuine end_turn and
// retry instead of silently ending the run.
func TestAnthropicWireCapturesStopReasonOnDone(t *testing.T) {
	sse := `data: {"type":"message_start","message":{"usage":{"input_tokens":100}}}

data: {"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":4096}}

`
	chunks := make(chan Chunk, 8)
	if err := parseAnthropicSSE(context.Background(), strings.NewReader(sse), chunks); err != nil {
		t.Fatalf("parseAnthropicSSE: %v", err)
	}
	close(chunks)
	var done Chunk
	for c := range chunks {
		if c.Done {
			done = c
		}
	}
	if done.StopReason != "max_tokens" {
		t.Fatalf("expected terminal Done chunk StopReason=max_tokens, got %q", done.StopReason)
	}
}

// TestAnthropicWireStopReasonWithoutUsage verifies stop_reason is captured even
// when a gateway omits usage on message_delta. The old code only entered the
// message_delta branch when Usage != nil, dropping stop_reason in that case.
func TestAnthropicWireStopReasonWithoutUsage(t *testing.T) {
	sse := `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

`
	chunks := make(chan Chunk, 4)
	if err := parseAnthropicSSE(context.Background(), strings.NewReader(sse), chunks); err != nil {
		t.Fatalf("parseAnthropicSSE: %v", err)
	}
	close(chunks)
	var done Chunk
	for c := range chunks {
		if c.Done {
			done = c
		}
	}
	if done.StopReason != "end_turn" {
		t.Fatalf("expected terminal Done chunk StopReason=end_turn, got %q", done.StopReason)
	}
}
