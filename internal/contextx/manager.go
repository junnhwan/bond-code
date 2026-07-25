package contextx

import "fmt"

// Manager is the runtime-facing context API used by agent.Loop and app.
type Manager struct {
	governor *Governor
}

// NewManager wraps a Governor.
func NewManager(gov *Governor) *Manager {
	return &Manager{governor: gov}
}

// Config returns the underlying governor config.
func (m *Manager) Config() GovernorConfig {
	if m == nil || m.governor == nil {
		return DefaultGovernorConfig()
	}
	return m.governor.Config()
}

// Govern executes per-turn preparation (integrity + tool results).
func (m *Manager) Govern(messages []Message, maxTokens int) []Message {
	return m.GovernDetailed(messages, maxTokens).Messages
}

// GovernResult describes one PrepareTurn pass.
type GovernResult struct {
	Messages             []Message
	BeforeTokens         int
	AfterTokens          int
	CompactedToolResults int
	TruncatedToolResults int
	SnippedMessages      int // always 0 after rewrite; kept for debug/compat
	SummaryArtifact      *SummaryArtifact
}

func (r GovernResult) Summary() string {
	return fmt.Sprintf(
		"context tokens: %d -> %d; micro_cleared=%d spilled_tool_results=%d",
		r.BeforeTokens,
		r.AfterTokens,
		r.CompactedToolResults,
		r.TruncatedToolResults,
	)
}

// GovernDetailed is the per-turn view builder (no snip, no LLM).
func (m *Manager) GovernDetailed(messages []Message, maxTokens int) GovernResult {
	return m.governor.GovernDetailed(messages, maxTokens)
}

// EstimateTokens estimates tokens for a message list.
func (m *Manager) EstimateTokens(messages []Message) int {
	return m.governor.estimator.EstimateMessages(messages)
}

// ShouldCompact reports whether usage is over the compaction threshold.
func (m *Manager) ShouldCompact(contextTokens int) bool {
	return ShouldCompact(contextTokens, m.Config())
}

// PrepareCompaction builds a cut plan for LLM summarization.
func (m *Manager) PrepareCompaction(messages []Message, previousSummary string) (*CompactionPlan, error) {
	return PrepareCompaction(messages, m.Config(), previousSummary)
}

// ForcePrepareCompaction always prepares a plan (manual / reactive).
func (m *Manager) ForcePrepareCompaction(messages []Message, previousSummary string) *CompactionPlan {
	return ForcePrepareCompaction(messages, m.Config(), previousSummary)
}

// ApplyCompaction applies a summary onto a plan.
func (m *Manager) ApplyCompaction(plan *CompactionPlan, summary string) CompactionResult {
	return ApplyCompaction(plan, summary)
}

// EmergencyShrink deterministically shrinks history after prompt_too_long.
func (m *Manager) EmergencyShrink(messages []Message) CompactionResult {
	return EmergencyShrink(messages, m.Config())
}
