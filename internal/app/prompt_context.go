package app

import (
	"context"
	"strings"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/memory"
	"github.com/junnhwan/bond-code/internal/skill"
)

func (a *App) messagesForPrompt(c context.Context, prompt string) []llm.Message {
	runtimeContext := a.runtimePromptContext(c, prompt)
	if len(a.history) == 0 {
		return agent.NewMessagesWithContext(prompt, runtimeContext)
	}
	messages := append([]llm.Message(nil), a.history...)
	systemPrompt := agent.BuildSystemPrompt(runtimeContext)
	if len(messages) > 0 && messages[0].Role == llm.RoleSystem {
		messages[0].Content = systemPrompt
	} else {
		messages = append([]llm.Message{{Role: llm.RoleSystem, Content: systemPrompt}}, messages...)
	}
	// Volatile context (memory body, tasks, summary, stats) rides on the current
	// user turn as a <system-reminder>, NOT the system prompt, so the system
	// prefix stays cache-stable. See agent.DynamicReminder.
	userContent := prompt
	if reminder := agent.DynamicReminder(runtimeContext); reminder != "" {
		userContent = reminder + "\n\n" + prompt
	}
	return append(messages, llm.Message{Role: llm.RoleUser, Content: userContent})
}

func (a *App) runtimePromptContext(c context.Context, currentPrompt string) agent.RuntimePromptContext {
	ctx := a.RuntimePromptContext
	ctx.PlanMode = a.planMode
	ctx = a.runtimeSummaryPromptContext(ctx)
	ctx = a.runtimeSkillPromptContext(ctx)
	if a.MemoryStore == nil {
		return a.runtimeTaskPromptContext(ctx)
	}
	if a.Config != nil && !a.Config.Memory.Enabled {
		return a.runtimeTaskPromptContext(ctx)
	}

	// Stable behavioral guidance → system prompt (CC loadMemoryPrompt).
	ctx.MemoryGuidance = memory.GuidancePrompt(a.MemoryStore.Dir())

	maxChars := a.MemoryMaxChars
	if maxChars <= 0 {
		maxChars = 4000
	}
	maxRelevant := 5
	if a.Config != nil && a.Config.Memory.MaxRelevant > 0 {
		maxRelevant = a.Config.Memory.MaxRelevant
	}

	index, err := a.MemoryStore.GetMemoryContext(maxChars)
	if err != nil {
		warning := "memory index read failed: " + err.Error()
		if ctx.ContextStats == "" {
			ctx.ContextStats = warning
		} else {
			ctx.ContextStats += "\n" + warning
		}
		return a.runtimeTaskPromptContext(ctx)
	}

	query := strings.Join(append([]string{currentPrompt}, a.recentUserPrompts()...), " ")
	relevant := a.selectMemoryFiles(c, query, maxRelevant)
	ctx.Memory = memory.ComposeInjection(index, relevant, maxChars)
	return a.runtimeTaskPromptContext(ctx)
}

// selectMemoryFiles picks topic memories for injection: LLM side-query when
// memory.llm_select is on (falls back to keyword search), otherwise keyword
// Search. Capped at max (CC default 5).
func (a *App) selectMemoryFiles(c context.Context, query string, max int) []memory.MemoryFile {
	if max <= 0 {
		max = 5
	}
	if a.Config != nil && a.Config.Memory.LLMSelect && a.LLM != nil {
		if files, err := selectRelevantMemories(c, a.LLM, a.MemoryStore, query, max); err == nil && files != nil {
			return files
		}
	}
	files, _ := a.MemoryStore.Search(memory.SearchOptions{Query: query, Limit: max})
	return files
}

func (a *App) recentUserPrompts() []string {
	if len(a.history) == 0 {
		return nil
	}
	var prompts []string
	for i := len(a.history) - 1; i >= 0 && len(prompts) < 4; i-- {
		if a.history[i].Role == llm.RoleUser {
			prompts = append(prompts, a.history[i].Content)
		}
	}
	return prompts
}

func (a *App) runtimeSkillPromptContext(ctx agent.RuntimePromptContext) agent.RuntimePromptContext {
	if a.SkillLoader == nil {
		return ctx
	}
	if a.Config != nil && !a.Config.Skills.Enabled {
		return ctx
	}
	index, err := a.SkillLoader.Index()
	if err != nil {
		warning := "skill index read failed: " + err.Error()
		if ctx.ContextStats == "" {
			ctx.ContextStats = warning
		} else {
			ctx.ContextStats += "\n" + warning
		}
		return ctx
	}
	if len(index) == 0 {
		return ctx
	}
	// Budgeted listing rides in DynamicReminder (volatile), not system prompt.
	ctx.SkillsListing = skill.FormatListing(index, a.SkillLoader.ListingBudget())
	return ctx
}

func (a *App) runtimeSummaryPromptContext(ctx agent.RuntimePromptContext) agent.RuntimePromptContext {
	if a.ContextSummary == nil {
		return ctx
	}
	artifact, err := a.ContextSummary.Load()
	if err != nil {
		warning := "context summary read failed: " + err.Error()
		if ctx.ContextStats == "" {
			ctx.ContextStats = warning
		} else {
			ctx.ContextStats += "\n" + warning
		}
		return ctx
	}
	if artifact == nil {
		return ctx
	}
	ctx.ContextSummary = artifact.PromptSection(4000)
	return ctx
}

func (a *App) runtimeTaskPromptContext(ctx agent.RuntimePromptContext) agent.RuntimePromptContext {
	if a.TaskStore == nil {
		return ctx
	}
	if a.Config != nil && (!a.Config.Planning.Enabled || !a.Config.Planning.InjectTasks) {
		return ctx
	}
	tasksText, err := a.TaskStore.FormatForPrompt()
	if err != nil {
		warning := "task read failed: " + err.Error()
		if ctx.ContextStats == "" {
			ctx.ContextStats = warning
		} else {
			ctx.ContextStats += "\n" + warning
		}
		return ctx
	}
	ctx.Tasks = tasksText
	return ctx
}
