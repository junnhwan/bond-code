package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/mattn/go-runewidth"
)

func TestAgentEventsRenderInlineTimelineBlocks(t *testing.T) {
	model := NewModel(Config{})
	events := []agent.Event{
		{Type: agent.EventModelChunk, Message: "thinking"},
		{Type: agent.EventToolRequested, ToolName: "read_file", Message: `{"path":"README.md"}`},
		{Type: agent.EventToolConfirmationRequested, ToolName: "write_file", Risk: "medium", Message: "write_file with input", Input: `{"path":"README.md"}`},
		{Type: agent.EventToolApproved, ToolName: "write_file", Risk: "medium", Message: "approved"},
		{Type: agent.EventToolResult, ToolName: "read_file", Message: "file content"},
	}

	for _, event := range events {
		model = model.ApplyAgentEvent(event)
	}

	blocks := model.timeline.Turns[0].Blocks
	if len(blocks) != 3 {
		t.Fatalf("expected assistant plus two tool blocks, got %#v", blocks)
	}
	assertBlock(t, blocks[0], BlockAssistant, "agent", "thinking")
	assertBlock(t, blocks[1], BlockTool, "read_file", "file content")
	assertBlock(t, blocks[2], BlockTool, "write_file", "approved")
	if blocks[1].Tool == nil || blocks[1].Tool.Status != ToolDone {
		t.Fatalf("expected read_file to be a done tool block, got %#v", blocks[1])
	}
	if blocks[2].Tool == nil || blocks[2].Tool.Status != ToolRunning || blocks[2].Tool.Risk != "medium" {
		t.Fatalf("expected write_file to be a running medium-risk tool block, got %#v", blocks[2])
	}
}

func TestAgentEventsPopulateGroupedTimeline(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model = model.SetInput("run tests")
	model, _ = model.Submit(context.Background())
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "checking"})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolRequested,
		ToolName:   "run_command",
		ToolCallID: "call-test",
		Input:      `{"command":"go test ./internal/tui"}`,
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolResult,
		ToolName:   "run_command",
		ToolCallID: "call-test",
		Output:     "ok",
	})

	if len(model.timeline.Turns) != 1 {
		t.Fatalf("expected one grouped turn, got %#v", model.timeline.Turns)
	}
	turn := model.timeline.Turns[0]
	if turn.User.Body != "run tests" {
		t.Fatalf("expected user prompt in turn, got %#v", turn.User)
	}
	if len(turn.Blocks) != 2 {
		t.Fatalf("expected assistant and tool blocks, got %#v", turn.Blocks)
	}
	if turn.Blocks[1].Tool == nil || turn.Blocks[1].Tool.Status != ToolDone {
		t.Fatalf("expected done tool block, got %#v", turn.Blocks[1])
	}
}

func TestAdapterRendersSubagentLifecycleEvent(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventSubagentStarted,
		ToolCallID: "sub-1",
		ToolName:   "research",
		Message:    "inspect docs",
	})

	if len(model.timeline.Turns) == 0 {
		t.Fatal("expected subagent event in timeline")
	}
	blocks := model.timeline.Turns[0].Blocks
	if len(blocks) != 1 || blocks[0].Kind != BlockSubagent || !strings.Contains(blocks[0].Title, "research") {
		t.Fatalf("expected subagent summary block, got %#v", blocks)
	}
	if blocks[0].Summary != "running" || !strings.Contains(blocks[0].Body, "inspect docs") {
		t.Fatalf("expected running subagent body, got %#v", blocks[0])
	}
	if model.live.ActiveSubagents != 1 || model.live.LatestSubagentStatus != "running" {
		t.Fatalf("expected live subagent state, got %#v", model.live)
	}
}

