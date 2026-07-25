package contextx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGovernor_DropOrphanToolResults(t *testing.T) {
	gov := NewGovernor(GovernorConfig{AutoCompact: true})

	tests := []struct {
		name     string
		input    []llm.Message
		expected int
	}{
		{
			name: "has orphan tool message",
			input: []llm.Message{
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "tc1", Name: "read_file"}}},
				{Role: llm.RoleTool, ToolCallID: "tc1", Content: "file content"},
				{Role: llm.RoleTool, ToolCallID: "tc2", Content: "orphan result"},
			},
			expected: 2,
		},
		{
			name: "no orphan messages",
			input: []llm.Message{
				{Role: llm.RoleUser, Content: "hello"},
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gov.dropOrphanToolResults(tt.input)
			assert.Equal(t, tt.expected, len(result))
		})
	}
}

func TestGovernor_BackfillMissingToolResults(t *testing.T) {
	gov := NewGovernor(GovernorConfig{AutoCompact: true})

	input := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "tc1", Name: "read_file"},
			{ID: "tc2", Name: "list_dir"},
		}},
		{Role: llm.RoleTool, ToolCallID: "tc1", Content: "file content"},
	}

	result := gov.backfillMissingToolResults(input)

	require.Equal(t, 3, len(result))
	foundBackfill := false
	for _, msg := range result {
		if msg.Role == llm.RoleTool && msg.ToolCallID == "tc2" {
			foundBackfill = true
			assert.Contains(t, msg.Content, "missing")
		}
	}
	assert.True(t, foundBackfill, "should have backfilled tc2")
}

func TestGovernor_MicroCompact(t *testing.T) {
	gov := NewGovernor(GovernorConfig{
		AutoCompact:            true,
		MicroCompactKeepRecent: 2,
		MicroCompactMinChars:   10,
	})

	input := []llm.Message{
		{Role: llm.RoleTool, ToolName: "read_file", Content: strings.Repeat("a", 100)},
		{Role: llm.RoleTool, ToolName: "read_file", Content: strings.Repeat("b", 100)},
		{Role: llm.RoleTool, ToolName: "read_file", Content: strings.Repeat("c", 100)},
		{Role: llm.RoleTool, ToolName: "read_file", Content: strings.Repeat("d", 100)},
	}

	result := gov.microCompact(input)

	require.Equal(t, 4, len(result))
	assert.Equal(t, toolResultClearedMessage, result[0].Content)
	assert.Equal(t, toolResultClearedMessage, result[1].Content)
	assert.Equal(t, strings.Repeat("c", 100), result[2].Content)
	assert.Equal(t, strings.Repeat("d", 100), result[3].Content)
}

func TestGovernor_ApplyToolResultBudget(t *testing.T) {
	gov := NewGovernor(GovernorConfig{
		AutoCompact:      true,
		ToolResultBudget: 100,
	})

	input := []llm.Message{
		{Role: llm.RoleTool, ToolName: "read_file", Content: strings.Repeat("x", 200)},
		{Role: llm.RoleTool, ToolName: "list_dir", Content: "small"},
	}

	result := gov.applyToolResultBudget(input)

	require.Equal(t, 2, len(result))
	assert.Contains(t, result[0].Content, "truncated")
	assert.Equal(t, "small", result[1].Content)
}

func TestGovernor_SpillsOversizedToolResultToStore(t *testing.T) {
	dataDir := t.TempDir()
	gov := NewGovernor(GovernorConfig{
		AutoCompact:            true,
		ToolResultBudget:       40,
		ToolResultPreviewChars: 12,
		ToolResultStore:        NewToolResultStore(dataDir, "session-test"),
	})
	input := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "tc1", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "tc1", ToolName: "read_file", Content: strings.Repeat("x", 120)},
	}

	result := gov.applyToolResultBudget(input)

	require.Equal(t, 2, len(result))
	assert.Contains(t, result[1].Content, "<persisted-output>")
	assert.Contains(t, result[1].Content, "Full output saved to:")
	assert.Contains(t, filepath.ToSlash(result[1].Content), "tool-results/session-test/tc1.txt")
	assert.Contains(t, result[1].Content, "Preview:\nxxxxxxxxxxxx")

	stored, err := os.ReadFile(filepath.Join(dataDir, "tool-results", "session-test", "tc1.txt"))
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("x", 120), string(stored))
}

func TestGovernor_ToolResultTurnBudgetSpillsLargestResults(t *testing.T) {
	dataDir := t.TempDir()
	gov := NewGovernor(GovernorConfig{
		AutoCompact:            true,
		ToolResultBudget:       1000,
		ToolResultPreviewChars: 10,
		ToolResultTurnBudget:   80,
		ToolResultStore:        NewToolResultStore(dataDir, "session-test"),
	})
	input := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "tc1", Name: "read_file"},
			{ID: "tc2", Name: "search_text"},
		}},
		{Role: llm.RoleTool, ToolCallID: "tc1", ToolName: "read_file", Content: strings.Repeat("a", 70)},
		{Role: llm.RoleTool, ToolCallID: "tc2", ToolName: "search_text", Content: strings.Repeat("b", 70)},
	}

	result := gov.applyToolResultBudget(input)

	require.Equal(t, 3, len(result))
	spilled := 0
	for _, msg := range result {
		if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "Full output saved to:") {
			spilled++
		}
	}
	assert.GreaterOrEqual(t, spilled, 1)
	assert.FileExists(t, filepath.Join(dataDir, "tool-results", "session-test", "tc1.txt"))
}

