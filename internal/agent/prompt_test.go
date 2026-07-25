package agent

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptEmptyContextIsConciseAndContainsStopRules(t *testing.T) {
	prompt := BuildSystemPrompt(RuntimePromptContext{})

	if !strings.Contains(prompt, "When no tool is needed, answer directly and stop.") {
		t.Fatalf("expected direct-answer stop rule, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "give the final answer instead of looping on more calls") {
		t.Fatalf("expected repeated-tool stop rule, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "read_file / list_dir / search_text instead of run_command") {
		t.Fatalf("expected file-read tool selection rule, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "## Memory") {
		t.Fatalf("empty context should not include memory guidance section, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "## Active Tasks") {
		t.Fatalf("empty context should not include active task section, got:\n%s", prompt)
	}
	// The static system prompt is intentionally enriched with CC-derived
	// guidance (working style, tool use, verification, safety, communication).
	// This ceiling guards against unbounded growth, not tightness — a real
	// coding agent's system prompt runs to several KB (Claude Code's is far
	// larger). Raise only with deliberation; trim prose first.
	if len(prompt) > 9000 {
		t.Fatalf("expected reasonably bounded empty system prompt, got %d chars", len(prompt))
	}
}

func TestBuildSystemPromptStaticOnlyAndDynamicReminderSeparate(t *testing.T) {
	ctx := RuntimePromptContext{
		ProjectRoot:    `D:\dev\my_proj\go\bond-code`,
		MemoryGuidance: "Types of memory include user and feedback.",
		Memory:         "- User prefers Chinese answers.",
		Tasks:          "- in_progress: Implement prompt context.",
		ContextStats:   "context tokens: 1200/100000",
		ToolPolicy:     "low risk tools can run automatically; medium risk requires confirmation",
		SkillsListing:  "- debugging: debug systematically",
		ContextSummary: "Summary:\nprevious work",
	}
	prompt := BuildSystemPrompt(ctx)

	// Static sections stay in the system prompt — including stable memory guidance.
	for _, want := range []string{
		"## Project",
		`D:\dev\my_proj\go\bond-code`,
		"## Tool Policy",
		"medium risk requires confirmation",
		"## Memory",
		"Types of memory include user and feedback.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected static %q in system prompt, got:\n%s", want, prompt)
		}
	}
	// Volatile content must NOT be in the system prompt (they bust the cache).
	for _, unwanted := range []string{
		"User prefers Chinese", "Implement prompt context", "previous work",
		"## Active Tasks", "## Context Summary", "## Context Governance",
		"## Available Skills", "debugging",
	} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("volatile %q must not be in system prompt, got:\n%s", unwanted, prompt)
		}
	}

	// Volatile sections go into the dynamic reminder instead.
	reminder := DynamicReminder(ctx)
	for _, want := range []string{
		"<system-reminder>",
		"## Memory", "User prefers Chinese answers.",
		"## Active Tasks", "Implement prompt context.",
		"## Context Summary", "previous work",
		"## Context Governance", "context tokens: 1200/100000",
		"## Available Skills", "debugging",
		"</system-reminder>",
	} {
		if !strings.Contains(reminder, want) {
			t.Fatalf("expected %q in dynamic reminder, got:\n%s", want, reminder)
		}
	}

	// Empty context produces no reminder.
	if DynamicReminder(RuntimePromptContext{}) != "" {
		t.Fatalf("empty context should produce no reminder, got: %s", DynamicReminder(RuntimePromptContext{}))
	}
}

func TestDynamicReminderRendersContextSummaryBeforeGovernance(t *testing.T) {
	reminder := DynamicReminder(RuntimePromptContext{
		ContextSummary: "Summary:\nprevious work",
		ContextStats:   "context tokens: 10 -> 8",
	})

	summaryIdx := strings.Index(reminder, "## Context Summary")
	if summaryIdx < 0 {
		t.Fatalf("expected context summary in reminder, got:\n%s", reminder)
	}
	governanceIdx := strings.Index(reminder, "## Context Governance")
	if governanceIdx < 0 {
		t.Fatalf("expected context governance in reminder, got:\n%s", reminder)
	}
	if summaryIdx > governanceIdx {
		t.Fatalf("expected context summary before governance, got:\n%s", reminder)
	}
	if !strings.Contains(reminder, "previous work") || !strings.Contains(reminder, "context tokens: 10 -> 8") {
		t.Fatalf("expected both context sections in reminder, got:\n%s", reminder)
	}
}

func TestBuildSystemPromptDoesNotReadAPIKeyEnvironment(t *testing.T) {
	t.Setenv("BONDCODE_API_KEY", "secret-value-that-must-not-appear")

	prompt := BuildSystemPrompt(RuntimePromptContext{
		Memory: "safe memory text",
	})

	if strings.Contains(prompt, "secret-value-that-must-not-appear") {
		t.Fatalf("system prompt must not include API key environment values:\n%s", prompt)
	}
}

func TestNewMessagesWithContextPutsMemoryInUserReminder(t *testing.T) {
	messages := NewMessagesWithContext("hello", RuntimePromptContext{Memory: "safe memory text"})

	if len(messages) != 2 {
		t.Fatalf("expected system and user messages, got %#v", messages)
	}
	// Memory is now in the user turn (dynamic reminder), NOT the system prompt,
	// so the system prompt stays cache-stable across turns.
	if strings.Contains(messages[0].Content, "safe memory text") {
		t.Fatalf("memory must not be in system prompt, got %#v", messages[0])
	}
	if !strings.Contains(messages[1].Content, "safe memory text") {
		t.Fatalf("expected memory in user reminder, got %#v", messages[1])
	}
	if !strings.Contains(messages[1].Content, "hello") {
		t.Fatalf("expected user prompt preserved in user turn, got %#v", messages[1])
	}
}
