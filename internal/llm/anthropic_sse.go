package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func parseAnthropicSSE(ctx context.Context, r io.Reader, chunks chan<- Chunk) error {
	return parseAnthropicSSEWithParser(ctx, r, chunks, newAnthropicSSEParser())
}

func parseAnthropicSSEWithParser(ctx context.Context, r io.Reader, chunks chan<- Chunk, parser *anthropicSSEParser) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := parser.emitEvent(ctx, strings.Join(dataLines, "\n"), chunks); err != nil {
				return err
			}
			dataLines = nil
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(dataLines) > 0 {
		if err := parser.emitEvent(ctx, strings.Join(dataLines, "\n"), chunks); err != nil {
			return err
		}
	}
	return parser.sendChunk(ctx, chunks, Chunk{Done: true, StopReason: parser.stopReason})
}

type anthropicSSEParser struct {
	toolUses   map[int]*pendingToolUse
	usage      *wireUsage
	stopReason string

	// beforeChunkSend is a package-private deterministic test seam. Production
	// parsers leave it nil; tests use it to rendezvous at downstream backpressure.
	beforeChunkSend func(Chunk)
}

// wireUsage mirrors the usage object Anthropic streams back: message_start
// carries input_tokens (+ cache_creation/read), message_delta carries the
// cumulative output_tokens.
type wireUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type pendingToolUse struct {
	id        string
	name      string
	input     string
	hasInput  bool
	partial   strings.Builder
	hasDelta  bool
	blockType string
}

func newAnthropicSSEParser() *anthropicSSEParser {
	return &anthropicSSEParser{toolUses: map[int]*pendingToolUse{}}
}

func (p *anthropicSSEParser) emitEvent(ctx context.Context, data string, chunks chan<- Chunk) error {
	if data == "" || data == "[DONE]" {
		return nil
	}
	if os.Getenv("BONDCODE_DEBUG_SSE") != "" {
		fmt.Fprintln(os.Stderr, "[bondcode sse] "+data)
	}
	var event struct {
		Type         string `json:"type"`
		Index        int    `json:"index"`
		ContentBlock *struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content_block"`
		Delta *struct {
			Type             string `json:"type"`
			Text             string `json:"text"`
			PartialJSON      string `json:"partial_json"`
			Thinking         string `json:"thinking"`
			ReasoningContent string `json:"reasoning_content"`
			StopReason       string `json:"stop_reason"`
		} `json:"delta"`
		Message *struct {
			Usage *wireUsage `json:"usage"`
		} `json:"message"`
		Usage *wireUsage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil
	}
	// message_start reports input_tokens up front (the true context occupancy
	// for this request, including cached prefixes); message_delta reports the
	// cumulative output_tokens as the stream finalizes. Accumulate and emit.
	if event.Type == "message_start" {
		if event.Message != nil && event.Message.Usage != nil {
			u := event.Message.Usage
			p.usage = &wireUsage{
				InputTokens: u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
			}
			if err := p.emitUsage(ctx, chunks); err != nil {
				return err
			}
		}
		return nil
	}
	if event.Type == "message_delta" {
		// delta.stop_reason is the terminal signal (end_turn | tool_use |
		// max_tokens | stop_sequence). Capture it; it rides the final Done
		// chunk so the agent loop can distinguish "model finished" from
		// "response was truncated by max_tokens" and retry instead of silently
		// exiting a long task. Note some compatible gateways omit usage on the
		// delta, so parse stop_reason unconditionally before the usage branch.
		if event.Delta != nil && event.Delta.StopReason != "" {
			p.stopReason = event.Delta.StopReason
		}
		if event.Usage != nil {
			if p.usage == nil {
				p.usage = &wireUsage{}
			}
			// Standard Anthropic carries input_tokens only in message_start and
			// the cumulative output_tokens in message_delta. Some compatible
			// gateways (e.g. GLM's /api/anthropic) stream input_tokens=0 in
			// message_start and surface the real counts only in message_delta,
			// so recompute input from the delta payload whenever it is non-zero,
			// instead of trusting the placeholder left by message_start.
			freshInput := event.Usage.InputTokens + event.Usage.CacheCreationInputTokens + event.Usage.CacheReadInputTokens
			if freshInput > 0 {
				p.usage.InputTokens = freshInput
			}
			p.usage.OutputTokens = event.Usage.OutputTokens
			if err := p.emitUsage(ctx, chunks); err != nil {
				return err
			}
		}
		return nil
	}
	if event.Type == "content_block_start" && event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
		pending := &pendingToolUse{
			id:        event.ContentBlock.ID,
			name:      event.ContentBlock.Name,
			blockType: event.ContentBlock.Type,
		}
		if len(event.ContentBlock.Input) > 0 {
			pending.input = string(event.ContentBlock.Input)
			pending.hasInput = true
		}
		p.toolUses[event.Index] = pending
		return nil
	}
	if event.Delta != nil && event.Delta.Text != "" {
		if err := p.sendChunk(ctx, chunks, Chunk{Content: event.Delta.Text}); err != nil {
			return err
		}
	}
	// Reasoning/thinking content: capture both Anthropic's thinking_delta
	// (delta.thinking) and OpenAI/GLM-style reasoning_content, without
	// tracking the content_block type, so it works across providers.
	if event.Delta != nil {
		reasoning := event.Delta.Thinking
		if reasoning == "" {
			reasoning = event.Delta.ReasoningContent
		}
		if reasoning != "" {
			if err := p.sendChunk(ctx, chunks, Chunk{Reasoning: reasoning}); err != nil {
				return err
			}
		}
	}
	if event.Type == "content_block_delta" && event.Delta != nil && event.Delta.Type == "input_json_delta" {
		if pending, ok := p.toolUses[event.Index]; ok {
			pending.partial.WriteString(event.Delta.PartialJSON)
			pending.hasDelta = true
		}
		return nil
	}
	if event.Type != "content_block_stop" {
		return nil
	}
	pending, ok := p.toolUses[event.Index]
	if !ok || pending.blockType != "tool_use" {
		return nil
	}
	delete(p.toolUses, event.Index)
	args := "{}"
	if pending.hasDelta {
		args = pending.partial.String()
	} else if pending.hasInput && strings.TrimSpace(pending.input) != "" {
		args = pending.input
	}
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	if pending.id != "" || pending.name != "" {
		if err := p.sendChunk(ctx, chunks, Chunk{ToolCall: &ToolCall{ID: pending.id, Name: pending.name, Arguments: args}}); err != nil {
			return err
		}
	}
	return nil
}

// emitUsage forwards the latest accumulated usage as a Chunk so the agent loop
// can surface real token counts. Called from message_start (input known up
// front) and message_delta (output accumulated); each call reflects the most
// complete usage seen so far on this stream.
func (p *anthropicSSEParser) emitUsage(ctx context.Context, chunks chan<- Chunk) error {
	if p.usage == nil {
		return nil
	}
	return p.sendChunk(ctx, chunks, Chunk{Usage: &Usage{
		InputTokens:              p.usage.InputTokens,
		OutputTokens:             p.usage.OutputTokens,
		CacheReadInputTokens:     p.usage.CacheReadInputTokens,
		CacheCreationInputTokens: p.usage.CacheCreationInputTokens,
	}})
}

func (p *anthropicSSEParser) sendChunk(ctx context.Context, chunks chan<- Chunk, chunk Chunk) error {
	if p.beforeChunkSend != nil {
		p.beforeChunkSend(chunk)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case chunks <- chunk:
		return nil
	}
}