func TestToolEventsMergeIntoStructuredToolBlock(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolRequested,
		ToolName:   "run_command",
		ToolCallID: "call-test",
		Risk:       "medium",
		Input:      `{"command":"go test ./..."}`,
		CreatedAt:  mustTime("2026-06-04T10:00:00Z"),
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolResult,
		ToolName:   "run_command",
		ToolCallID: "call-test",
		Output:     "ok  github.com/junnhwan/bond-code/internal/tui  0.42s\nFAIL github.com/junnhwan/bond-code/internal/agent 0.10s",
		CreatedAt:  mustTime("2026-06-04T10:00:02Z"),
	})

	blocks := model.timeline.Turns[0].Blocks
	if len(blocks) != 1 {
		t.Fatalf("expected requested/result events to merge into one block, got %#v", blocks)
	}
	tool := blocks[0].Tool
	if tool == nil {
		t.Fatalf("expected structured tool block, got %#v", blocks[0])
	}
	if tool.Status != ToolDone || tool.Name != "run_command" || tool.Risk != "medium" {
		t.Fatalf("unexpected tool metadata: %#v", tool)
	}
	if tool.Duration != 2*time.Second {
		t.Fatalf("expected duration from event timestamps, got %s", tool.Duration)
	}
	if !strings.Contains(tool.Summary, "go test") || !strings.Contains(tool.Summary, "FAIL 1") {
		t.Fatalf("expected go test scan summary, got %q", tool.Summary)
	}
	if tool.Collapsed {
		t.Fatal("expected short tool output to remain expanded")
	}
}

func TestLongToolOutputDefaultsCollapsedAndCtrlOTogglesVerboseMode(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolRequested,
		ToolName:   "run_command",
		ToolCallID: "call-diff",
		Input:      `{"command":"git diff"}`,
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolResult,
		ToolName:   "run_command",
		ToolCallID: "call-diff",
		Output:     strings.Repeat("+ changed line\n", 80),
	})

	blocks := model.timeline.Turns[0].Blocks
	if len(blocks) != 1 || blocks[0].Tool == nil {
		t.Fatalf("expected one tool block, got %#v", blocks)
	}
	if !blocks[0].Tool.Collapsed {
		t.Fatalf("expected long output to start collapsed, got %#v", blocks[0].Tool)
	}
	view := model.View()
	if strings.Count(view, "+ changed line") > 5 {
		t.Fatalf("expected collapsed view to hide most output, got:\n%s", view)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	model = updated.(Model)
	if !model.verbose {
		t.Fatal("expected Ctrl+O to enable verbose transcript mode")
	}
	if !model.timeline.Turns[0].Blocks[0].Tool.Collapsed {
		t.Fatal("Ctrl+O should preserve per-block collapsed state")
	}
}

func TestGitDiffSummaryIgnoresFileHeaderLines(t *testing.T) {
	summary := summarizeToolOutput("git diff", strings.Join([]string{
		"diff --git a/a.txt b/a.txt",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1,2 +1,2 @@",
		"-old",
		"+new",
	}, "\n"), "")

	if summary != "git diff: +1 -1" {
		t.Fatalf("expected git diff summary to ignore headers, got %q", summary)
	}
}

func TestGoTestSummaryClassifiesFailures(t *testing.T) {
	output := strings.Join([]string{
		"ok\tgithub.com/junnhwan/bond-code/internal/tui\t0.12s",
		"FAIL\tgithub.com/junnhwan/bond-code/internal/agent\t0.01s",
		"# github.com/junnhwan/bond-code/internal/cli",
		"internal/cli/chat.go:10:2: undefined: missingSymbol",
		"panic: test panic",
	}, "\n")
	summary := summarizeToolOutput("go test", output, "")

	for _, want := range []string{"go test", "ok 1", "FAIL 1", "compile error", "panic"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("expected go test summary to contain %q, got %q", want, summary)
		}
	}
}

