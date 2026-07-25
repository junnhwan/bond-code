package contextx

// Governor is the per-turn governance engine (integrity + tool-result layers).
// Semantic compaction lives in PrepareCompaction / ApplyCompaction — not here.
type Governor struct {
	cfg       GovernorConfig
	estimator *Estimator
}

// NewGovernor creates a Governor with normalized defaults.
func NewGovernor(cfg GovernorConfig) *Governor {
	return &Governor{
		cfg:       normalizeConfig(cfg),
		estimator: NewEstimator(),
	}
}

// Config returns a copy of the normalized config.
func (g *Governor) Config() GovernorConfig {
	return g.cfg
}

// Govern runs per-turn preparation (no history snip, no LLM compact).
func (g *Governor) Govern(messages []Message, maxTokens int) []Message {
	return g.GovernDetailed(messages, maxTokens).Messages
}

// GovernDetailed applies integrity + tool-result governance and returns stats.
// maxTokens is accepted for API compatibility; snip-by-budget was removed.
// Threshold compaction is owned by App/Loop via ShouldCompact + Compact.
func (g *Governor) GovernDetailed(messages []Message, maxTokens int) GovernResult {
	_ = maxTokens
	cfg := g.cfg
	if maxTokens > 0 {
		cfg.MaxTokens = maxTokens
	}
	before := g.estimator.EstimateMessages(messages)
	cleaned := ensureIntegrity(messages)
	governed, micro, spilled := governToolResults(cleaned, cfg)
	final := ensureIntegrity(governed)
	return GovernResult{
		Messages:             final,
		BeforeTokens:         before,
		AfterTokens:          g.estimator.EstimateMessages(final),
		CompactedToolResults: micro,
		TruncatedToolResults: spilled,
		// SnippedMessages intentionally stays 0 — snip layer removed.
	}
}

// dropOrphanToolResults / backfill / microCompact / applyToolResultBudget are
// kept as thin wrappers so existing unit tests that call them on Governor still compile.

func (g *Governor) dropOrphanToolResults(messages []Message) []Message {
	return dropOrphanToolResults(messages)
}

func (g *Governor) backfillMissingToolResults(messages []Message) []Message {
	return backfillMissingToolResults(messages)
}

func (g *Governor) microCompact(messages []Message) []Message {
	return microClearToolResults(messages, g.cfg)
}

func (g *Governor) applyToolResultBudget(messages []Message) []Message {
	return applyToolResultBudget(messages, g.cfg)
}
