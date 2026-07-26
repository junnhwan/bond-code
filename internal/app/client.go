package app

import (
	"time"

	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/llm"
)

// buildModelClient constructs the LLM client for a model config: a raw
// Anthropic-compatible client, wrapped in (1) a shared rate-limit gate so
// parent + subagent streams share one API-key budget, then (2) the retry
// decorator for bounded exponential backoff on 429/5xx/network errors with
// optional fallback models under sustained overload.
//
// Stack (outer → inner): RetryClient → RateLimitedClient → AnthropicClient.
// Each retry attempt re-enters the gate so RPM + cooldown apply per HTTP try.
// Fallback models share the same Gate instance.
//
// It is factored out of Bootstrap so app.SwitchModel can rebuild a fresh client
// when the model changes at runtime (/model <name>) without duplicating the
// construction logic. Bootstrap-only overrides (fake LLM, injected test client)
// are NOT applied here — those replace the client after this returns, so test
// doubles stay unwrapped.
func buildModelClient(cfg config.ModelConfig) llm.Client {
	rawClient := newRawModelClient(cfg, cfg.Model)
	gate := newRateLimitGate(cfg.RateLimit)
	limited := llm.NewRateLimitedClient(rawClient, gate)
	if !cfg.Retry.Enabled {
		return limited
	}
	retryCfg := llm.RetryConfig{
		Enabled:                   cfg.Retry.Enabled,
		MaxAttempts:               cfg.Retry.MaxAttempts,
		BaseBackoffMs:             cfg.Retry.BaseBackoffMs,
		MaxBackoffMs:              cfg.Retry.MaxBackoffMs,
		OverloadFallbackThreshold: cfg.Retry.OverloadFallbackThreshold,
		FallbackModels:            cfg.Retry.FallbackModels,
	}
	// Fallback factory rebuilds a raw+gated client for one alternate model,
	// reusing the same Gate so overload fallback still respects the key budget.
	fbFactory := func(model string) llm.Client {
		return llm.NewRateLimitedClient(newRawModelClient(cfg, model), gate)
	}
	return llm.NewRetryClient(limited, retryCfg, fbFactory)
}

func newRawModelClient(cfg config.ModelConfig, model string) llm.Client {
	return llm.NewAnthropicCompatibleClient(llm.AnthropicCompatibleConfig{
		BaseURL:                  cfg.BaseURL,
		APIKeyEnv:                cfg.APIKeyEnv,
		Model:                    model,
		Temperature:              cfg.Temperature,
		MaxTokens:                cfg.MaxTokens,
		StreamIdleTimeoutSeconds: cfg.StreamIdleTimeoutSeconds,
		ThinkingEnabled:          cfg.Thinking.Enabled,
		ThinkingBudgetTokens:     cfg.Thinking.BudgetTokens,
		PromptCache:              cfg.PromptCache,
	})
}

func newRateLimitGate(cfg config.RateLimitConfig) *llm.Gate {
	if cfg.Enabled == nil || !*cfg.Enabled {
		return nil
	}
	return llm.NewGate(llm.RateLimitConfig{
		Enabled:              true,
		MaxConcurrent:        cfg.MaxConcurrent,
		MaxRequestsPerMinute: cfg.MaxRequestsPerMinute,
		CooldownOnRateLimit:  time.Duration(cfg.CooldownOnRateLimitMs) * time.Millisecond,
	})
}
