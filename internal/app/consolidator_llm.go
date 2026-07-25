package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/memory"
)

// ConsolidateMemories is the lightweight "Dream" pass: when topic file count
// exceeds threshold, ask the LLM which filenames are stale/redundant, delete
// them, then rebuild MEMORY.md. Mirrors CC autoDream's consolidate-and-reindex
// intent without a full forked agent.
func ConsolidateMemories(ctx context.Context, client llm.Client, store *memory.MemoryStore, threshold int) (int, error) {
	if client == nil || store == nil {
		return 0, nil
	}
	if threshold <= 0 {
		threshold = 40
	}
	headers, err := store.Scan()
	if err != nil {
		return 0, err
	}
	if len(headers) < threshold {
		// Still refresh the index so it stays consistent with topic files.
		_, _ = store.RebuildIndex()
		return 0, nil
	}
	text, err := agent.CompleteText(ctx, client, []llm.Message{
		{Role: llm.RoleUser, Content: dreamPrompt(headers)},
	})
	if err != nil || strings.TrimSpace(text) == "" {
		_, _ = store.RebuildIndex()
		return 0, err
	}
	names := parseStringArray(text)
	deleted := 0
	byName := map[string]bool{}
	for _, h := range headers {
		byName[h.Filename] = true
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !byName[name] {
			continue
		}
		if err := store.Delete(name); err == nil {
			deleted++
		}
	}
	_, _ = store.RebuildIndex()
	return deleted, nil
}

func dreamPrompt(headers []memory.MemoryHeader) string {
	var b strings.Builder
	b.WriteString("You are consolidating a coding-agent memory directory (a \"dream\" pass).\n")
	b.WriteString("Return ONLY a JSON array of filenames to DELETE — stale, redundant, contradicted, or no longer load-bearing memories.\n")
	b.WriteString("Keep memories that still guide future sessions (user prefs, durable feedback, active project context, external references).\n")
	b.WriteString("If nothing should be deleted, return [].\n\n")
	b.WriteString("Memory files:\n")
	b.WriteString(memory.FormatManifest(headers))
	b.WriteString(fmt.Sprintf("\n\n(%d files total)\n", len(headers)))
	return b.String()
}
