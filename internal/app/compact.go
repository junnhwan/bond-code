package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
)

// CompactResult describes one AI compaction pass.
type CompactResult struct {
	BeforeTokens int
	AfterTokens  int
	Summary      string
	Compacted    bool
}

// Compact triggers AI summarization of the conversation history; it is the
// handler behind the /compact command.
func (a *App) Compact(ctx context.Context, sink agent.EventSink) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := a.compactHistoryLocked(ctx, sink, true)
	return err
}

// compactHistoryLocked performs Pi-style cut + LLM checkpoint summarization.
// When force is false it returns early unless usage exceeds the compact threshold.
func (a *App) compactHistoryLocked(ctx context.Context, sink agent.EventSink, force bool) (*CompactResult, error) {
	history := append([]llm.Message(nil), a.history...)
	if len(history) < 2 || a.LLM == nil {
		return &CompactResult{}, nil
	}

	cfg := a.contextGovernorConfig()
	beforeTokens := a.contextEstimate(history)
	if !force && !contextx.ShouldCompact(beforeTokens, cfg) {
		return &CompactResult{BeforeTokens: beforeTokens}, nil
	}

	var previousSummary string
	if a.ContextSummary != nil {
		if artifact, err := a.ContextSummary.Load(); err == nil && artifact != nil {
			previousSummary = artifact.Summary
		}
	}

	var plan *contextx.CompactionPlan
	if force {
		plan = contextx.ForcePrepareCompaction(history, cfg, previousSummary)
	} else {
		var err error
		plan, err = contextx.PrepareCompaction(history, cfg, previousSummary)
		if err != nil {
			return nil, err
		}
	}
	if plan == nil {
		return &CompactResult{BeforeTokens: beforeTokens}, nil
	}

	span := append([]llm.Message(nil), plan.MessagesToSummarize...)
	if len(plan.TurnPrefixMessages) > 0 {
		span = append(span, plan.TurnPrefixMessages...)
	}

	if sink != nil {
		sink(agent.Event{
			Type: agent.EventCompactionStarted,
			Message: fmt.Sprintf(
				"compacting %d messages (~%d tokens), keeping recent window",
				len(span), beforeTokens,
			),
		})
	}

	summary, err := agent.CompleteText(ctx, a.LLM, agent.SummarizationMessages(span, previousSummary))
	if err != nil {
		if sink != nil {
			sink(agent.Event{Type: agent.EventCompactionFinished, Message: "compaction failed: " + err.Error(), Error: err.Error()})
		}
		return nil, err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		if sink != nil {
			sink(agent.Event{Type: agent.EventCompactionFinished, Message: "compaction produced empty summary; history unchanged"})
		}
		return &CompactResult{BeforeTokens: beforeTokens}, nil
	}

	applied := contextx.ApplyCompaction(plan, summary)
	a.history = applied.Messages

	if a.ContextSummary != nil {
		artifact := applied.Artifact
		artifact.Compacted = true
		_ = a.ContextSummary.Save(artifact)
	}

	result := &CompactResult{
		BeforeTokens: applied.BeforeTokens,
		AfterTokens:  applied.AfterTokens,
		Summary:      applied.Summary,
		Compacted:    true,
	}
	if sink != nil {
		sink(agent.Event{
			Type:             agent.EventCompactionFinished,
			Message:          fmt.Sprintf("compacted %d -> %d tokens", result.BeforeTokens, result.AfterTokens),
			ContextTokens:    result.AfterTokens,
			ContextMaxTokens: cfg.MaxTokens,
		})
	}
	return result, nil
}

func (a *App) contextGovernorConfig() contextx.GovernorConfig {
	cfg := contextx.DefaultGovernorConfig()
	if a.ContextManager != nil {
		cfg = a.ContextManager.Config()
	}
	if a.MaxContextTokens > 0 {
		cfg.MaxTokens = a.MaxContextTokens
	}
	if a.Config != nil {
		c := a.Config.Context
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
		} else {
			cfg.AutoCompact = true
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
	}
	return contextx.NormalizeConfig(cfg)
}

func (a *App) maxContextTokensValue() int {
	if a.MaxContextTokens > 0 {
		return a.MaxContextTokens
	}
	return 100000
}

func (a *App) contextEstimate(messages []llm.Message) int {
	if a.ContextManager != nil {
		return a.ContextManager.EstimateTokens(messages)
	}
	est := contextx.NewEstimator()
	return est.EstimateMessages(messages)
}
