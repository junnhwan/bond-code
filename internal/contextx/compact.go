package contextx

import (
	"fmt"
	"strings"
	"time"

	"github.com/junnhwan/bond-code/internal/llm"
)

// CompactionPlan is the pure preparation result for a compaction pass (Pi prepareCompaction).
type CompactionPlan struct {
	// MessagesToSummarize are complete turns before the keep window.
	MessagesToSummarize []Message
	// TurnPrefixMessages are the early part of a split turn (if IsSplitTurn).
	TurnPrefixMessages []Message
	IsSplitTurn        bool
	// Kept are messages retained verbatim after the cut (plus leading system).
	Kept            []Message
	FirstKept       int // index into body
	TokensBefore    int
	PreviousSummary string
	FileOps         FileOperations
	Settings        GovernorConfig
}

// CompactionResult is the post-apply history plus metadata.
type CompactionResult struct {
	Messages     []Message
	Summary      string
	BeforeTokens int
	AfterTokens  int
	Artifact     SummaryArtifact
}

// PrepareCompaction selects a cut and partitions messages. Returns nil plan when
// there is nothing useful to compact (already small / empty body).
func PrepareCompaction(messages []Message, cfg GovernorConfig, previousSummary string) (*CompactionPlan, error) {
	cfg = normalizeConfig(cfg)
	est := NewEstimator()
	system, body := splitSystemBody(messages)
	if len(body) < 2 {
		return nil, nil
	}

	tokensBefore := est.EstimateMessages(messages)
	cut := findCutPoint(body, cfg.KeepRecentTokens, est)
	if cut.FirstKept <= 0 {
		// Keeping everything from the start — nothing to summarize unless forced
		// callers still want a full-history summary. For threshold compact, skip.
		if cut.FirstKept == 0 && est.EstimateMessages(body) <= cfg.KeepRecentTokens {
			return nil, nil
		}
	}
	if cut.FirstKept >= len(body) {
		return nil, nil
	}
	// Need at least one message before the keep window.
	historyEnd := cut.FirstKept
	if cut.IsSplitTurn {
		historyEnd = cut.TurnStart
		if historyEnd < 0 {
			historyEnd = 0
		}
	}
	if historyEnd <= 0 && !cut.IsSplitTurn {
		return nil, nil
	}

	toSummarize := append([]Message(nil), body[:historyEnd]...)
	var turnPrefix []Message
	if cut.IsSplitTurn && cut.TurnStart >= 0 && cut.TurnStart < cut.FirstKept {
		turnPrefix = append([]Message(nil), body[cut.TurnStart:cut.FirstKept]...)
	}
	if len(toSummarize) == 0 && len(turnPrefix) == 0 {
		return nil, nil
	}

	keptBody := append([]Message(nil), body[cut.FirstKept:]...)
	kept := append(append([]Message(nil), system...), keptBody...)

	fileOps := extractFileOperations(toSummarize)
	if len(turnPrefix) > 0 {
		mergeFileOps(&fileOps, extractFileOperations(turnPrefix))
	}

	return &CompactionPlan{
		MessagesToSummarize: toSummarize,
		TurnPrefixMessages:  turnPrefix,
		IsSplitTurn:         cut.IsSplitTurn,
		Kept:                kept,
		FirstKept:           cut.FirstKept,
		TokensBefore:        tokensBefore,
		PreviousSummary:     previousSummary,
		FileOps:             fileOps,
		Settings:            cfg,
	}, nil
}

// ApplyCompaction rebuilds history as: system? + summary user message + kept tail.
// Mirrors Pi (summary + messages from firstKept) and CC (boundary + summary + keep).
func ApplyCompaction(plan *CompactionPlan, summary string) CompactionResult {
	if plan == nil {
		return CompactionResult{}
	}
	summary = strings.TrimSpace(summary)
	lists := computeFileLists(plan.FileOps)
	if len(lists.ReadFiles) > 0 || len(lists.ModifiedFiles) > 0 {
		summary = strings.TrimSpace(summary + formatFileOperations(lists.ReadFiles, lists.ModifiedFiles))
	}

	system, keptBody := splitSystemBody(plan.Kept)
	summaryMsg := Message{
		Role:    llm.RoleUser,
		Content: formatCompactSummaryMessage(summary),
	}
	out := make([]Message, 0, len(system)+1+len(keptBody))
	out = append(out, system...)
	out = append(out, summaryMsg)
	out = append(out, keptBody...)
	out = ensureIntegrity(out)

	est := NewEstimator()
	after := est.EstimateMessages(out)
	artifact := SummaryArtifact{
		Version:       2,
		Summary:       summary,
		CreatedAt:     time.Now().UTC(),
		BeforeTokens:  plan.TokensBefore,
		AfterTokens:   after,
		ReadFiles:     filePathsToObservations(lists.ReadFiles, true),
		ModifiedFiles: filePathsToObservations(lists.ModifiedFiles, false),
	}
	return CompactionResult{
		Messages:     out,
		Summary:      summary,
		BeforeTokens: plan.TokensBefore,
		AfterTokens:  after,
		Artifact:     artifact,
	}
}

