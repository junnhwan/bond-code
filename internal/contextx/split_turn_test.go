package contextx

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
)

func TestFindTurnStartIndex(t *testing.T) {
	body := []Message{
		{Role: llm.RoleUser, Content: "q1"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "1", ToolName: "read_file"},
		{Role: llm.RoleAssistant, Content: "answer1"},
		{Role: llm.RoleUser, Content: "q2"},
	}
	if got := findTurnStartIndex(body, 3); got != 0 {
		t.Fatalf("turn start for index 3 = %d, want 0", got)
	}
	if got := findTurnStartIndex(body, 4); got != 4 {
		t.Fatalf("turn start for index 4 = %d, want 4", got)
	}
}

func TestFindCutPointPrefersUserBoundary(t *testing.T) {
	body := []Message{
		{Role: llm.RoleUser, Content: "old " + string(make([]byte, 800))},
		{Role: llm.RoleAssistant, Content: "old ans " + string(make([]byte, 800))},
		{Role: llm.RoleUser, Content: "recent"},
		{Role: llm.RoleAssistant, Content: "recent ans"},
	}
	cut := findCutPoint(body, 100, NewEstimator())
	if cut.FirstKept < 0 || cut.FirstKept >= len(body) {
		t.Fatalf("invalid cut %d", cut.FirstKept)
	}
	if body[cut.FirstKept].Role == llm.RoleTool {
		t.Fatal("cut must not land on tool result")
	}
}
