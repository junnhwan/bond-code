package llm

import "testing"

// TestNormalizeThinkingConfigAutoRaisesMaxTokens covers the root-cause config
// guard: when thinking is enabled, max_tokens must exceed budget_tokens by a
// visible-output floor, otherwise thinking eats the whole output window and the
// response is truncated to empty (no text, no tool call).
func TestNormalizeThinkingConfigAutoRaisesMaxTokens(t *testing.T) {
	cases := []struct {
		name            string
		thinkingEnabled bool
		budget          int
		maxTokens       int
		wantMax         int
	}{
		{"thinking off leaves max_tokens", false, 4096, 4096, 4096},
		{"budget meets default max raises", true, 4096, 4096, 8192},
		{"zero max_tokens raised", true, 4096, 0, 8192},
		{"already enough unchanged", true, 4096, 8192, 8192},
		{"generous unchanged", true, 4096, 16384, 16384},
		{"missing budget defaults to 4096", true, 0, 0, 8192},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := normalizeThinkingConfig(AnthropicCompatibleConfig{
				MaxTokens:            c.maxTokens,
				ThinkingEnabled:      c.thinkingEnabled,
				ThinkingBudgetTokens: c.budget,
			})
			if cfg.MaxTokens != c.wantMax {
				t.Fatalf("MaxTokens = %d, want %d", cfg.MaxTokens, c.wantMax)
			}
		})
	}
}
