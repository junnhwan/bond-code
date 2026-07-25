package memory

import (
	"fmt"
	"strings"
)

// GuidancePrompt returns the stable behavioral memory instructions for the
// system prompt (CC loadMemoryPrompt / buildMemoryLines — without MEMORY.md
// content). Content injection stays on the user turn for prompt-cache stability.
func GuidancePrompt(memoryDir string) string {
	var b strings.Builder
	b.WriteString("You have a persistent, file-based memory system at `")
	b.WriteString(memoryDir)
	b.WriteString("`. This directory already exists — use the memory_save tool to write (do not mkdir).\n\n")
	b.WriteString("Build up this memory over time so future conversations know who the user is, how they like to collaborate, what to avoid or repeat, and the context behind their work.\n\n")
	b.WriteString("If the user explicitly asks you to remember something, save it immediately as whichever type fits best. If they ask you to forget something, say so and use memory tools / ask the user to confirm removal.\n\n")
	b.WriteString(typesSection())
	b.WriteString("\n")
	b.WriteString(whatNotToSaveSection())
	b.WriteString("\n")
	b.WriteString(howToSaveSection())
	b.WriteString("\n")
	b.WriteString(whenToAccessSection())
	b.WriteString("\n")
	b.WriteString(trustingRecallSection())
	b.WriteString("\n")
	b.WriteString(persistenceDistinctionSection())
	return strings.TrimSpace(b.String())
}

func typesSection() string {
	return strings.Join([]string{
		"## Types of memory",
		"",
		"There are several discrete types of memory:",
		"",
		"<types>",
		"<type>",
		"    <name>user</name>",
		"    <description>The user's role, goals, responsibilities, and knowledge. Tailor future behavior to their preferences and perspective. Avoid negative judgments about the user.</description>",
		"    <when_to_save>When you learn details about the user's role, preferences, responsibilities, or knowledge</when_to_save>",
		"</type>",
		"<type>",
		"    <name>feedback</name>",
		"    <description>Guidance about how to approach work — both what to avoid AND what to keep doing. Record from failure AND success: if you only save corrections you drift away from validated approaches.</description>",
		"    <when_to_save>Any time the user corrects your approach OR confirms a non-obvious approach worked. Include *why* so you can judge edge cases later.</when_to_save>",
		"    <body_structure>Lead with the rule, then a **Why:** line and a **How to apply:** line.</body_structure>",
		"</type>",
		"<type>",
		"    <name>project</name>",
		"    <description>Ongoing work, goals, initiatives, bugs, or incidents that are NOT derivable from code or git history — who is doing what, why, by when. Convert relative dates to absolute dates when saving.</description>",
		"    <when_to_save>When you learn non-code project context that will matter in future sessions</when_to_save>",
		"    <body_structure>Lead with the fact/decision, then **Why:** and **How to apply:**.</body_structure>",
		"</type>",
		"<type>",
		"    <name>reference</name>",
		"    <description>Pointers to external systems (Linear projects, Slack channels, dashboards) and their purpose.</description>",
		"    <when_to_save>When you learn about resources outside the project directory</when_to_save>",
		"</type>",
		"</types>",
	}, "\n")
}

func whatNotToSaveSection() string {
	return strings.Join([]string{
		"## What NOT to save in memory",
		"",
		"- Code patterns, conventions, architecture, file paths, or project structure — derive these by reading the project.",
		"- Git history, recent changes, or who-changed-what — `git log` / `git blame` are authoritative.",
		"- Debugging solutions or fix recipes — the fix is in the code; the commit message has the context.",
		"- Anything already documented in project instruction files (CLAUDE.md, AGENTS.md, README).",
		"- Ephemeral task details: in-progress work, temporary state, current conversation context.",
		"",
		"These exclusions apply even when the user explicitly asks you to save. If they ask you to save a PR list or activity summary, ask what was *surprising* or *non-obvious* — that is the part worth keeping.",
	}, "\n")
}

func howToSaveSection() string {
	return strings.Join([]string{
		"## How to save memories",
		"",
		"Use the `memory_save` tool. Each memory is its own topic file with frontmatter:",
		"",
		"```markdown",
		"---",
		"name: {{memory name}}",
		"description: {{one-line description — used to decide relevance later, so be specific}}",
		"type: {{user, feedback, project, reference}}",
		"---",
		"",
		"{{memory content — for feedback/project: rule/fact, then **Why:** and **How to apply:**}}",
		"```",
		"",
		"- `MEMORY.md` is an **index**, not a dump — the harness maintains one-line hooks automatically when you call memory_save.",
		"- Organize memory semantically by topic, not chronologically.",
		"- Update an existing memory (same filename/topic) rather than writing duplicates.",
		"- Keep name, description, and type up to date with the content.",
	}, "\n")
}

