package contextx

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/junnhwan/bond-code/internal/llm"
)

type SummaryConfig struct {
	PreviousSummary string
	RecentMessages  int
	MaxSummaryChars int
	// LLMSummary, when non-empty, is used as the semantic summary body.
	LLMSummary string
}

type FileObservation struct {
	Path     string    `json:"path"`
	ToolName string    `json:"tool_name"`
	At       time.Time `json:"at"`
}

// SummaryArtifact is the persisted compaction checkpoint (version 2 = Pi-style).
type SummaryArtifact struct {
	Version         int               `json:"version"`
	Summary         string            `json:"summary"`
	RecentMessages  []Message         `json:"recent_messages,omitempty"`
	ReadFiles       []FileObservation `json:"read_files,omitempty"`
	ModifiedFiles   []FileObservation `json:"modified_files,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	BeforeTokens    int               `json:"before_tokens,omitempty"`
	AfterTokens     int               `json:"after_tokens,omitempty"`
	SnippedMessages int               `json:"snipped_messages,omitempty"` // legacy field; unused in v2
	Compacted       bool              `json:"compacted,omitempty"`
}

// BuildSummaryArtifact builds a fallback/deterministic artifact (tests + legacy paths).
func BuildSummaryArtifact(messages []Message, cfg SummaryConfig) SummaryArtifact {
	if cfg.RecentMessages <= 0 {
		cfg.RecentMessages = 8
	}
	if cfg.MaxSummaryChars <= 0 {
		cfg.MaxSummaryChars = 4000
	}
	summary := strings.TrimSpace(cfg.LLMSummary)
	if summary == "" {
		summary = renderDeterministicSummary(messages, cfg.PreviousSummary)
	}
	ops := extractFileOperations(messages)
	lists := computeFileLists(ops)
	return SummaryArtifact{
		Version:        2,
		Summary:        boundSummary(summary, cfg.MaxSummaryChars),
		RecentMessages: recentMessages(messages, cfg.RecentMessages),
		ReadFiles:      filePathsToObservations(lists.ReadFiles, true),
		ModifiedFiles:  filePathsToObservations(lists.ModifiedFiles, false),
		CreatedAt:      time.Now().UTC(),
	}
}

func (a SummaryArtifact) PromptSection(maxChars int) string {
	inventory := renderFileInventory(a.ReadFiles, a.ModifiedFiles)
	summary := strings.TrimSpace(a.Summary)
	if maxChars > 0 && len(summary)+len(inventory) > maxChars {
		available := maxChars - len(inventory)
		if available < 0 {
			available = 0
		}
		summary = truncateWithMarker(summary, available, "[context summary truncated]")
	}
	var b strings.Builder
	if summary != "" {
		b.WriteString("Summary:\n")
		b.WriteString(summary)
	}
	if inventory != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(inventory)
	}
	text := strings.TrimSpace(b.String())
	if maxChars > 0 && len(text) > maxChars+60 {
		return truncateWithMarker(text, maxChars, "[context summary truncated]")
	}
	return text
}

func renderDeterministicSummary(messages []Message, previous string) string {
	var b strings.Builder
	previous = strings.TrimSpace(previous)
	if previous != "" {
		b.WriteString("## Previous Summary\n")
		b.WriteString(previous)
		b.WriteString("\n\n")
	}
	b.WriteString("## Progress\n")
	for _, msg := range messages {
		if msg.Role == llm.RoleSystem || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		content := strings.ReplaceAll(strings.TrimSpace(msg.Content), "\n", " ")
		if len(content) > 240 {
			content = content[:240] + "..."
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", msg.Role, content))
	}
	return strings.TrimSpace(b.String())
}

func recentMessages(messages []Message, keep int) []Message {
	if keep >= len(messages) {
		return append([]Message(nil), messages...)
	}
	return append([]Message(nil), messages[len(messages)-keep:]...)
}

func boundSummary(summary string, maxChars int) string {
	return truncateWithMarker(strings.TrimSpace(summary), maxChars, "[summary truncated]")
}

func isReadTool(name string) bool {
	return name == "read_file" || name == "list_dir" || name == "search_text"
}

func isWriteTool(name string) bool {
	return name == "write_file"
}

func extractPath(content string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return ""
	}
	if path := extractDirectPath(parsed); path != "" {
		return path
	}
	if metadata, ok := parsed["metadata"].(map[string]any); ok {
		if path := extractDirectPath(metadata); path != "" {
			return path
		}
	}
	for _, key := range []string{"summary", "output"} {
		if value, ok := parsed[key].(string); ok {
			if path := extractPathFromText(value); path != "" {
				return path
			}
		}
	}
	return ""
}

func extractDirectPath(parsed map[string]any) string {
	for _, key := range []string{"path", "file", "target"} {
		if value, ok := parsed[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractPathFromText(text string) string {
	lower := strings.ToLower(text)
	for _, marker := range []string{" from ", " to "} {
		idx := strings.LastIndex(lower, marker)
		if idx < 0 {
			continue
		}
		candidate := strings.TrimSpace(text[idx+len(marker):])
		if candidate == "" {
			continue
		}
		end := len(candidate)
		for i, r := range candidate {
			if r == '\r' || r == '\n' || r == '\t' || r == ' ' {
				end = i
				break
			}
		}
		candidate = strings.Trim(candidate[:end], `"'.,;`)
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func renderFileInventory(readFiles, modifiedFiles []FileObservation) string {
	var b strings.Builder
	if len(readFiles) > 0 {
		b.WriteString("Read files:\n")
		for _, obs := range readFiles {
			b.WriteString("- ")
			b.WriteString(obs.Path)
			b.WriteString("\n")
		}
	}
	if len(modifiedFiles) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Modified files:\n")
		for _, obs := range modifiedFiles {
			b.WriteString("- ")
			b.WriteString(obs.Path)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func truncateWithMarker(value string, maxChars int, marker string) string {
	value = strings.TrimSpace(value)
	if maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	marker = "\n" + marker
	if maxChars <= len(marker) {
		return value[:maxChars]
	}
	return strings.TrimSpace(value[:maxChars-len(marker)]) + marker
}
