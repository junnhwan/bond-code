package app

import (
	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/contextx"
)

// governorConfigFrom loads a contextx.GovernorConfig from app config.
func governorConfigFrom(c config.ContextConfig, toolStore *contextx.ToolResultStore) contextx.GovernorConfig {
	cfg := contextx.DefaultGovernorConfig()
	if c.MaxTokens > 0 {
		cfg.MaxTokens = c.MaxTokens
	}
	if c.ReserveTokens > 0 {
		cfg.ReserveTokens = c.ReserveTokens
	}
	if c.KeepRecentTokens > 0 {
		cfg.KeepRecentTokens = c.KeepRecentTokens
	}
	if c.AutoCompactExplicitlySet() {
		cfg.AutoCompact = c.AutoCompact
	}
	if c.MicroCompactKeepRecent > 0 {
		cfg.MicroCompactKeepRecent = c.MicroCompactKeepRecent
	}
	if c.MicroCompactMinChars > 0 {
		cfg.MicroCompactMinChars = c.MicroCompactMinChars
	}
	if c.ToolResultBudget > 0 {
		cfg.ToolResultBudget = c.ToolResultBudget
	}
	if c.ToolResultPreviewChars > 0 {
		cfg.ToolResultPreviewChars = c.ToolResultPreviewChars
	}
	if c.ToolResultTurnBudget > 0 {
		cfg.ToolResultTurnBudget = c.ToolResultTurnBudget
	}
	cfg.ToolResultStore = toolStore
	return contextx.NormalizeConfig(cfg)
}