func TestToolBlockTitlesUseKeyParameters(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "read_file", in: `{"path":"internal/tui/model.go"}`, want: "ReadFile(internal/tui/model.go)"},
		{name: "run_command", in: `{"command":"go test ./..."}`, want: "Shell(go test ./...)"},
		{name: "search_text", in: `{"pattern":"TODO","path":"internal"}`, want: `Search("TODO")`},
		{name: "mcp__github__search", in: `{"query":"bond"}`, want: "MCP github.search"},
	}

	for _, tc := range cases {
		model := NewModel(Config{})
		model = model.ApplyAgentEvent(agent.Event{Type: agent.EventToolRequested, ToolName: tc.name, Input: tc.in})
		blocks := model.timeline.Turns[0].Blocks
		if len(blocks) != 1 || blocks[0].Tool == nil {
			t.Fatalf("expected one tool block, got %#v", blocks)
		}
		if !strings.Contains(blocks[0].Title, tc.want) {
			t.Fatalf("expected title to contain %q, got %q", tc.want, blocks[0].Title)
		}
	}
}

func TestApprovalEventClearsPendingConfirmation(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolConfirmationRequested,
		ToolName:   "write_file",
		ToolCallID: "call-write",
		Risk:       "medium",
		Message:    "write_file with input",
		Input:      `{"path":"README.md"}`,
	})
	if model.agent.Pending == nil {
		t.Fatal("expected pending confirmation after request")
	}

	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolApproved,
		ToolName:   "write_file",
		ToolCallID: "call-write",
		Risk:       "medium",
		Message:    "approved",
	})

	if model.agent.Pending != nil {
		t.Fatalf("expected approval to clear pending confirmation, got %#v", model.agent.Pending)
	}
}

func TestAgentErrorPopulatesGroupedTimeline(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model = model.SetInput("hello")
	model, _ = model.Submit(context.Background())
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventAgentError, Error: "boom"})

	if len(model.timeline.Turns) != 1 {
		t.Fatalf("expected one grouped turn, got %#v", model.timeline.Turns)
	}
	blocks := model.timeline.Turns[0].Blocks
	if len(blocks) != 1 || blocks[0].Kind != BlockError || !strings.Contains(blocks[0].Body, "boom") {
		t.Fatalf("expected grouped agent error block, got %#v", blocks)
	}
}

func TestAgentErrorHumanizesCommonModelFailures(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "auth", raw: "model API returned HTTP 401: invalid api key", want: "Authentication failed"},
		{name: "rate limit", raw: "model API returned HTTP 429: rate limit exceeded", want: "Rate limited"},
		{name: "timeout", raw: "context deadline exceeded", want: "Request timed out"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
			model = model.SetInput("hello")
			model, _ = model.Submit(context.Background())
			model = model.ApplyAgentEvent(agent.Event{Type: agent.EventAgentError, Error: tc.raw})

			block := model.timeline.Turns[0].Blocks[0]
			if !strings.Contains(block.Body, tc.want) {
				t.Fatalf("expected humanized message %q, got:\n%s", tc.want, block.Body)
			}
			if !strings.Contains(block.Body, "Original: "+tc.raw) {
				t.Fatalf("expected original error to be preserved, got:\n%s", block.Body)
			}
		})
	}
}

func mustTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}

func assertBlock(t *testing.T, block Block, kind BlockKind, title string, body string) {
	t.Helper()
	if block.Kind != kind {
		t.Fatalf("expected kind %s, got %#v", kind, block)
	}
	if !strings.Contains(block.Title, title) {
		t.Fatalf("expected title containing %q, got %#v", title, block)
	}
	if !strings.Contains(block.Body, body) {
		t.Fatalf("expected body containing %q, got %#v", body, block)
	}
}

func TestTruncatePlainHonorsDisplayWidth(t *testing.T) {
	if got := truncatePlain("hello world", 8); got != "hello..." {
		t.Fatalf("ascii truncation expected hello..., got %q", got)
	}
	// Six CJK runes occupy 12 display cells; clamping to 6 cells must cut and tail.
	got := truncatePlain("你好世界你好世界", 6)
	if w := runewidth.StringWidth(got); w > 6 {
		t.Fatalf("expected truncation within 6 display cells, got %q (%d cells)", got, w)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ellipsis tail on wide-text truncation, got %q", got)
	}
}