func whenToAccessSection() string {
	return strings.Join([]string{
		"## When to access memories",
		"- When memories seem relevant, or the user references prior-conversation work.",
		"- You MUST access memory when the user explicitly asks you to check, recall, or remember.",
		"- If the user says to *ignore* or *not use* memory: proceed as if memory were empty. Do not apply remembered facts, cite, compare against, or mention memory content.",
		"- Memory records can become stale. Use memory as context for what was true at a point in time. Before answering or building assumptions solely from memory, verify against current files/resources. If a recalled memory conflicts with current information, trust what you observe now — and update or remove the stale memory.",
	}, "\n")
}

func trustingRecallSection() string {
	return strings.Join([]string{
		"## Before recommending from memory",
		"",
		"A memory that names a specific function, file, or flag is a claim that it existed *when the memory was written*. It may have been renamed, removed, or never merged. Before recommending it:",
		"",
		"- If the memory names a file path: check the file exists.",
		"- If the memory names a function or flag: search for it.",
		"- If the user is about to act on your recommendation (not just asking about history), verify first.",
		"",
		`"The memory says X exists" is not the same as "X exists now."`,
		"",
		"A memory that summarizes repo state is frozen in time. If the user asks about *recent* or *current* state, prefer git log or reading the code over recalling the snapshot.",
	}, "\n")
}

func persistenceDistinctionSection() string {
	return strings.Join([]string{
		"## Memory and other forms of persistence",
		"Memory is for information useful in *future* conversations.",
		"- Use a plan (or plan mode) for non-trivial implementation approaches in the *current* conversation — not memory.",
		"- Use todos/tasks for discrete steps and progress in the *current* conversation — not memory.",
	}, "\n")
}

// FormatManifest renders a header list for selector/extractor prompts.
func FormatManifest(headers []MemoryHeader) string {
	if len(headers) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for _, h := range headers {
		tag := ""
		if h.Type != "" {
			tag = fmt.Sprintf("[%s] ", h.Type)
		}
		ts := ""
		if h.MtimeMs > 0 {
			ts = " (" + AgeText(h.MtimeMs) + ")"
		}
		if h.Description != "" {
			fmt.Fprintf(&b, "- %s%s%s: %s\n", tag, h.Filename, ts, h.Description)
		} else {
			fmt.Fprintf(&b, "- %s%s%s\n", tag, h.Filename, ts)
		}
	}
	return strings.TrimSpace(b.String())
}

// RenderRelevant formats selected topic files for prompt injection, with age notes.
func RenderRelevant(files []MemoryFile, maxChars int) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	for i, f := range files {
		var block strings.Builder
		if i > 0 {
			block.WriteString("\n\n")
		}
		fmt.Fprintf(&block, "### %s", f.Filename)
		if f.Type != "" {
			fmt.Fprintf(&block, " [%s]", f.Type)
		}
		block.WriteString(" · ")
		block.WriteString(AgeText(f.MtimeMs))
		block.WriteString("\n")
		if f.Description != "" {
			block.WriteString(f.Description)
			block.WriteString("\n")
		}
		if note := FreshnessText(f.MtimeMs); note != "" {
			block.WriteString(note)
			block.WriteString("\n")
		}
		block.WriteString(strings.TrimSpace(f.Body))
		block.WriteString("\n")
		if maxChars > 0 && b.Len() > 0 && b.Len()+block.Len() > maxChars {
			break
		}
		if maxChars > 0 && b.Len() == 0 && block.Len() > maxChars {
			s := block.String()
			b.WriteString(s[:maxChars])
			break
		}
		b.WriteString(block.String())
	}
	return strings.TrimSpace(b.String())
}

// ComposeInjection builds the volatile memory block for <system-reminder>:
// MEMORY.md index + relevant topic bodies.
func ComposeInjection(index string, relevant []MemoryFile, maxChars int) string {
	index = strings.TrimSpace(index)
	body := RenderRelevant(relevant, 0)
	var b strings.Builder
	if index != "" {
		b.WriteString("### MEMORY.md (index)\n")
		b.WriteString(index)
	}
	if body != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("### Relevant memories\n")
		b.WriteString(body)
	}
	out := strings.TrimSpace(b.String())
	if maxChars > 0 && len(out) > maxChars {
		return out[:maxChars]
	}
	return out
}
