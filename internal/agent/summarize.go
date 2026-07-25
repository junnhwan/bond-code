package agent

import (
	"context"
	"strings"

	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
)

// Pi-style checkpoint summarizer (neutral wording for coding agent).
const summarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI coding assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

const summarizationUserPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const updateSummarizationUserPrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use the same EXACT section format (Goal, Constraints & Preferences, Progress, Key Decisions, Next Steps, Critical Context).

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// SummarizationMessages builds the LLM request for compaction (Pi serialize + checkpoint).
// history should be the span being summarized (not necessarily the full session).
func SummarizationMessages(history []llm.Message, previousSummary string) []llm.Message {
	conversation := contextx.SerializeConversation(history)
	var b strings.Builder
	b.WriteString("<conversation>\n")
	b.WriteString(conversation)
	b.WriteString("\n</conversation>\n\n")
	prev := strings.TrimSpace(previousSummary)
	if prev != "" {
		b.WriteString("<previous-summary>\n")
		b.WriteString(prev)
		b.WriteString("\n</previous-summary>\n\n")
		b.WriteString(updateSummarizationUserPrompt)
	} else {
		b.WriteString(summarizationUserPrompt)
	}
	return []llm.Message{
		{Role: llm.RoleSystem, Content: summarizationSystemPrompt},
		{Role: llm.RoleUser, Content: b.String()},
	}
}

// CompleteText performs a streaming completion with no tools and returns the
// accumulated assistant text. Used for one-shot summarization.
func CompleteText(ctx context.Context, client llm.Client, messages []llm.Message) (string, error) {
	chunks, errs := client.Stream(ctx, messages, nil)
	var b strings.Builder
	for chunk := range chunks {
		if chunk.Content != "" {
			b.WriteString(chunk.Content)
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err := <-errs; err != nil {
		return strings.TrimSpace(b.String()), err
	}
	return strings.TrimSpace(b.String()), nil
}
