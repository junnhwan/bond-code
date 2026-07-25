package app

import (
	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/llm"
)

// buildModelClient constructs the LLM client for a model config: a raw
// Anthropic-compatible client, optionally wrapped in the retry decorator that
// does bounded exponential backoff on 429/5xx/network errors and falls back to
// alternate models under sustained overload.
//
// It is factored out of Bootstrap so app.SwitchModel can rebuild a fresh client
// when the model changes at runtime (/model <name>) without duplicating the
// construction logic. Bootstrap-only overrides (fake LLM, injected test client)
// are NOT applied here — those replace the client after this returns, so test
// doubles stay unwrapped.
func buildModelClient(cfg config.ModelConfig) llm.Client {
	rawClient := llm.NewAnthropicCompatibleClient(llm.AnthropicCompatibleConfig{
		BaseURL:                  cfg.BaseURL,
		APIKeyEnv:                cfg.APIKeyEnv,
		Model:                    cfg.Model,
		Temperature:              cfg.Temperature,
		MaxTokens:                cfg.MaxTokens,
		StreamIdleTimeoutSeconds: cfg.StreamIdleTimeoutSeconds,
		ThinkingEnabled:          cfg.Thinking.Enabled,
		ThinkingBudgetTokens:     cfg.Thinking.BudgetTokens,
		PromptCache:              cfg.PromptCache,
	})
	if !cfg.Retry.Enabled {
		return rawClient
	}
	retryCfg := llm.RetryConfig{
		Enabled:                   cfg.Retry.Enabled,
		MaxAttempts:               cfg.Retry.MaxAttempts,
		BaseBackoffMs:             cfg.Retry.BaseBackoffMs,
		MaxBackoffMs:              cfg.Retry.MaxBackoffMs,
		OverloadFallbackThreshold: cfg.Retry.OverloadFallbackThreshold,
		FallbackModels:            cfg.Retry.FallbackModels,
	}
	// The fallback factory rebuilds a raw client for one alternate model,
	// reusing every other setting (base URL, key env, thinking, cache) from the
	// same config — so an overload fallback lands on the same gateway, just a
	// different model name.
	fbFactory := func(model string) llm.Client {
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
	return llm.NewRetryClient(rawClient, retryCfg, fbFactory)
}
