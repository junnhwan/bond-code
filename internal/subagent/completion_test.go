package subagent

import (
	"strings"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/llm"
)

func TestCountToolResults(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "go"},
		{Role: llm.RoleAssistant, Content: "calling", ToolCalls: []llm.ToolCall{{ID: "1", Name: "list_dir"}}},
		{Role: llm.RoleTool, Content: "[]", ToolCallID: "1", ToolName: "list_dir"},
		{Role: llm.RoleAssistant, Content: "done"},
	}
	if got := countToolResults(msgs); got != 1 {
		t.Fatalf("countToolResults = %d, want 1", got)
	}
}

func TestIsEmptyCompletion(t *testing.T) {
	r := &SubagentResult{Status: "completed", ToolCount: 0}
	if !r.IsEmptyCompletion() {
		t.Fatal("expected empty completion")
	}
	r.ToolCount = 2
	if r.IsEmptyCompletion() {
		t.Fatal("tools > 0 should not be empty completion")
	}
	r.Status = "failed"
	r.ToolCount = 0
	if r.IsEmptyCompletion() {
		t.Fatal("failed is not empty completion")
	}
}

func TestFormatTaskResultEmptyCompletion(t *testing.T) {
	out := formatTaskResult(&SubagentResult{
		TaskID:      "t1",
		AgentType:   AgentTypeCoder,
		Status:      "completed",
		Iterations:  1,
		ToolCount:   0,
		FinalAnswer: "I would create the files next.",
	})
	if !strings.Contains(out, `tools="0"`) {
		t.Fatalf("missing tools attr: %s", out)
	}
	if !strings.Contains(out, `empty="true"`) {
		t.Fatalf("missing empty attr: %s", out)
	}
	if !strings.Contains(out, "empty completion") {
		t.Fatalf("missing empty notice: %s", out)
	}
	if !strings.Contains(out, "I would create the files next.") {
		t.Fatalf("missing original answer: %s", out)
	}
}

func TestFormatTaskResultRateLimit(t *testing.T) {
	out := formatTaskResult(&SubagentResult{
		TaskID:     "t2",
		AgentType:  AgentTypeResearch,
		Status:     "failed",
		Iterations: 1,
		ToolCount:  0,
		Error:      "model API failed after 3 attempts: model API returned HTTP 429: 1分钟内最多请求15次",
	})
	if !strings.Contains(out, "Rate limited by the model provider") {
		t.Fatalf("missing rate-limit notice: %s", out)
	}
	if !strings.Contains(out, "429") {
		t.Fatalf("should keep original error: %s", out)
	}
}

func TestFormatTaskResultWithTools(t *testing.T) {
	start := time.Now().Add(-2 * time.Second)
	end := time.Now()
	out := formatTaskResult(&SubagentResult{
		TaskID:      "t3",
		AgentType:   AgentTypeCoder,
		Status:      "completed",
		Iterations:  3,
		ToolCount:   4,
		FinalAnswer: "wrote files",
		StartTime:   start,
		EndTime:     end,
	})
	if !strings.Contains(out, `tools="4"`) {
		t.Fatalf("tools attr: %s", out)
	}
	if strings.Contains(out, `empty="true"`) {
		t.Fatalf("must not mark non-empty: %s", out)
	}
	if !strings.Contains(out, `duration_ms=`) {
		t.Fatalf("duration attr: %s", out)
	}
	if strings.Contains(out, "empty completion") {
		t.Fatalf("must not inject empty notice: %s", out)
	}
}

func TestAnnotateResultMetadata(t *testing.T) {
	r := &SubagentResult{
		Status:    "completed",
		Messages:  []llm.Message{{Role: llm.RoleTool, Content: "ok"}},
		StartTime: time.Now().Add(-time.Second),
		EndTime:   time.Now(),
	}
	annotateResultMetadata(r)
	if r.ToolCount != 1 {
		t.Fatalf("ToolCount = %d", r.ToolCount)
	}
	if r.Metadata["tool_count"] != 1 {
		t.Fatalf("metadata tool_count = %#v", r.Metadata["tool_count"])
	}
	if r.Metadata["empty_completion"] != nil {
		t.Fatalf("should not set empty_completion when tools ran")
	}
}

func TestIsRateLimitText(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"HTTP 429: rate limit", true},
		{"1分钟内最多请求15次", true},
		{"too many requests", true},
		{"connection refused", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isRateLimitText(tc.in); got != tc.want {
			t.Errorf("isRateLimitText(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}
