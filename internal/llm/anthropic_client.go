package llm

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/junnhwan/bond-code/internal/observe"
)

type AnthropicCompatibleConfig struct {
	BaseURL                  string
	APIKeyEnv                string
	Model                    string
	Temperature              float32
	MaxTokens                int
	StreamIdleTimeoutSeconds int
	ThinkingEnabled          bool
	ThinkingBudgetTokens     int
	// PromptCache injects Anthropic prompt-cache breakpoints (system + last tool)
	// into the outgoing request. See applyCacheBreakpoints.
	PromptCache bool
}

type AnthropicCompatibleClient struct {
	cfg               AnthropicCompatibleConfig
	httpClient        *http.Client
	streamIdleTimeout time.Duration
}

func NewAnthropicCompatibleClient(cfg AnthropicCompatibleConfig) *AnthropicCompatibleClient {
	cfg = normalizeThinkingConfig(cfg)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 60 * time.Second
	return &AnthropicCompatibleClient{
		cfg: cfg,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   0,
		},
		streamIdleTimeout: normalizeStreamIdleTimeout(cfg.StreamIdleTimeoutSeconds),
	}
}

// normalizeThinkingConfig enforces the Anthropic-protocol invariant that an
// extended-thinking budget must be strictly less than max_tokens: max_tokens
// caps thinking + visible output combined. When budget_tokens meets or exceeds
// max_tokens, thinking can consume the entire output window and truncate the
// response to zero visible content (no text, no tool_use) — which the loop
// used to treat as "model finished", silently ending long tasks.
//
// Auto-raise max_tokens to leave a comfortable visible-output window above the
// budget, with a stderr warning, so a misconfigured bondcode.yaml (budget set
// but max_tokens left at the 4096 default) still works instead of silently
// stalling. No-op when thinking is disabled.
func normalizeThinkingConfig(cfg AnthropicCompatibleConfig) AnthropicCompatibleConfig {
	if !cfg.ThinkingEnabled {
		return cfg
	}
	budget := cfg.ThinkingBudgetTokens
	if budget < 1024 {
		budget = 4096
	}
	// Reserve a visible-output floor on top of the thinking budget. 4096 covers
	// a normal text answer + a tool call with headroom for the next turn.
	const visibleOutputFloor = 4096
	need := budget + visibleOutputFloor
	if cfg.MaxTokens < need {
		fmt.Fprintf(os.Stderr, "[bondcode] thinking.budget_tokens=%d requires max_tokens>=%d to leave room for visible output; raising max_tokens from %d to %d\n", budget, need, cfg.MaxTokens, need)
		cfg.MaxTokens = need
	}
	return cfg
}

func (c *AnthropicCompatibleClient) Stream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan Chunk, <-chan error) {
	chunks := make(chan Chunk)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		defer func() {
			if r := recover(); r != nil {
				observe.LogPanic("llm-stream", r, debug.Stack())
				select {
				case errs <- fmt.Errorf("stream panic: %v", r):
				default:
				}
			}
		}()
		errs <- c.streamAnthropicWire(ctx, messages, tools, chunks)
	}()
	return chunks, errs
}
