package app

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/memory"
)

// selectRelevantMemories mirrors Claude Code findRelevantMemories: scan headers
// (name/description/type), ask the model to pick only clearly useful files
// (up to max), then load full topic bodies. Falls back to keyword search.
func selectRelevantMemories(ctx context.Context, client llm.Client, store *memory.MemoryStore, query string, max int) ([]memory.MemoryFile, error) {
	if store == nil {
		return nil, nil
	}
	if max <= 0 {
		max = 5
	}
	headers, err := store.Scan()
	if err != nil {
		return nil, err
	}
	if len(headers) == 0 {
		return nil, nil
	}
	// Small pool: no side-query needed (and client may be nil in tests).
	if len(headers) <= max {
		return loadHeaders(store, headers, max)
	}
	if client == nil {
		return keywordFallback(store, query, max)
	}

	text, err := agent.CompleteText(ctx, client, []llm.Message{
		{Role: llm.RoleUser, Content: memorySelectionPrompt(query, headers, max)},
	})
	if err != nil || strings.TrimSpace(text) == "" {
		return keywordFallback(store, query, max)
	}
	names := parseStringArray(text)
	if len(names) == 0 {
		// Also accept integer indices.
		indices := parseIntArray(text)
		if len(indices) == 0 {
			return keywordFallback(store, query, max)
		}
		var picked []memory.MemoryHeader
		for _, i := range indices {
			if i < 0 || i >= len(headers) {
				continue
			}
			picked = append(picked, headers[i])
			if len(picked) >= max {
				break
			}
		}
		if len(picked) == 0 {
			return keywordFallback(store, query, max)
		}
		return loadHeaders(store, picked, max)
	}

	byName := map[string]memory.MemoryHeader{}
	for _, h := range headers {
		byName[h.Filename] = h
	}
	var picked []memory.MemoryHeader
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		h, ok := byName[name]
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		picked = append(picked, h)
		if len(picked) >= max {
			break
		}
	}
	if len(picked) == 0 {
		return keywordFallback(store, query, max)
	}
	return loadHeaders(store, picked, max)
}

func loadHeaders(store *memory.MemoryStore, headers []memory.MemoryHeader, max int) ([]memory.MemoryFile, error) {
	out := make([]memory.MemoryFile, 0, len(headers))
	for _, h := range headers {
		f, err := store.Read(h.Filename)
		if err != nil {
			continue
		}
		out = append(out, *f)
		if len(out) >= max {
			break
		}
	}
	return out, nil
}

func keywordFallback(store *memory.MemoryStore, query string, max int) ([]memory.MemoryFile, error) {
	return store.Search(memory.SearchOptions{Query: query, Limit: max})
}

func memorySelectionPrompt(query string, headers []memory.MemoryHeader, max int) string {
	var b strings.Builder
	b.WriteString("You are selecting memories that will be useful while processing a user's query.\n")
	b.WriteString(fmt.Sprintf("Return ONLY a JSON array of filenames (up to %d). ", max))
	b.WriteString("Only include memories that will clearly be useful based on name and description. ")
	b.WriteString("If unsure, omit it. If none are clearly useful, return [].\n\n")
	b.WriteString("Query:\n")
	b.WriteString(strings.TrimSpace(query))
	b.WriteString("\n\nAvailable memories:\n")
	b.WriteString(memory.FormatManifest(headers))
	return b.String()
}

var intArrayRe = regexp.MustCompile(`(?s)\[.*?\]`)

func parseIntArray(text string) []int {
	match := intArrayRe.FindString(text)
	if match == "" {
		return nil
	}
	var out []int
	if err := json.Unmarshal([]byte(match), &out); err != nil {
		return nil
	}
	return out
}

func parseStringArray(text string) []string {
	match := intArrayRe.FindString(text)
	if match == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(match), &out); err != nil {
		return nil
	}
	return out
}