// ForcePrepareCompaction always builds a plan that summarizes everything except
// the keep-recent window — used by manual /compact and reactive overflow recovery.
func ForcePrepareCompaction(messages []Message, cfg GovernorConfig, previousSummary string) *CompactionPlan {
	cfg = normalizeConfig(cfg)
	plan, err := PrepareCompaction(messages, cfg, previousSummary)
	if err == nil && plan != nil {
		return plan
	}
	// Fallback: keep only the last ~keepRecentTokens worth of messages.
	est := NewEstimator()
	system, body := splitSystemBody(messages)
	if len(body) == 0 {
		return nil
	}
	cut := findCutPoint(body, cfg.KeepRecentTokens, est)
	if cut.FirstKept <= 0 {
		// Keep half the body if everything fits in keep window but force was requested.
		cut.FirstKept = len(body) / 2
		if cut.FirstKept < 1 && len(body) > 1 {
			cut.FirstKept = 1
		}
		// Snap to valid cut.
		for cut.FirstKept < len(body) && body[cut.FirstKept].Role == llm.RoleTool {
			cut.FirstKept++
		}
		if cut.FirstKept >= len(body) {
			cut.FirstKept = len(body) - 1
		}
	}
	toSummarize := append([]Message(nil), body[:cut.FirstKept]...)
	keptBody := append([]Message(nil), body[cut.FirstKept:]...)
	if len(toSummarize) == 0 {
		return nil
	}
	return &CompactionPlan{
		MessagesToSummarize: toSummarize,
		Kept:                append(append([]Message(nil), system...), keptBody...),
		FirstKept:           cut.FirstKept,
		TokensBefore:        est.EstimateMessages(messages),
		PreviousSummary:     previousSummary,
		FileOps:             extractFileOperations(toSummarize),
		Settings:            cfg,
	}
}

func formatCompactSummaryMessage(summary string) string {
	var b strings.Builder
	b.WriteString("The conversation history before this point was compacted into the following summary:\n\n")
	b.WriteString(summary)
	b.WriteString("\n\nContinue from this summary. Do not ask the user to repeat prior context.")
	return b.String()
}

// ShouldCompact reports whether contextTokens exceed the Pi threshold.
func ShouldCompact(contextTokens int, cfg GovernorConfig) bool {
	cfg = normalizeConfig(cfg)
	if !cfg.AutoCompact {
		return false
	}
	return contextTokens > cfg.CompactThreshold()
}

// EmergencyShrink is a deterministic last-resort shrink for prompt_too_long when
// LLM summarization is unavailable. It keeps system + recent keepRecentTokens
// and inserts a short placeholder summary (no model call).
func EmergencyShrink(messages []Message, cfg GovernorConfig) CompactionResult {
	cfg = normalizeConfig(cfg)
	plan := ForcePrepareCompaction(messages, cfg, "")
	if plan == nil {
		// Absolute fallback: keep system + last message only.
		system, body := splitSystemBody(messages)
		if len(body) == 0 {
			return CompactionResult{Messages: messages}
		}
		kept := append(append([]Message(nil), system...), body[len(body)-1])
		est := NewEstimator()
		return CompactionResult{
			Messages:     ensureIntegrity(kept),
			Summary:      "Earlier conversation was dropped to recover from context overflow.",
			BeforeTokens: est.EstimateMessages(messages),
			AfterTokens:  est.EstimateMessages(kept),
		}
	}
	summary := "Earlier conversation was compacted after a context-overflow error. Key recent work is retained below; re-read files if exact contents are needed."
	if n := len(plan.MessagesToSummarize); n > 0 {
		summary = fmt.Sprintf(
			"Earlier conversation (%d messages) was compacted after a context-overflow error. Key recent work is retained below; re-read files if exact contents are needed.",
			n,
		)
	}
	return ApplyCompaction(plan, summary)
}
