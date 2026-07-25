package contextx

// GovernorConfig configures per-turn tool-result governance and compaction.
// Field names keep the historical Governor* prefix used by bootstrap/config.
type GovernorConfig struct {
	// Context window size in estimated tokens. Used by ShouldCompact / cut.
	MaxTokens int
	// Tokens reserved for the model response and the current turn (Pi reserveTokens).
	// Compaction triggers when usage > MaxTokens - ReserveTokens.
	ReserveTokens int
	// Approximate recent-context tokens to keep after compaction (Pi keepRecentTokens).
	KeepRecentTokens int
	// When false, threshold auto-compact is off; manual /compact still works.
	// Callers should set this explicitly; DefaultGovernorConfig enables it.
	AutoCompact bool

	MicroCompactKeepRecent int // keep last N compactable tool results, default 6
	MicroCompactMinChars   int // only clear results longer than this, default 500
	ToolResultBudget       int // per-result char cap before spill/truncate, default 8000
	ToolResultPreviewChars int // spill preview size, default 2000
	ToolResultTurnBudget   int // aggregate budget for latest tool turn, 0 = disabled
	ToolResultStore        *ToolResultStore

	// Consecutive compact failures before circuit-breaker opens (default 3).
	CompactCircuitBreaker int
}

// DefaultGovernorConfig returns Pi/CC-inspired defaults for a lightweight agent.
func DefaultGovernorConfig() GovernorConfig {
	return GovernorConfig{
		MaxTokens:              100_000,
		ReserveTokens:          16_384,
		KeepRecentTokens:       20_000,
		AutoCompact:            true,
		MicroCompactKeepRecent: 6,
		MicroCompactMinChars:   500,
		ToolResultBudget:       8_000,
		ToolResultPreviewChars: 2_000,
		ToolResultTurnBudget:   50_000,
		CompactCircuitBreaker:  3,
	}
}

// NormalizeConfig applies defaults; exported for app bootstrap.
func NormalizeConfig(cfg GovernorConfig) GovernorConfig {
	return normalizeConfig(cfg)
}

func normalizeConfig(cfg GovernorConfig) GovernorConfig {
	def := DefaultGovernorConfig()
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = def.MaxTokens
	}
	if cfg.ReserveTokens <= 0 {
		cfg.ReserveTokens = def.ReserveTokens
	}
	if cfg.KeepRecentTokens <= 0 {
		cfg.KeepRecentTokens = def.KeepRecentTokens
	}
	// AutoCompact is left as-is (zero = false). Prefer DefaultGovernorConfig()
	// or explicit true at bootstrap.
	if cfg.MicroCompactKeepRecent <= 0 {
		cfg.MicroCompactKeepRecent = def.MicroCompactKeepRecent
	}
	if cfg.MicroCompactMinChars <= 0 {
		cfg.MicroCompactMinChars = def.MicroCompactMinChars
	}
	if cfg.ToolResultBudget <= 0 {
		cfg.ToolResultBudget = def.ToolResultBudget
	}
	if cfg.ToolResultPreviewChars <= 0 {
		cfg.ToolResultPreviewChars = def.ToolResultPreviewChars
	}
	if cfg.ToolResultTurnBudget < 0 {
		cfg.ToolResultTurnBudget = 0
	}
	if cfg.CompactCircuitBreaker <= 0 {
		cfg.CompactCircuitBreaker = def.CompactCircuitBreaker
	}
	return cfg
}

// CompactThreshold is MaxTokens - ReserveTokens (Pi shouldCompact).
func (c GovernorConfig) CompactThreshold() int {
	c = normalizeConfig(c)
	th := c.MaxTokens - c.ReserveTokens
	if th < 1 {
		return 1
	}
	return th
}
