package contextx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/tool"
)

func TestBuildSummaryArtifactKeepsRecentTailAndInventory(t *testing.T) {
	messages := []Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "read README and update docs"},
		{Role: llm.RoleAssistant, Content: "I will inspect files."},
		{Role: llm.RoleTool, ToolName: "read_file", Content: `{"path":"README.md","output":"BondCode"}`},
		{Role: llm.RoleTool, ToolName: "write_file", Content: `{"path":"docs/planning/evidence-map.md","status":"ok"}`},
		{Role: llm.RoleAssistant, Content: "updated evidence map"},
	}

	artifact := BuildSummaryArtifact(messages, SummaryConfig{
		PreviousSummary: "Earlier: baseline verified.",
		RecentMessages:  2,
		MaxSummaryChars: 400,
	})

	if artifact.Version != 2 {
		t.Fatalf("expected version 2, got %d", artifact.Version)
	}
	if !strings.Contains(artifact.Summary, "Earlier: baseline verified.") {
		t.Fatalf("expected previous summary in artifact summary, got %q", artifact.Summary)
	}
	if len(artifact.RecentMessages) != 2 {
		t.Fatalf("expected two recent messages, got %d", len(artifact.RecentMessages))
	}
	if len(artifact.ReadFiles) != 1 || artifact.ReadFiles[0].Path != "README.md" {
		t.Fatalf("expected README read inventory, got %#v", artifact.ReadFiles)
	}
	if len(artifact.ModifiedFiles) != 1 || artifact.ModifiedFiles[0].Path != "docs/planning/evidence-map.md" {
		t.Fatalf("expected evidence map modified inventory, got %#v", artifact.ModifiedFiles)
	}
}

func TestSummaryArtifactPromptSectionIsBounded(t *testing.T) {
	artifact := SummaryArtifact{
		Version:   1,
		Summary:   strings.Repeat("a", 1000),
		ReadFiles: []FileObservation{{Path: "README.md", ToolName: "read_file"}},
	}

	section := artifact.PromptSection(120)

	if len(section) > 180 {
		t.Fatalf("expected bounded prompt section, got length %d", len(section))
	}
	if !strings.Contains(section, "README.md") {
		t.Fatalf("expected file inventory in prompt section, got %q", section)
	}
}

func TestBuildSummaryArtifactExtractsInventoryFromToolResultEnvelope(t *testing.T) {
	readResult := tool.Success("read_file", "read 12 bytes from README.md", "BondCode")
	writeResult := tool.Success("write_file", "file written", "wrote 8 bytes to docs/planning/evidence-map.md")
	readJSON, err := json.Marshal(readResult)
	if err != nil {
		t.Fatalf("marshal read result: %v", err)
	}
	writeJSON, err := json.Marshal(writeResult)
	if err != nil {
		t.Fatalf("marshal write result: %v", err)
	}
	messages := []Message{
		{Role: llm.RoleTool, ToolName: "read_file", Content: string(readJSON)},
		{Role: llm.RoleTool, ToolName: "write_file", Content: string(writeJSON)},
	}

	artifact := BuildSummaryArtifact(messages, SummaryConfig{RecentMessages: 1})

	if len(artifact.ReadFiles) != 1 || artifact.ReadFiles[0].Path != "README.md" {
		t.Fatalf("expected README.md from tool.Result envelope, got %#v", artifact.ReadFiles)
	}
	if len(artifact.ModifiedFiles) != 1 || artifact.ModifiedFiles[0].Path != "docs/planning/evidence-map.md" {
		t.Fatalf("expected evidence map from tool.Result envelope, got %#v", artifact.ModifiedFiles)
	}
}
