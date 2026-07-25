package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/tool"
)

// MemorySaveTool writes one topic memory file and updates MEMORY.md (CC two-step
// save collapsed into one audited tool call — BondCode keeps an explicit tool
// rather than free-form write_file into the memdir).
type MemorySaveTool struct {
	store *MemoryStore
}

func NewMemorySaveTool(store *MemoryStore) *MemorySaveTool {
	return &MemorySaveTool{store: store}
}

func (t *MemorySaveTool) Name() string { return tool.MemorySave }

func (t *MemorySaveTool) Description() string {
	return "Save one durable memory to the file-based memory directory. " +
		"Types: user (role/preferences), feedback (what to avoid AND repeat — include Why/How to apply), " +
		"project (non-code project context; absolute dates), reference (external system pointers). " +
		"Do NOT save code patterns, architecture, git history, or ephemeral task state. " +
		"Provide a specific description (used for future relevance selection). " +
		"Updates an existing topic when filename/name matches; otherwise creates a new topic file and index entry."
}

func (t *MemorySaveTool) Schema() any {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"type": map[string]interface{}{
				"type":        "string",
				"description": "Memory type.",
				"enum":        []string{"user", "feedback", "project", "reference"},
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Short memory title (becomes the topic identity).",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "One-line description used to decide relevance in future conversations — be specific.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Memory body. For feedback/project: rule/fact, then **Why:** and **How to apply:**.",
			},
			"filename": map[string]interface{}{
				"type":        "string",
				"description": "Optional topic filename (e.g. feedback_testing.md). Derived from type+name when omitted.",
			},
		},
		"required": []string{"type", "name", "description", "content"},
	}
}

func (t *MemorySaveTool) Risk(args json.RawMessage) tool.RiskLevel { return tool.RiskMedium }

func (t *MemorySaveTool) Execute(ctx context.Context, args json.RawMessage) (*tool.Result, error) {
	var parsed SaveArgs
	if err := json.Unmarshal(args, &parsed); err != nil {
		return tool.ErrorResult(t.Name(), "invalid JSON arguments", err.Error()), nil
	}
	typ, ok := ParseType(parsed.Type)
	if !ok {
		return tool.ErrorResult(t.Name(), "invalid type", "type must be user|feedback|project|reference"), nil
	}
	file := MemoryFile{
		Type:        typ,
		Name:        parsed.Name,
		Description: parsed.Description,
		Body:        parsed.Content,
		Filename:    parsed.Filename,
	}
	if err := t.store.Save(file); err != nil {
		return tool.ErrorResult(t.Name(), "failed to save memory", err.Error()), nil
	}
	t.store.MarkModelWrite()
	// Re-derive filename for the success message.
	savedName := file.Filename
	if savedName == "" {
		savedName = deriveFilename(typ, parsed.Name)
	} else {
		savedName = sanitizeFilename(savedName)
	}
	return tool.Success(t.Name(), "memory saved",
		fmt.Sprintf("Saved %s memory to %s and updated MEMORY.md index.", typ, savedName)), nil
}

// MemorySearchTool searches topic memories by query/type.
type MemorySearchTool struct {
	store    *MemoryStore
	maxChars int
}

func NewMemorySearchTool(store *MemoryStore, maxChars int) *MemorySearchTool {
	return &MemorySearchTool{store: store, maxChars: maxChars}
}

func (t *MemorySearchTool) Name() string { return tool.MemorySearch }

func (t *MemorySearchTool) Description() string {
	return "Search the file-based memory directory by keyword and optional type " +
		"(user|feedback|project|reference). Returns matching topic memories with age notes. Does not mutate files."
}

func (t *MemorySearchTool) Schema() any {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "Keyword query."},
			"type": map[string]interface{}{
				"type": "string", "description": "Optional type filter.",
				"enum": []string{"user", "feedback", "project", "reference"},
			},
			"limit":     map[string]interface{}{"type": "integer", "description": "Maximum number of memories (default 8)."},
			"max_chars": map[string]interface{}{"type": "integer", "description": "Maximum output characters."},
		},
	}
}

func (t *MemorySearchTool) Risk(args json.RawMessage) tool.RiskLevel { return tool.RiskLow }

func (t *MemorySearchTool) Execute(ctx context.Context, args json.RawMessage) (*tool.Result, error) {
	var parsed SearchArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &parsed); err != nil {
			return tool.ErrorResult(t.Name(), "invalid JSON arguments", err.Error()), nil
		}
	}
	maxChars := t.maxChars
	if parsed.MaxChars > 0 {
		maxChars = parsed.MaxChars
	}
	limit := parsed.Limit
	if limit <= 0 {
		limit = 8
	}
	var typ MemoryType
	if parsed.Type != "" {
		tpe, ok := ParseType(parsed.Type)
		if !ok {
			return tool.ErrorResult(t.Name(), "invalid type", "type must be user|feedback|project|reference"), nil
		}
		typ = tpe
	}
	files, err := t.store.Search(SearchOptions{
		Query:    parsed.Query,
		Type:     typ,
		Limit:    limit,
		MaxChars: maxChars,
	})
	if err != nil {
		return tool.ErrorResult(t.Name(), "failed to search memory", err.Error()), nil
	}
	if len(files) == 0 {
		return tool.Success(t.Name(), "memory search", "No matching memories."), nil
	}
	return tool.Success(t.Name(), "memory search", formatSearchResults(files)), nil
}

func formatSearchResults(files []MemoryFile) string {
	var b strings.Builder
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "%s [%s] · %s\n", f.Filename, f.Type, AgeText(f.MtimeMs))
		if f.Description != "" {
			b.WriteString(f.Description)
			b.WriteString("\n")
		}
		if note := FreshnessText(f.MtimeMs); note != "" {
			b.WriteString(note)
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimSpace(f.Body))
	}
	return b.String()
}
