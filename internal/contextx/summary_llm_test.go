package contextx

import (
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
)

// LLMSummary 非空时优先用作语义摘要（L4），替代规则截断。
func TestBuildSummaryArtifactPrefersLLMSummary(t *testing.T) {
	messages := []Message{
		{Role: llm.RoleUser, Content: "long conversation that should be summarized semantically"},
	}
	artifact := BuildSummaryArtifact(messages, SummaryConfig{
		LLMSummary: "LLM semantic summary",
	})
	if !strings.Contains(artifact.Summary, "LLM semantic summary") {
		t.Fatalf("expected LLM summary to be used, got %q", artifact.Summary)
	}
}

// LLMSummary 为空时降级到 renderDeterministicSummary（规则截断）。
func TestBuildSummaryArtifactFallsBackWhenLLMEmpty(t *testing.T) {
	messages := []Message{
		{Role: llm.RoleUser, Content: "conversation content here"},
	}
	artifact := BuildSummaryArtifact(messages, SummaryConfig{})
	if !strings.Contains(artifact.Summary, "conversation content here") {
		t.Fatalf("expected deterministic fallback, got %q", artifact.Summary)
	}
}
