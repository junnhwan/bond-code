package tui

import (
	"strings"
	"testing"
)

func TestTimelineStartsUserTurn(t *testing.T) {
	state := TimelineState{}
	state = state.StartUserTurn("fix the tui")

	if len(state.Turns) != 1 {
		t.Fatalf("expected one turn, got %#v", state.Turns)
	}
	if state.Turns[0].User.Body != "fix the tui" {
		t.Fatalf("unexpected user block: %#v", state.Turns[0].User)
	}
}

func TestTimelineAppendsAssistantChunksToLatestBlock(t *testing.T) {
	state := TimelineState{}
	state = state.StartUserTurn("hello")
	state = state.AppendAssistantChunk("first")
	state = state.AppendAssistantChunk(" second")

	blocks := state.Turns[0].Blocks
	if len(blocks) != 1 {
		t.Fatalf("expected one assistant block, got %#v", blocks)
	}
	if blocks[0].Kind != BlockAssistant || blocks[0].Body != "first second" {
		t.Fatalf("unexpected assistant block: %#v", blocks[0])
	}
}

func TestTimelineUpsertsToolByID(t *testing.T) {
	state := TimelineState{}
	state = state.StartUserTurn("run tests")
	tool := &ToolBlock{ID: "call-1", Name: "run_command", Status: ToolRunning, Input: `{"command":"go test ./..."}`}
	state = state.UpsertToolBlock(tool)
	state = state.UpsertToolBlock(&ToolBlock{ID: "call-1", Name: "run_command", Status: ToolDone, Output: "ok"})

	blocks := state.Turns[0].Blocks
	if len(blocks) != 1 {
		t.Fatalf("expected merged tool block, got %#v", blocks)
	}
	if blocks[0].Tool == nil || blocks[0].Tool.Status != ToolDone || !strings.Contains(blocks[0].Tool.Output, "ok") {
		t.Fatalf("expected done tool with output, got %#v", blocks[0])
	}
}

func TestTimelineUpsertsRunningToolWithoutID(t *testing.T) {
	state := TimelineState{}
	state = state.StartUserTurn("run command")
	state = state.UpsertToolBlock(&ToolBlock{Name: "run_command", Status: ToolRunning, Input: `{"command":"pwd"}`})
	state = state.UpsertToolBlock(&ToolBlock{Name: "run_command", Status: ToolDone, Output: "D:/repo"})

	blocks := state.Turns[0].Blocks
	if len(blocks) != 1 {
		t.Fatalf("expected fallback merge for tool without ID, got %#v", blocks)
	}
	if blocks[0].Tool == nil || blocks[0].Tool.Status != ToolDone || !strings.Contains(blocks[0].Tool.Output, "D:/repo") {
		t.Fatalf("expected merged done tool, got %#v", blocks[0])
	}
}

func TestTimelineDoesNotMergeBlockedToolWithoutID(t *testing.T) {
	state := TimelineState{}
	state = state.StartUserTurn("run command")
	state = state.UpsertToolBlock(&ToolBlock{Name: "run_command", Status: ToolBlocked, Input: `{"command":"rm -rf tmp"}`})
	state = state.UpsertToolBlock(&ToolBlock{Name: "run_command", Status: ToolRunning, Input: `{"command":"pwd"}`})

	blocks := state.Turns[0].Blocks
	if len(blocks) != 2 {
		t.Fatalf("expected blocked tool to remain terminal, got %#v", blocks)
	}
	if blocks[0].Tool == nil || blocks[0].Tool.Status != ToolBlocked {
		t.Fatalf("expected first tool to stay blocked, got %#v", blocks[0])
	}
	if blocks[1].Tool == nil || blocks[1].Tool.Status != ToolRunning {
		t.Fatalf("expected second tool to be a new running block, got %#v", blocks[1])
	}
}

func TestTimelineAssignsUniqueIDsForToolBlocksWithoutCallIDs(t *testing.T) {
	state := TimelineState{}
	state = state.StartUserTurn("run command")
	state = state.UpsertToolBlock(&ToolBlock{Name: "run_command", Status: ToolBlocked, Input: `{"command":"rm -rf tmp"}`})
	state = state.UpsertToolBlock(&ToolBlock{Name: "run_command", Status: ToolRunning, Input: `{"command":"pwd"}`})

	blocks := state.Turns[0].Blocks
	if blocks[0].ID == "" || blocks[1].ID == "" {
		t.Fatalf("expected non-empty block IDs, got %#v", blocks)
	}
	if blocks[0].ID == blocks[1].ID {
		t.Fatalf("expected unique block IDs for no-call-ID tools, got %#v", blocks)
	}
}

func TestTimelineAppendAssistantChunkDoesNotMutatePreviousState(t *testing.T) {
	previous := TimelineState{}
	previous = previous.StartUserTurn("hello")
	previous = previous.AppendAssistantChunk("first")

	next := previous.AppendAssistantChunk(" second")

	if got := previous.Turns[0].Blocks[0].Body; got != "first" {
		t.Fatalf("expected previous state to keep first chunk, got %q", got)
	}
	if got := next.Turns[0].Blocks[0].Body; got != "first second" {
		t.Fatalf("expected next state to append chunk, got %q", got)
	}
}

func TestTimelineClonesToolBlockOnInsert(t *testing.T) {
	tool := &ToolBlock{Name: "run_command", Status: ToolRunning, Input: `{"command":"pwd"}`}
	state := TimelineState{}
	state = state.StartUserTurn("run command")
	state = state.UpsertToolBlock(tool)

	tool.Status = ToolDone
	tool.Output = "mutated"

	stored := state.Turns[0].Blocks[0].Tool
	if stored == nil {
		t.Fatalf("expected stored tool block, got %#v", state.Turns[0].Blocks[0])
	}
	if stored.Status != ToolRunning || stored.Output != "" {
		t.Fatalf("expected timeline to keep cloned running tool, got %#v", stored)
	}
}

func TestSeedTimelineRebuildsTurns(t *testing.T) {
	seed := []SeedMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "user", Content: "do a thing"},
		{Role: "tool", ToolName: "read_file", Content: `{"path":"x.go"}`},
		{Role: "assistant", Content: "done"},
	}
	timeline := SeedTimeline(seed)
	if len(timeline.Turns) != 2 {
		t.Fatalf("expected 2 turns (one per user message), got %d", len(timeline.Turns))
	}
	if timeline.Turns[0].User.Body != "hello" {
		t.Fatalf("first turn user body mismatch: %q", timeline.Turns[0].User.Body)
	}
	// Second turn has a tool block then a final assistant block.
	blocks := timeline.Turns[1].Blocks
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks in second turn, got %d", len(blocks))
	}
	if blocks[0].Kind != BlockTool || blocks[0].Title != "tool read_file" {
		t.Fatalf("expected first block tool read_file, got %#v", blocks[0])
	}
	if blocks[1].Kind != BlockAssistant || blocks[1].Body != "done" {
		t.Fatalf("expected final assistant block, got %#v", blocks[1])
	}
}
