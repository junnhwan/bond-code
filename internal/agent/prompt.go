package agent

import (
	"strings"

	"github.com/junnhwan/bond-code/internal/llm"
)

func buildMessages(prompt string) []llm.Message {
	return NewMessages(prompt)
}

func NewMessages(prompt string) []llm.Message {
	return NewMessagesWithContext(prompt, RuntimePromptContext{})
}

func NewMessagesWithContext(prompt string, ctx RuntimePromptContext) []llm.Message {
	userContent := prompt
	if reminder := DynamicReminder(ctx); reminder != "" {
		userContent = reminder + "\n\n" + prompt
	}
	return []llm.Message{
		{Role: llm.RoleSystem, Content: BuildSystemPrompt(ctx)},
		{Role: llm.RoleUser, Content: userContent},
	}
}

// SkillListingPrompt is kept for callers that still pass a preformatted listing
// string. Prefer skill.FormatListing + RuntimePromptContext.SkillsListing.
func SkillListingPrompt(listing string) string {
	return strings.TrimSpace(listing)
}
