package llm

import (
	"context"
	"sync"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	ToolName   string
}

type ToolSpec struct {
	Name        string
	Description string
	Schema      any
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type Chunk struct {
	Content   string
	Reasoning string
	ToolCall  *ToolCall
	// Usage carries real token counts reported by the model API on
	// message_start/message_delta. Nil for chunks/providers that don't report
	// usage (e.g. FakeClient). InputTokens is the full input size including
	// cache reads/creates — the true current context-window occupancy.
	Usage *Usage
	// StopReason carries the provider's terminal reason for the stream
	// (end_turn | tool_use | max_tokens | stop_sequence), reported on the final
	// message_delta and surfaced on the terminal Done chunk. Empty when the
	// provider doesn't surface it. The agent loop uses it to distinguish a
	// genuine "model is done" (end_turn / tool_use) from a truncated response
	// (max_tokens) so it can retry instead of silently ending a long task.
	StopReason string
	Done       bool
}

// Usage holds real token counts measured by the model API, as opposed to the
// chars/3 estimate the context governor uses internally for trimming decisions.
type Usage struct {
	InputTokens  int
	OutputTokens int
	// CacheReadInputTokens / CacheCreationInputTokens are the prompt-cache token
	// breakdown reported by Anthropic-compatible providers. InputTokens already
	// includes these (the SSE parser sums them for context-window accounting);
	// these fields expose the breakdown so debug traces can show the cache hit
	// rate. Zero for providers/chunks that don't report cache stats.
	CacheReadInputTokens     int
	CacheCreationInputTokens int
}

type Client interface {
	Stream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan Chunk, <-chan error)
}

type FakeClient struct {
	chunks []Chunk

	mu   sync.Mutex
	last []Message
}

func NewFakeClient(chunks []Chunk) *FakeClient {
	return &FakeClient{chunks: append([]Chunk(nil), chunks...)}
}

func (f *FakeClient) Stream(ctx context.Context, messages []Message, _ []ToolSpec) (<-chan Chunk, <-chan error) {
	f.mu.Lock()
	f.last = append([]Message(nil), messages...)
	response := append([]Chunk(nil), f.chunks...)
	f.mu.Unlock()

	chunks := make(chan Chunk)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		for _, chunk := range response {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case chunks <- chunk:
			}
		}
		errs <- nil
	}()
	return chunks, errs
}

func (f *FakeClient) LastMessages() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Message(nil), f.last...)
}