func TestGovernor_ToolResultTurnBudgetOnlyCountsLatestToolTurn(t *testing.T) {
	dataDir := t.TempDir()
	gov := NewGovernor(GovernorConfig{
		AutoCompact:            true,
		ToolResultBudget:       1000,
		ToolResultPreviewChars: 10,
		ToolResultTurnBudget:   80,
		ToolResultStore:        NewToolResultStore(dataDir, "session-test"),
	})
	input := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "old", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "old", ToolName: "read_file", Content: strings.Repeat("a", 70)},
		{Role: llm.RoleAssistant, Content: "processed the old result"},
		{Role: llm.RoleUser, Content: "continue"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "latest", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "latest", ToolName: "read_file", Content: strings.Repeat("b", 70)},
	}

	result := gov.applyToolResultBudget(input)

	assert.Equal(t, strings.Repeat("a", 70), result[1].Content)
	assert.Equal(t, strings.Repeat("b", 70), result[5].Content)
	assert.NoFileExists(t, filepath.Join(dataDir, "tool-results", "session-test", "old.txt"))
	assert.NoFileExists(t, filepath.Join(dataDir, "tool-results", "session-test", "latest.txt"))
}

func TestManagerGovernDetailedNoLongerSnips(t *testing.T) {
	manager := NewManager(NewGovernor(GovernorConfig{AutoCompact: true, MaxTokens: 80}))
	input := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: strings.Repeat("a", 300)},
		{Role: llm.RoleAssistant, Content: strings.Repeat("b", 300)},
		{Role: llm.RoleUser, Content: "now"},
	}
	result := manager.GovernDetailed(input, 80)
	assert.Equal(t, 0, result.SnippedMessages)
	assert.Equal(t, len(input), len(result.Messages))
	assert.Equal(t, "sys", result.Messages[0].Content)
}

func TestPrepareAndApplyCompaction(t *testing.T) {
	var msgs []Message
	msgs = append(msgs, Message{Role: llm.RoleSystem, Content: "sys"})
	for i := 0; i < 20; i++ {
		msgs = append(msgs, Message{Role: llm.RoleUser, Content: strings.Repeat("u", 200) + string(rune('a'+i%26))})
		msgs = append(msgs, Message{Role: llm.RoleAssistant, Content: strings.Repeat("a", 200)})
	}
	cfg := DefaultGovernorConfig()
	cfg.KeepRecentTokens = 500 // force early cut
	plan, err := PrepareCompaction(msgs, cfg, "")
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.NotEmpty(t, plan.MessagesToSummarize)
	require.NotEmpty(t, plan.Kept)

	applied := ApplyCompaction(plan, "## Goal\nShip context rewrite")
	require.True(t, len(applied.Messages) < len(msgs))
	require.Equal(t, llm.RoleSystem, applied.Messages[0].Role)
	require.Contains(t, applied.Messages[1].Content, "compacted into the following summary")
	require.Contains(t, applied.Messages[1].Content, "Ship context rewrite")
	require.Less(t, applied.AfterTokens, applied.BeforeTokens)
}

func TestShouldCompactThreshold(t *testing.T) {
	cfg := DefaultGovernorConfig()
	cfg.MaxTokens = 10000
	cfg.ReserveTokens = 2000
	assert.False(t, ShouldCompact(7000, cfg))
	assert.True(t, ShouldCompact(8500, cfg))
	cfg.AutoCompact = false
	assert.False(t, ShouldCompact(9000, cfg))
}

func TestFindCutPointNeverCutsAtToolResult(t *testing.T) {
	body := []Message{
		{Role: llm.RoleUser, Content: "q1"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "1", ToolName: "read_file", Content: strings.Repeat("x", 4000)},
		{Role: llm.RoleAssistant, Content: "done"},
		{Role: llm.RoleUser, Content: "q2"},
	}
	cut := findCutPoint(body, 50, NewEstimator())
	if cut.FirstKept < len(body) {
		assert.NotEqual(t, llm.RoleTool, body[cut.FirstKept].Role)
	}
}

func TestEmergencyShrinkReducesHistory(t *testing.T) {
	var msgs []Message
	for i := 0; i < 30; i++ {
		msgs = append(msgs, Message{Role: llm.RoleUser, Content: strings.Repeat("turn ", 50)})
		msgs = append(msgs, Message{Role: llm.RoleAssistant, Content: strings.Repeat("ans ", 50)})
	}
	cfg := DefaultGovernorConfig()
	cfg.KeepRecentTokens = 200
	result := EmergencyShrink(msgs, cfg)
	require.NotEmpty(t, result.Messages)
	require.Less(t, len(result.Messages), len(msgs))
	require.Less(t, result.AfterTokens, result.BeforeTokens)
}
