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

// MemoryExtractorConfig controls automatic memory extraction (CC extractMemories).
type MemoryExtractorConfig struct {
	Enabled             bool
	MaxDialogueMessages int
	MaxDialogueChars    int
}

func DefaultMemoryExtractorConfig() MemoryExtractorConfig {
	return MemoryExtractorConfig{Enabled: true, MaxDialogueMessages: 12, MaxDialogueChars: 8000}
}

// MemoryExtractor runs an LLM pass over recent dialogue after a turn and writes
// topic memories via the memdir store. Skips when the main agent already called
// memory_save this turn (CC hasMemoryWritesSince mutual exclusion).
//
// Failures are non-fatal: extraction must never block or break the main turn.
type MemoryExtractor struct {
	client llm.Client
	store  *memory.MemoryStore
	cfg    MemoryExtractorConfig
}

func NewMemoryExtractor(client llm.Client, store *memory.MemoryStore, cfg MemoryExtractorConfig) *MemoryExtractor {
	if cfg.MaxDialogueMessages <= 0 || cfg.MaxDialogueChars <= 0 {
		d := DefaultMemoryExtractorConfig()
		if cfg.MaxDialogueMessages <= 0 {
			cfg.MaxDialogueMessages = d.MaxDialogueMessages
		}
		if cfg.MaxDialogueChars <= 0 {
			cfg.MaxDialogueChars = d.MaxDialogueChars
		}
	}
	return &MemoryExtractor{client: client, store: store, cfg: cfg}
}

type extractedMemory struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Filename    string `json:"filename"`
}

// Extract runs one extraction pass. Returns the count written.
func (e *MemoryExtractor) Extract(ctx context.Context, messages []llm.Message) (int, error) {
	if e == nil || e.store == nil || e.client == nil {
		return 0, nil
	}
	// Mutual exclusion with main-agent memory_save (CC extractMemories).
	if e.store.ConsumeModelWrite() {
		return 0, nil
	}
	dialogue := recentDialogue(messages, e.cfg.MaxDialogueMessages, e.cfg.MaxDialogueChars)
	if strings.TrimSpace(dialogue) == "" {
		return 0, nil
	}
	headers, _ := e.store.Scan()
	text, err := agent.CompleteText(ctx, e.client, []llm.Message{
		{Role: llm.RoleUser, Content: extractionPrompt(dialogue, headers)},
	})
	if err != nil || strings.TrimSpace(text) == "" {
		return 0, err
	}
	items := parseExtractedMemories(text)
	if len(items) == 0 {
		return 0, nil
	}
	written := 0
	for _, it := range items {
		typ, ok := memory.ParseType(it.Type)
		if !ok {
			typ = memory.TypeProject
		}
		file := memory.MemoryFile{
			Type:        typ,
			Name:        strings.TrimSpace(it.Name),
			Description: strings.TrimSpace(it.Description),
			Body:        strings.TrimSpace(it.Content),
			Filename:    strings.TrimSpace(it.Filename),
		}
		if file.Name == "" {
			file.Name = string(typ) + " note"
		}
		if file.Description == "" {
			file.Description = file.Name
		}
		if file.Body == "" {
			continue
		}
		if err := e.store.Save(file); err != nil {
			continue
		}
		written++
	}
	return written, nil
}

func recentDialogue(messages []llm.Message, keep, maxChars int) string {
	if keep <= 0 {
		keep = 12
	}
	start := len(messages) - keep
	if start < 0 {
		start = 0
	}
	var parts []string
	for _, msg := range messages[start:] {
		c := strings.TrimSpace(msg.Content)
		if c == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", msg.Role, c))
	}
	dialogue := strings.Join(parts, "\n")
	if maxChars > 0 && len(dialogue) > maxChars {
		dialogue = dialogue[len(dialogue)-maxChars:]
	}
	return dialogue
}

func extractionPrompt(dialogue string, existing []memory.MemoryHeader) string {
	var b strings.Builder
	b.WriteString("You are the memory extraction subagent. Analyze the dialogue and update persistent memories.\n")
	b.WriteString("Return ONLY a JSON array. Each item: {\"type\",\"name\",\"description\",\"content\"} (optional \"filename\").\n")
	b.WriteString("- type: one of \"user\", \"feedback\", \"project\", \"reference\"\n")
	b.WriteString("- name: short title\n")
	b.WriteString("- description: one-line hook for future relevance selection (be specific)\n")
	b.WriteString("- content: durable body; for feedback/project include **Why:** and **How to apply:**\n\n")
	b.WriteString("Save only content NOT derivable from code/git: user role/prefs, feedback (corrections AND confirmations), project context (who/why/when with absolute dates), external references.\n")
	b.WriteString("Do NOT save: code patterns, architecture, file structure, git history, fix recipes, ephemeral task state, or anything already in project docs.\n")
	b.WriteString("Skip trivial or already-covered facts. If nothing new, return [].\n\n")
	b.WriteString("Existing memory files (update rather than duplicate):\n")
	b.WriteString(memory.FormatManifest(existing))
	b.WriteString("\n\nDialogue:\n")
	b.WriteString(dialogue)
	return b.String()
}

var jsonArrayRe = regexp.MustCompile(`(?s)\[.*\]`)

func parseExtractedMemories(text string) []extractedMemory {
	match := jsonArrayRe.FindString(text)
	if match == "" {
		return nil
	}
	var items []extractedMemory
	if err := json.Unmarshal([]byte(match), &items); err != nil {
		return nil
	}
	out := make([]extractedMemory, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.Content) == "" {
			continue
		}
		out = append(out, it)
	}
	return out
}
