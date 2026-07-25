package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/llm"
)

// prepareModelMessages builds the per-step model view:
// integrity + tool-result governance (CC-style), plus any prior compaction
// summary already present in history. It does NOT snip history or call the LLM.
func (l *Loop) prepareModelMessages(ctx context.Context, messages []llm.Message, emit EventSink, step int) []llm.Message {
	_ = ctx
	if l.contextManager == nil {
		return messages
	}

	maxTokens := l.maxContextTokens
	if maxTokens <= 0 {
		maxTokens = l.contextManager.Config().MaxTokens
	}
	if maxTokens <= 0 {
		maxTokens = 100000
	}

	governed := l.contextManager.GovernDetailed(messages, maxTokens)
	// Surface a previously saved compaction artifact only when history does not
	// already contain a compact-summary user message (ApplyCompaction embeds it).
	if l.contextSummary != nil && !historyHasCompactSummary(governed.Messages) {
		if artifact, err := l.contextSummary.Load(); err == nil && artifact != nil && strings.TrimSpace(artifact.Summary) != "" {
			governed.SummaryArtifact = artifact
			governed.Messages = withContextSummarySection(governed.Messages, artifact.PromptSection(4000))
		}
	}

	emit(Event{
		Type:             EventContextUpdated,
		Message:          governed.Summary(),
		ContextTokens:    governed.AfterTokens,
		ContextMaxTokens: maxTokens,
	})
	l.debugContextDecide(step, governed)
	return governed.Messages
}

// recoverFromPromptTooLong runs emergency (deterministic) compaction after a
// provider prompt_too_long error, then retries the step once. Full LLM compact
// remains App-level (/compact and pre-turn threshold); mid-step reactive path
// must not consume extra model turns that would desync fake/test clients.
func (l *Loop) recoverFromPromptTooLong(messages []llm.Message, emit EventSink) []llm.Message {
	if l.contextManager == nil {
		return messages
	}
	result := l.contextManager.EmergencyShrink(messages)
	maxTokens := l.maxContextTokens
	if maxTokens <= 0 {
		maxTokens = l.contextManager.Config().MaxTokens
	}
	emit(Event{
		Type:    EventCompactionStarted,
		Message: fmt.Sprintf("reactive compaction (prompt too long), ~%d tokens", result.BeforeTokens),
	})
	if l.contextSummary != nil && strings.TrimSpace(result.Summary) != "" {
		artifact := result.Artifact
		if artifact.Summary == "" {
			artifact.Summary = result.Summary
			artifact.BeforeTokens = result.BeforeTokens
			artifact.AfterTokens = result.AfterTokens
			artifact.Compacted = true
		}
		_ = l.contextSummary.Save(artifact)
	}
	// CompactionFinished must arrive before any ContextUpdated that writes the
	// after-token count, so the TUI divider can read the prior ContextTokens as
	// "before" (same contract as App.Compact).
	emit(Event{
		Type:             EventCompactionFinished,
		Message:          fmt.Sprintf("reactive · %d -> %d tokens", result.BeforeTokens, result.AfterTokens),
		ContextTokens:    result.AfterTokens,
		ContextMaxTokens: maxTokens,
	})
	if len(result.Messages) == 0 {
		return messages
	}
	return result.Messages
}

func historyHasCompactSummary(messages []llm.Message) bool {
	for _, msg := range messages {
		if msg.Role == llm.RoleUser && strings.Contains(msg.Content, "was compacted into the following summary") {
			return true
		}
	}
	return false
}

func withContextSummarySection(messages []llm.Message, section string) []llm.Message {
	section = strings.TrimSpace(section)
	if section == "" {
		return messages
	}
	out := append([]llm.Message(nil), messages...)
	promptSection := "<system-reminder>\n## Context Summary\n" + section + "\n</system-reminder>"
	bodyStart := 0
	if len(out) > 0 && out[0].Role == llm.RoleSystem {
		bodyStart = 1
	}
	if bodyStart < len(out) && out[bodyStart].Role == llm.RoleUser {
		if strings.Contains(out[bodyStart].Content, "## Context Summary") {
			return out
		}
		out[bodyStart].Content = strings.TrimSpace(promptSection + "\n\n" + out[bodyStart].Content)
		return out
	}
	continuation := llm.Message{Role: llm.RoleUser, Content: promptSection}
	out = append(out, llm.Message{})
	copy(out[bodyStart+1:], out[bodyStart:])
	out[bodyStart] = continuation
	return out
}

func appendReminderToSystem(messages []llm.Message, reminder string) []llm.Message {
	out := append([]llm.Message(nil), messages...)
	if len(out) > 0 && out[0].Role == llm.RoleSystem {
		out[0].Content = strings.TrimSpace(out[0].Content + "\n\n" + reminder)
		return out
	}
	return append([]llm.Message{{Role: llm.RoleSystem, Content: reminder}}, out...)
}

func appendPlanningReminder(messages []llm.Message) []llm.Message {
	reminder := llm.Message{
		Role: llm.RoleSystem,
		Content: "Planning reminder: this task has used tools for several steps without a todo plan. " +
			"If the work has 3 or more steps, call todo_write with a concise plan before continuing.",
	}
	out := append([]llm.Message(nil), messages...)
	if len(out) > 0 && out[0].Role == llm.RoleSystem {
		out[0].Content = strings.TrimSpace(out[0].Content + "\n\n" + reminder.Content)
		return out
	}
	return append([]llm.Message{reminder}, out...)
}

func hasTaskContext(messages []llm.Message) bool {
	if len(messages) == 0 || messages[0].Role != llm.RoleSystem {
		return false
	}
	content := messages[0].Content
	return strings.Contains(content, "## Active Tasks") || strings.Contains(content, "# Tasks")
}

// summarizeHistoryWithLLM is retained for unit tests and optional callers that
// want a one-shot checkpoint summary without going through App.Compact.
func (l *Loop) summarizeHistoryWithLLM(ctx context.Context, messages []llm.Message, previous string) (string, error) {
	return CompleteText(ctx, l.client, SummarizationMessages(messages, previous))
}
