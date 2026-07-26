package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
)

// lastAssistantBlock returns the last assistant block in the active turn, or a
// zero Block when there is none.
func lastAssistantBlock(m Model) Block {
	if len(m.timeline.Turns) == 0 {
		return Block{}
	}
	turn := m.timeline.Turns[len(m.timeline.Turns)-1]
	for i := len(turn.Blocks) - 1; i >= 0; i-- {
		if turn.Blocks[i].Kind == BlockAssistant {
			return turn.Blocks[i]
		}
	}
	return Block{}
}

func assistantBody(m Model) string {
	return lastAssistantBlock(m).Body
}

func reasoningBody(m Model) string {
	if len(m.timeline.Turns) == 0 {
		return ""
	}
	turn := m.timeline.Turns[len(m.timeline.Turns)-1]
	for i := len(turn.Blocks) - 1; i >= 0; i-- {
		if turn.Blocks[i].Kind == BlockReasoning {
			return turn.Blocks[i].Body
		}
	}
	return ""
}

// TestFirstStreamChunksApplyImmediately guards interaction latency. Bubble Tea
// already coalesces terminal writes at 60 FPS, so the event layer must expose
// the newest assistant/reasoning state without adding another timer delay.
func TestFirstStreamChunksApplyImmediately(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true

	next, _ := model.Update(agentEventMsg{event: agent.Event{Type: agent.EventModelChunk, Message: "hello"}})
	model = next.(Model)
	if model.agent.LiveStream == nil || model.agent.LiveStream.kind != BlockAssistant || model.agent.LiveStream.body != "hello" {
		t.Fatalf("first assistant chunk must enter live state immediately, got %#v", model.agent.LiveStream)
	}

	next, _ = model.Update(agentEventMsg{event: agent.Event{Type: agent.EventReasoningChunk, Message: "thinking"}})
	model = next.(Model)
	if model.agent.LiveStream == nil || model.agent.LiveStream.kind != BlockReasoning || model.agent.LiveStream.body != "thinking" {
		t.Fatalf("first reasoning chunk must enter live state immediately, got %#v", model.agent.LiveStream)
	}
}

func applyChunkEvent(t *testing.T, model Model, chunk string) Model {
	t.Helper()
	next, _ := model.Update(agentEventMsg{event: agent.Event{Type: agent.EventModelChunk, Message: chunk}})
	return next.(Model)
}

func TestStreamChunksApplyImmediatelyInOrder(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	for _, chunk := range []string{"hel", "lo"} {
		model = applyChunkEvent(t, model, chunk)
	}
	if model.agent.LiveStream == nil || model.agent.LiveStream.body != "hello" {
		t.Fatalf("assistant chunks must apply immediately and in order, got %#v", model.agent.LiveStream)
	}
}

func TestAgentDonePreservesImmediatelyAppliedTail(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model = applyChunkEvent(t, model, "tail")

	next, _ := model.Update(agentDoneMsg{})
	model = next.(Model)
	if got := assistantBody(model); got != "tail" {
		t.Fatalf("agent completion changed the already-visible stream tail, got %q", got)
	}
}

func TestAgentDoneReleasesLiveAssistantBuffer(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model = applyChunkEvent(t, model, "complete answer")
	if model.agent.LiveStream == nil || model.agent.LiveStream.kind != BlockAssistant || model.agent.LiveStream.buffer == nil {
		t.Fatalf("expected an active assistant live buffer before completion, got %#v", model.agent.LiveStream)
	}
	if got := assistantBody(model); got != "" {
		t.Fatalf("assistant delta mutated committed history before completion: %q", got)
	}

	next, _ := model.Update(agentDoneMsg{})
	model = next.(Model)
	if model.agent.LiveStream != nil {
		t.Fatalf("agent completion must release live stream state, got %#v", model.agent.LiveStream)
	}
	if got := assistantBody(model); got != "complete answer" {
		t.Fatalf("releasing the live buffer changed the final transcript: %q", got)
	}
}

func TestCancelPreservesAlreadyRenderedStreamText(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model.agent.Cancel = func() {}
	model = applyChunkEvent(t, model, "tail")

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if got := assistantBody(model); got != "tail" {
		t.Fatalf("cancel must preserve text that was already shown, got %q", got)
	}
	if got := model.latestRunState(); got != "cancelled" {
		t.Fatalf("cancelled run state = %q, want cancelled", got)
	}
}

func TestCanceledAgentDonePreservesAlreadyRenderedTail(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model = applyChunkEvent(t, model, "tail")

	next, _ := model.Update(agentDoneMsg{err: context.Canceled})
	model = next.(Model)
	if got := assistantBody(model); got != "tail" {
		t.Fatalf("canceled completion must preserve text that was already shown, got %q", got)
	}
}

// TestLiveAssistantCompleteLinesRenderRaw guards the raw live plane: only complete
// lines are visible while the unfinished tail remains buffered until commit.
func TestLiveAssistantCompleteLinesRenderRaw(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	completeLines := strings.Repeat("word ", 2500) + "\n"
	chunk := completeLines + "unfinished"
	beforeVersion := model.timeline.Version
	model = applyChunkEvent(t, model, chunk)

	live := model.agent.LiveStream
	if live == nil || live.kind != BlockAssistant || live.body != chunk {
		t.Fatalf("long assistant chunk was not retained in live state: %#v", live)
	}
	if got := live.visibleBody(); got != completeLines {
		t.Fatalf("raw live output = %q, want only complete lines ending at byte %d", got, len(completeLines))
	}
	if model.timeline.Version != beforeVersion || assistantBody(model) != "" {
		t.Fatalf("long live chunk mutated committed history: timeline=%#v", model.timeline)
	}

	next, _ := model.Update(agentDoneMsg{})
	model = next.(Model)
	if got := assistantBody(model); got != chunk {
		t.Fatalf("completion did not commit the hidden tail exactly once: %q", got)
	}
}

// Every event applies before the next sole channel reader is armed. The
// returned command must remain the direct reader, not a timer batch that can
// delay output or create competing event paths.
func TestStreamChunkArmsOnlyDirectAgentReader(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	stream := make(chan tea.Msg)
	model.agent.Stream = stream

	next, cmd := model.Update(agentEventMsg{event: agent.Event{Type: agent.EventModelChunk, Message: "hello"}})
	model = next.(Model)
	if model.agent.LiveStream == nil || model.agent.LiveStream.body != "hello" {
		t.Fatalf("chunk was not applied before arming the next reader: %#v", model.agent.LiveStream)
	}
	if cmd == nil {
		t.Fatal("chunk must keep the sole agent reader armed")
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		t.Fatalf("chunk command returned timer/batch work instead of waiting directly for the stream: %T", msg)
	case <-time.After(40 * time.Millisecond):
	}
	stream <- agentTickMsg{}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("agent reader did not release after receiving a message")
	}
}

func TestReasoningChunksApplyImmediatelyInOrder(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	for _, chunk := range []string{"think", "ing"} {
		next, _ := model.Update(agentEventMsg{event: agent.Event{Type: agent.EventReasoningChunk, Message: chunk}})
		model = next.(Model)
	}
	if model.agent.LiveStream == nil || model.agent.LiveStream.kind != BlockReasoning || model.agent.LiveStream.body != "thinking" {
		t.Fatalf("reasoning chunks must apply immediately and in order, got %#v", model.agent.LiveStream)
	}
}

func TestToolEventAppearsAfterPrecedingReasoningWithoutFlushDelay(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	next, _ := model.Update(agentEventMsg{event: agent.Event{Type: agent.EventReasoningChunk, Message: "thinking"}})
	model = next.(Model)
	next, _ = model.Update(agentEventMsg{event: agent.Event{Type: agent.EventToolRequested, ToolName: "read_file", ToolCallID: "call-1"}})
	model = next.(Model)

	blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
	if len(blocks) != 2 || blocks[0].Kind != BlockReasoning || blocks[0].Body != "thinking" || blocks[1].Kind != BlockTool {
		t.Fatalf("tool event must appear immediately after preceding reasoning, got %#v", blocks)
	}
}

func TestStreamedAssistantReusesAmortizedBodyBuffer(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	beforeVersion := model.timeline.Version
	model = applyChunkEvent(t, model, strings.Repeat("a", 4096))
	model = applyChunkEvent(t, model, strings.Repeat("b", 4096))
	if model.agent.LiveStream == nil || model.agent.LiveStream.kind != BlockAssistant {
		t.Fatalf("expected assistant live state, got %#v", model.agent.LiveStream)
	}
	before := model.agent.LiveStream.body

	model = applyChunkEvent(t, model, "tail")
	after := model.agent.LiveStream.body
	if after != before+"tail" {
		t.Fatalf("streamed body mismatch: got length %d, want %d", len(after), len(before)+len("tail"))
	}
	if unsafe.StringData(after) != unsafe.StringData(before) {
		t.Fatal("small streamed appends must reuse the warmed live body buffer instead of copying the full response")
	}
	if model.timeline.Version != beforeVersion || assistantBody(model) != "" {
		t.Fatal("assistant deltas must not mutate committed timeline while reusing the live buffer")
	}
}

func TestLiveAssistantBufferDoesNotCrossToolBoundary(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model = applyChunkEvent(t, model, "before tool")
	if model.agent.LiveStream == nil {
		t.Fatal("expected assistant live state before tool boundary")
	}
	firstBuffer := model.agent.LiveStream.buffer
	firstGeneration := model.agent.LiveStream.generation

	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventToolRequested, ToolName: "read_file", ToolCallID: "call-1"})
	if model.agent.LiveStream != nil {
		t.Fatalf("tool activity must commit and clear the preceding live stream, got %#v", model.agent.LiveStream)
	}
	blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
	if len(blocks) != 2 || blocks[0].Kind != BlockAssistant || blocks[0].Body != "before tool" || blocks[1].Kind != BlockTool {
		t.Fatalf("unexpected committed assistant/tool ordering: %#v", blocks)
	}
	timelineAfterBoundary := model.timeline

	model = applyChunkEvent(t, model, "after tool")
	live := model.agent.LiveStream
	if live == nil || live.kind != BlockAssistant || live.body != "after tool" {
		t.Fatalf("assistant text after tool did not start a new live stream: %#v", live)
	}
	if live.buffer == firstBuffer || live.generation <= firstGeneration {
		t.Fatal("assistant text after a tool must use a new live buffer generation")
	}
	blocks = model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
	if len(blocks) != 2 || !reflect.DeepEqual(model.timeline, timelineAfterBoundary) {
		t.Fatalf("post-tool delta mutated committed history: blocks=%#v", blocks)
	}
}

func TestLiveAssistantBufferGuardsBranchedModels(t *testing.T) {
	base := NewModel(Config{})
	base.agent.Busy = true
	base = applyChunkEvent(t, base, strings.Repeat("a", 4096))
	base = applyChunkEvent(t, base, strings.Repeat("b", 4096))
	if base.agent.LiveStream == nil {
		t.Fatal("expected assistant live state before branching")
	}
	baseBody := base.agent.LiveStream.body
	beforeVersion := base.timeline.Version

	left := base.applyAssistantChunk(" left", time.Now())
	right := base.applyAssistantChunk(" right", time.Now())
	if got := base.agent.LiveStream.body; got != baseBody {
		t.Fatalf("branching mutated the source live body: got length %d, want %d", len(got), len(baseBody))
	}
	if left.agent.LiveStream == nil || left.agent.LiveStream.body != baseBody+" left" {
		t.Fatalf("left live branch leaked: %#v", left.agent.LiveStream)
	}
	if right.agent.LiveStream == nil || right.agent.LiveStream.body != baseBody+" right" {
		t.Fatalf("right live branch leaked: %#v", right.agent.LiveStream)
	}
	if base.timeline.Version != beforeVersion || left.timeline.Version != beforeVersion || right.timeline.Version != beforeVersion {
		t.Fatal("branched live appends mutated committed timeline state")
	}
}

func TestStreamedAssistantCommitsCanonicalBodyOnce(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	beforeVersion := model.timeline.Version
	model = applyChunkEvent(t, model, "first")
	model = applyChunkEvent(t, model, " second")

	if model.agent.LiveStream == nil || model.agent.LiveStream.body != "first second" {
		t.Fatalf("unexpected assistant live body: %#v", model.agent.LiveStream)
	}
	liveBody := model.agent.LiveStream.body
	if model.timeline.Version != beforeVersion || assistantBody(model) != "" {
		t.Fatalf("assistant deltas mutated committed timeline: %#v", model.timeline)
	}

	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventAgentFinished})
	body := assistantBody(model)
	if body != liveBody {
		t.Fatalf("boundary committed body = %q, want %q", body, liveBody)
	}
	blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
	if len(blocks) != 1 || blocks[0].Kind != BlockAssistant || blocks[0].Body != body {
		t.Fatalf("expected one canonical committed assistant block, got %#v", blocks)
	}
	if unsafe.StringData(body) != unsafe.StringData(liveBody) {
		t.Fatal("boundary commit must transfer the canonical live body into the timeline without copying")
	}
}

func TestStreamedReasoningReusesAmortizedBodyBuffer(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	beforeVersion := model.timeline.Version
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: strings.Repeat("a", 4096)})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: strings.Repeat("b", 4096)})
	if model.agent.LiveStream == nil || model.agent.LiveStream.kind != BlockReasoning {
		t.Fatalf("expected reasoning live state, got %#v", model.agent.LiveStream)
	}
	before := model.agent.LiveStream.body

	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "tail"})
	after := model.agent.LiveStream.body
	if after != before+"tail" {
		t.Fatalf("streamed reasoning mismatch: got length %d, want %d", len(after), len(before)+len("tail"))
	}
	if unsafe.StringData(after) != unsafe.StringData(before) {
		t.Fatal("small reasoning appends must reuse the warmed live body buffer instead of copying the full trace")
	}
	if model.timeline.Version != beforeVersion || reasoningBody(model) != "" {
		t.Fatal("reasoning deltas must not mutate committed timeline while reusing the live buffer")
	}
}

func TestAgentDoneReleasesLiveReasoningBuffer(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "complete reasoning"})
	if model.agent.LiveStream == nil || model.agent.LiveStream.kind != BlockReasoning || model.agent.LiveStream.buffer == nil {
		t.Fatalf("expected an active reasoning live buffer before completion, got %#v", model.agent.LiveStream)
	}
	if got := reasoningBody(model); got != "" {
		t.Fatalf("reasoning delta mutated committed history before completion: %q", got)
	}

	next, _ := model.Update(agentDoneMsg{})
	model = next.(Model)
	if model.agent.LiveStream != nil {
		t.Fatalf("agent completion must release live stream state, got %#v", model.agent.LiveStream)
	}
	if got := reasoningBody(model); got != "complete reasoning" {
		t.Fatalf("releasing the live buffer changed the final transcript: %q", got)
	}
}

func TestLiveReasoningBufferDoesNotCrossAssistantOrToolBoundary(t *testing.T) {
	t.Run("assistant", func(t *testing.T) {
		model := NewModel(Config{})
		model.agent.Busy = true
		model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "before assistant"})
		if model.agent.LiveStream == nil {
			t.Fatal("expected reasoning live state before assistant boundary")
		}
		firstBuffer := model.agent.LiveStream.buffer
		firstGeneration := model.agent.LiveStream.generation

		model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "answer"})
		live := model.agent.LiveStream
		if live == nil || live.kind != BlockAssistant || live.body != "answer" {
			t.Fatalf("assistant delta did not replace reasoning live state: %#v", live)
		}
		if live.buffer == firstBuffer || live.generation <= firstGeneration {
			t.Fatal("assistant text must start a new live buffer generation")
		}
		blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
		if len(blocks) != 1 || blocks[0].Kind != BlockReasoning || blocks[0].Body != "before assistant" {
			t.Fatalf("assistant delta committed more than the preceding reasoning boundary: %#v", blocks)
		}

		assistantBuffer := live.buffer
		model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "after assistant"})
		live = model.agent.LiveStream
		if live == nil || live.kind != BlockReasoning || live.body != "after assistant" {
			t.Fatalf("reasoning delta did not start a new live stream: %#v", live)
		}
		if live.buffer == firstBuffer || live.buffer == assistantBuffer {
			t.Fatal("reasoning after assistant text must use a new live buffer")
		}
		blocks = model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
		if len(blocks) != 2 || blocks[0].Kind != BlockReasoning || blocks[0].Body != "before assistant" ||
			blocks[1].Kind != BlockAssistant || blocks[1].Body != "answer" {
			t.Fatalf("unexpected committed reasoning/assistant ordering: %#v", blocks)
		}
	})

	t.Run("tool", func(t *testing.T) {
		model := NewModel(Config{})
		model.agent.Busy = true
		model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "before tool"})
		if model.agent.LiveStream == nil {
			t.Fatal("expected reasoning live state before tool boundary")
		}
		firstBuffer := model.agent.LiveStream.buffer
		firstGeneration := model.agent.LiveStream.generation

		model = model.ApplyAgentEvent(agent.Event{Type: agent.EventToolRequested, ToolName: "read_file", ToolCallID: "call-1"})
		if model.agent.LiveStream != nil {
			t.Fatalf("tool activity must commit and clear preceding reasoning, got %#v", model.agent.LiveStream)
		}
		blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
		if len(blocks) != 2 || blocks[0].Kind != BlockReasoning || blocks[0].Body != "before tool" || blocks[1].Kind != BlockTool {
			t.Fatalf("unexpected committed reasoning/tool ordering: %#v", blocks)
		}

		model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "after tool"})
		live := model.agent.LiveStream
		if live == nil || live.kind != BlockReasoning || live.body != "after tool" {
			t.Fatalf("reasoning after tool did not start a live stream: %#v", live)
		}
		if live.buffer == firstBuffer || live.generation <= firstGeneration {
			t.Fatal("reasoning after a tool must use a new live buffer generation")
		}
		blocks = model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
		if len(blocks) != 2 {
			t.Fatalf("post-tool reasoning delta mutated committed history: %#v", blocks)
		}
	})
}

func TestMultiStepReasoningMergesOneBlockAndKeepsAllTools(t *testing.T) {
	// Full event path: several ReAct steps with thinking between tools must
	// yield one folded thinking header and every tool row still renderable.
	model := NewModel(Config{})
	model.agent.Busy = true
	model.showToolDetails = true
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventAgentStarted})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "plan step one\n"})
	model = model.ApplyAgentEvent(agent.Event{
		Type: agent.EventToolRequested, ToolName: "read_file", ToolCallID: "c1",
		Input: `{"path":"a.go"}`,
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type: agent.EventToolResult, ToolName: "read_file", ToolCallID: "c1", Output: "ok-a",
	})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "plan step two\n"})
	model = model.ApplyAgentEvent(agent.Event{
		Type: agent.EventToolRequested, ToolName: "edit_file", ToolCallID: "c2",
		Input: `{"path":"b.go"}`,
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type: agent.EventToolResult, ToolName: "edit_file", ToolCallID: "c2", Output: "ok-b",
	})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "wrap up\n"})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "done answer"})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventAgentFinished, Message: "done answer"})

	blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
	var reasoning, tools int
	var reasonBody string
	for _, b := range blocks {
		switch b.Kind {
		case BlockReasoning:
			reasoning++
			reasonBody = b.Body
		case BlockTool:
			tools++
		}
	}
	if reasoning != 1 {
		t.Fatalf("want 1 thinking block after multi-step turn, got %d: %#v", reasoning, blocks)
	}
	if tools != 2 {
		t.Fatalf("want 2 tool rows (not swallowed), got %d: %#v", tools, blocks)
	}
	for _, want := range []string{"plan step one", "plan step two", "wrap up"} {
		if !strings.Contains(reasonBody, want) {
			t.Fatalf("merged thinking missing %q in %q", want, reasonBody)
		}
	}

	lines, _ := model.renderTimelineLines(100)
	view := ansi.Strip(strings.Join(lines, "\n"))
	// CC mode A: default hides thinking text; tools must still show.
	if strings.Contains(view, "plan step one") || strings.Contains(view, "plan step two") {
		t.Fatalf("default view must hide thinking body:\n%s", view)
	}
	// Tool rows use verb/subject chrome (Read/Edit or raw name).
	if !strings.Contains(view, "a.go") && !strings.Contains(view, "read_file") {
		t.Fatalf("view missing first tool:\n%s", view)
	}
	if !strings.Contains(view, "b.go") && !strings.Contains(view, "edit_file") {
		t.Fatalf("view missing second tool:\n%s", view)
	}
	model.showThinking = true
	lines, _ = model.renderTimelineLines(100)
	view = ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(view, "plan step one") {
		t.Fatalf("showThinking on must reveal merged thinking:\n%s", view)
	}
}

func TestLiveReasoningBufferGuardsBranchedModels(t *testing.T) {
	base := NewModel(Config{})
	base.agent.Busy = true
	base = base.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: strings.Repeat("a", 4096)})
	base = base.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: strings.Repeat("b", 4096)})
	if base.agent.LiveStream == nil {
		t.Fatal("expected reasoning live state before branching")
	}
	baseBody := base.agent.LiveStream.body
	beforeVersion := base.timeline.Version

	left := base.applyReasoningChunk(" left", time.Now())
	right := base.applyReasoningChunk(" right", time.Now())
	if got := base.agent.LiveStream.body; got != baseBody {
		t.Fatalf("branching mutated the source reasoning live body: got length %d, want %d", len(got), len(baseBody))
	}
	if left.agent.LiveStream == nil || left.agent.LiveStream.body != baseBody+" left" {
		t.Fatalf("left reasoning live branch leaked: %#v", left.agent.LiveStream)
	}
	if right.agent.LiveStream == nil || right.agent.LiveStream.body != baseBody+" right" {
		t.Fatalf("right reasoning live branch leaked: %#v", right.agent.LiveStream)
	}
	if base.timeline.Version != beforeVersion || left.timeline.Version != beforeVersion || right.timeline.Version != beforeVersion {
		t.Fatal("branched reasoning appends mutated committed timeline state")
	}
}

func TestContextEventsKeepLiveReasoningBuffer(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "before usage"})
	if model.agent.LiveStream == nil || model.agent.LiveStream.kind != BlockReasoning {
		t.Fatalf("expected reasoning live state, got %#v", model.agent.LiveStream)
	}
	stream := model.agent.LiveStream
	buffer := stream.buffer
	generation := stream.generation
	beforeVersion := model.timeline.Version

	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventContextUpdated, ContextTokens: 100, ContextMaxTokens: 1000})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventContextMeasured, MeasuredInputTokens: 101, MeasuredOutputTokens: 7})
	if model.agent.LiveStream != stream || model.agent.LiveStream.buffer != buffer {
		t.Fatal("context and usage events must preserve the active reasoning stream and buffer")
	}
	if model.agent.LiveStream.body != "before usage" || model.agent.LiveStream.generation != generation || model.agent.LiveStream.visibleBody() != "" {
		t.Fatalf("context events changed reasoning live state: %#v", model.agent.LiveStream)
	}

	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: " after usage"})
	live := model.agent.LiveStream
	if live == nil || live.body != "before usage after usage" || live.generation != generation || live.buffer != buffer {
		t.Fatalf("reasoning around context events did not remain consecutive: %#v", live)
	}
	if live.visibleBody() != "" {
		t.Fatalf("unfinished reasoning tail became visible without a newline: %q", live.visibleBody())
	}
	if model.timeline.Version != beforeVersion || reasoningBody(model) != "" {
		t.Fatal("context-interleaved reasoning deltas mutated committed history")
	}
}

func TestLiveStreamState(t *testing.T) {
	now := time.Unix(123, 0)
	tests := []struct {
		name       string
		kind       BlockKind
		wantDetail string
	}{
		{name: "assistant", kind: BlockAssistant, wantDetail: "responding"},
		{name: "reasoning", kind: BlockReasoning, wantDetail: "thinking"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(Config{})
			model.timeline = model.timeline.StartUserTurn("prompt").MarkAgentStarted(now)
			beforeVersion := model.timeline.Version

			model = model.appendLiveChunk(tt.kind, "first\npartial", now)
			if model.timeline.Version != beforeVersion {
				t.Fatalf("live append changed timeline version: got %d, want %d", model.timeline.Version, beforeVersion)
			}
			if model.agent.LiveStream == nil {
				t.Fatal("live append did not create stream state")
			}
			if got := model.agent.LiveStream.kind; got != tt.kind {
				t.Fatalf("live kind = %q, want %q", got, tt.kind)
			}
			if got := model.agent.LiveStream.body; got != "first\npartial" {
				t.Fatalf("live body = %q, want %q", got, "first\npartial")
			}
			if got := model.agent.LiveStream.visibleBody(); got != "first\n" {
				t.Fatalf("visible body = %q, want %q", got, "first\n")
			}
			if got := model.agent.LiveDetail; got != tt.wantDetail {
				t.Fatalf("live detail = %q, want %q", got, tt.wantDetail)
			}

			generation := model.agent.LiveStream.generation
			model = model.appendLiveChunk(tt.kind, "tail", now)
			if got := model.agent.LiveStream.body; got != "first\npartialtail" {
				t.Fatalf("same-kind append body = %q", got)
			}
			if got := model.agent.LiveStream.generation; got != generation {
				t.Fatalf("same-kind append generation = %d, want %d", got, generation)
			}
		})
	}

	t.Run("empty chunk is no-op", func(t *testing.T) {
		model := NewModel(Config{})
		beforeVersion := model.timeline.Version
		next := model.appendLiveChunk(BlockAssistant, "", now)
		if next.agent.LiveStream != nil || next.agent.LiveGeneration != 0 || next.agent.LiveDetail != "" {
			t.Fatalf("empty chunk changed live state: %#v", next.agent)
		}
		if next.timeline.Version != beforeVersion {
			t.Fatalf("empty chunk changed timeline version: got %d, want %d", next.timeline.Version, beforeVersion)
		}
	})
}

func TestLiveStreamVisiblePrefix(t *testing.T) {
	model := NewModel(Config{})
	model = model.appendLiveChunk(BlockAssistant, "hidden", time.Time{})
	if got := model.agent.LiveStream.visibleBody(); got != "" {
		t.Fatalf("unfinished first line became visible: %q", got)
	}

	model = model.appendLiveChunk(BlockAssistant, " tail\nnext", time.Time{})
	if got := model.agent.LiveStream.visibleBody(); got != "hidden tail\n" {
		t.Fatalf("later newline did not reveal buffered tail: %q", got)
	}

	model = model.appendLiveChunk(BlockAssistant, " still hidden", time.Time{})
	if got := model.agent.LiveStream.visibleBody(); got != "hidden tail\n" {
		t.Fatalf("newline-free growth changed visible prefix: %q", got)
	}
}

func TestLiveStreamCopyIsolation(t *testing.T) {
	model := NewModel(Config{})
	base := model.appendLiveChunk(BlockAssistant, "base", time.Time{})
	left := base.appendLiveChunk(BlockAssistant, "-left", time.Time{})
	right := base.appendLiveChunk(BlockAssistant, "-right", time.Time{})

	if got := base.agent.LiveStream.body; got != "base" {
		t.Fatalf("branch append mutated base body: %q", got)
	}
	if got := left.agent.LiveStream.body; got != "base-left" {
		t.Fatalf("left branch body = %q", got)
	}
	if got := right.agent.LiveStream.body; got != "base-right" {
		t.Fatalf("right branch body = %q", got)
	}
}

func TestLiveStreamGeneration(t *testing.T) {
	model := NewModel(Config{})
	model = model.appendLiveChunk(BlockAssistant, "one", time.Time{})
	first := model.agent.LiveStream.generation
	if first == 0 || model.agent.LiveGeneration != first {
		t.Fatalf("first generation = %d, source = %d", first, model.agent.LiveGeneration)
	}

	model = model.appendLiveChunk(BlockAssistant, " two", time.Time{})
	if got := model.agent.LiveStream.generation; got != first {
		t.Fatalf("same-kind append advanced generation: got %d, want %d", got, first)
	}

	model.agent.LiveStream = nil
	model = model.appendLiveChunk(BlockReasoning, "three", time.Time{})
	if got := model.agent.LiveStream.generation; got != first+1 {
		t.Fatalf("successive live block generation = %d, want %d", got, first+1)
	}
	if got := model.agent.LiveGeneration; got != first+1 {
		t.Fatalf("generation source = %d, want %d", got, first+1)
	}
}

func TestStreamDeltasDoNotMutateTimeline(t *testing.T) {
	tests := []struct {
		name       string
		eventType  agent.EventType
		kind       BlockKind
		wantDetail string
	}{
		{name: "assistant", eventType: agent.EventModelChunk, kind: BlockAssistant, wantDetail: "responding"},
		{name: "reasoning", eventType: agent.EventReasoningChunk, kind: BlockReasoning, wantDetail: "thinking"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(Config{})
			model.timeline = model.timeline.StartUserTurn("prompt").MarkAgentStarted(time.Unix(100, 0))
			before := model.timeline
			beforeVersion := model.timeline.Version
			beforeBlocks := len(model.timeline.Turns[len(model.timeline.Turns)-1].Blocks)
			beforeKey := timelineKeyForTurn(model.timeline.Turns[len(model.timeline.Turns)-1])

			for _, chunk := range []string{"first", "\nsecond", " tail"} {
				model = model.ApplyAgentEvent(agent.Event{Type: tt.eventType, Message: chunk})
			}

			if model.timeline.Version != beforeVersion {
				t.Fatalf("delta path changed timeline version: got %d, want %d", model.timeline.Version, beforeVersion)
			}
			if got := len(model.timeline.Turns[len(model.timeline.Turns)-1].Blocks); got != beforeBlocks {
				t.Fatalf("delta path changed committed block count: got %d, want %d", got, beforeBlocks)
			}
			if got := timelineKeyForTurn(model.timeline.Turns[len(model.timeline.Turns)-1]); got != beforeKey {
				t.Fatalf("delta path changed turn structural key: got %#v, want %#v", got, beforeKey)
			}
			if !reflect.DeepEqual(model.timeline, before) {
				t.Fatalf("delta path mutated committed timeline:\n got %#v\nwant %#v", model.timeline, before)
			}
			if model.agent.LiveStream == nil || model.agent.LiveStream.kind != tt.kind || model.agent.LiveStream.body != "first\nsecond tail" {
				t.Fatalf("unexpected live state: %#v", model.agent.LiveStream)
			}
			if got := model.agent.LiveDetail; got != tt.wantDetail {
				t.Fatalf("live detail = %q, want %q", got, tt.wantDetail)
			}
		})
	}
}

func TestLiveContextEventsPreserveStream(t *testing.T) {
	model := NewModel(Config{})
	model = model.appendLiveChunk(BlockAssistant, "complete\ntail", time.Time{})
	before := *model.agent.LiveStream

	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventContextUpdated, ContextTokens: 100, ContextMaxTokens: 1000})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventContextMeasured, MeasuredInputTokens: 101, MeasuredOutputTokens: 7})

	if model.agent.LiveStream == nil {
		t.Fatal("context events cleared live stream")
	}
	after := *model.agent.LiveStream
	if after.kind != before.kind || after.body != before.body || after.visibleLen != before.visibleLen || after.generation != before.generation {
		t.Fatalf("context events changed live stream: got %#v, want %#v", after, before)
	}
}

func TestChildTraceEventPreservesStream(t *testing.T) {
	model := NewModel(Config{})
	model = model.appendLiveChunk(BlockReasoning, "complete\ntail", time.Time{})
	before := *model.agent.LiveStream

	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventSubagentToolCall,
		ToolCallID: "child-1",
		ToolName:   "read_file",
		Message:    "running",
	})

	if model.agent.LiveStream == nil {
		t.Fatal("child trace event cleared live stream")
	}
	after := *model.agent.LiveStream
	if after.kind != before.kind || after.body != before.body || after.visibleLen != before.visibleLen || after.generation != before.generation {
		t.Fatalf("child trace event changed live stream: got %#v, want %#v", after, before)
	}
}

func TestOnlyVisibleLiveLinesMarkNewOutput(t *testing.T) {
	model := NewModel(Config{})
	model.scrollPaused = true
	model.scroll = 3

	model = model.appendLiveChunk(BlockAssistant, "hidden tail", time.Time{})
	if model.newOutputBelow || model.newOutputCount != 0 {
		t.Fatalf("hidden live tail marked visible output: below=%v count=%d", model.newOutputBelow, model.newOutputCount)
	}

	model = model.appendLiveChunk(BlockAssistant, " completed\nnext tail", time.Time{})
	if !model.newOutputBelow || model.newOutputCount != 1 {
		t.Fatalf("newline did not mark newly visible output once: below=%v count=%d", model.newOutputBelow, model.newOutputCount)
	}

	model = model.appendLiveChunk(BlockAssistant, " still hidden", time.Time{})
	if model.newOutputCount != 1 {
		t.Fatalf("hidden tail growth changed visible output count: %d", model.newOutputCount)
	}
}

func TestLiveStreamKindSwitchCommits(t *testing.T) {
	tests := []struct {
		name       string
		firstType  agent.EventType
		firstKind  BlockKind
		secondType agent.EventType
		secondKind BlockKind
	}{
		{
			name:       "assistant to reasoning",
			firstType:  agent.EventModelChunk,
			firstKind:  BlockAssistant,
			secondType: agent.EventReasoningChunk,
			secondKind: BlockReasoning,
		},
		{
			name:       "reasoning to assistant",
			firstType:  agent.EventReasoningChunk,
			firstKind:  BlockReasoning,
			secondType: agent.EventModelChunk,
			secondKind: BlockAssistant,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(Config{})
			model.timeline = model.timeline.StartUserTurn("prompt")
			model = model.ApplyAgentEvent(agent.Event{Type: tt.firstType, Message: "complete\nhidden tail"})
			firstGeneration := model.agent.LiveStream.generation

			model = model.ApplyAgentEvent(agent.Event{Type: tt.secondType, Message: "next"})

			blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
			if len(blocks) != 1 || blocks[0].Kind != tt.firstKind || blocks[0].Body != "complete\nhidden tail" {
				t.Fatalf("kind switch did not commit full prior stream exactly once: %#v", blocks)
			}
			if model.agent.LiveStream == nil || model.agent.LiveStream.kind != tt.secondKind || model.agent.LiveStream.body != "next" {
				t.Fatalf("kind switch did not start next live stream: %#v", model.agent.LiveStream)
			}
			if got := model.agent.LiveStream.generation; got != firstGeneration+1 {
				t.Fatalf("kind switch generation = %d, want %d", got, firstGeneration+1)
			}
		})
	}
}

func TestCommitLiveStreamIsIdempotentAndIncludesUnfinishedTail(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("prompt")
	model = model.appendLiveChunk(BlockAssistant, "complete\nunfinished tail", time.Time{})
	beforeVersion := model.timeline.Version

	model = model.commitLiveStream()
	if model.agent.LiveStream != nil || model.agent.LiveDetail != "" {
		t.Fatalf("commit did not clear live state/detail: stream=%#v detail=%q", model.agent.LiveStream, model.agent.LiveDetail)
	}
	blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
	if len(blocks) != 1 || blocks[0].Kind != BlockAssistant || blocks[0].Body != "complete\nunfinished tail" {
		t.Fatalf("commit lost or duplicated unfinished tail: %#v", blocks)
	}
	if model.timeline.Version != beforeVersion+1 {
		t.Fatalf("commit version = %d, want %d", model.timeline.Version, beforeVersion+1)
	}
	committed := model.timeline
	model = model.commitLiveStream()
	if !reflect.DeepEqual(model.timeline, committed) {
		t.Fatalf("second commit changed history: timeline=%#v", model.timeline)
	}
}

func TestCommitLiveStreamClearsEmptyStateWithoutBlock(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("prompt")
	model.agent.LiveStream = &liveStreamState{kind: BlockAssistant, generation: 1}
	model.agent.LiveDetail = "responding"
	beforeVersion := model.timeline.Version

	model = model.commitLiveStream()
	if model.agent.LiveStream != nil || model.agent.LiveDetail != "" {
		t.Fatalf("empty commit did not clear live state: %#v", model.agent)
	}
	if model.timeline.Version != beforeVersion || len(model.timeline.Turns[0].Blocks) != 0 {
		t.Fatalf("empty commit changed timeline: %#v", model.timeline)
	}
}

func TestCommitLiveStreamKeepsAdjacentAssistantBlocksDistinct(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("prompt")
	model = model.appendLiveChunk(BlockAssistant, "first", time.Time{}).commitLiveStream()
	model = model.appendLiveChunk(BlockAssistant, "second", time.Time{}).commitLiveStream()

	blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
	if len(blocks) != 2 || blocks[0].Kind != BlockAssistant || blocks[0].Body != "first" ||
		blocks[1].Kind != BlockAssistant || blocks[1].Body != "second" || blocks[0].ID == blocks[1].ID {
		t.Fatalf("adjacent assistant commits merged or lost identity: %#v", blocks)
	}
}

func TestToolRequestCommitsLiveBeforeTool(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("prompt")
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "answer tail"})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolRequested,
		ToolName:   "read_file",
		ToolCallID: "call-1",
		Input:      `{"path":"README.md"}`,
	})

	blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
	if len(blocks) != 2 || blocks[0].Kind != BlockAssistant || blocks[0].Body != "answer tail" || blocks[1].Kind != BlockTool {
		t.Fatalf("tool request ordering = %#v", blocks)
	}
	if blocks[1].Tool == nil || blocks[1].Tool.ID != "call-1" || blocks[1].Tool.Status != ToolRunning {
		t.Fatalf("tool request identity/status was not preserved: %#v", blocks[1].Tool)
	}
}

func TestToolConfirmationCommitsPrecedingReasoning(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("prompt")
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "reasoning tail"})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolConfirmationRequested,
		ToolName:   "shell",
		ToolCallID: "call-confirm",
		Risk:       "medium",
	})

	blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
	if len(blocks) != 2 || blocks[0].Kind != BlockReasoning || blocks[0].Body != "reasoning tail" || blocks[1].Kind != BlockTool {
		t.Fatalf("confirmation ordering = %#v", blocks)
	}
	if blocks[1].Tool == nil || blocks[1].Tool.ID != "call-confirm" || blocks[1].Tool.Status != ToolPending {
		t.Fatalf("confirmation tool identity/status was not preserved: %#v", blocks[1].Tool)
	}
}

func TestLiveStreamBoundaryTable(t *testing.T) {
	tests := []struct {
		name  string
		event agent.Event
	}{
		{name: "agent started", event: agent.Event{Type: agent.EventAgentStarted}},
		{name: "compaction started", event: agent.Event{Type: agent.EventCompactionStarted}},
		{name: "compaction finished", event: agent.Event{Type: agent.EventCompactionFinished, ContextTokens: 50, ContextMaxTokens: 1000}},
		{name: "tool requested", event: agent.Event{Type: agent.EventToolRequested, ToolName: "read_file", ToolCallID: "call-1"}},
		{name: "tool confirmation", event: agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "shell", ToolCallID: "call-1"}},
		{name: "tool approved", event: agent.Event{Type: agent.EventToolApproved, ToolName: "shell", ToolCallID: "call-1"}},
		{name: "tool rejected", event: agent.Event{Type: agent.EventToolRejected, ToolName: "shell", ToolCallID: "call-1"}},
		{name: "tool result", event: agent.Event{Type: agent.EventToolResult, ToolName: "read_file", ToolCallID: "call-1", Output: "ok"}},
		{name: "loop guard", event: agent.Event{Type: agent.EventLoopGuard, Message: "guarded"}},
		{name: "text degeneration", event: agent.Event{Type: agent.EventTextDegeneration, Message: "recovering"}},
		{name: "subagent started", event: agent.Event{Type: agent.EventSubagentStarted, ToolCallID: "child-1", ToolName: "explore", Message: "start"}},
		{name: "subagent progress", event: agent.Event{Type: agent.EventSubagentProgress, ToolCallID: "child-1", ToolName: "explore", Message: "progress"}},
		{name: "subagent finished", event: agent.Event{Type: agent.EventSubagentFinished, ToolCallID: "child-1", ToolName: "explore", Output: "done"}},
		{name: "subagent failed", event: agent.Event{Type: agent.EventSubagentFailed, ToolCallID: "child-1", ToolName: "explore", Error: "boom"}},
		{name: "subagent cancelled", event: agent.Event{Type: agent.EventSubagentCancelled, ToolCallID: "child-1", ToolName: "explore", Message: "cancelled"}},
		{name: "agent finished", event: agent.Event{Type: agent.EventAgentFinished, Message: "fallback ignored in task 3"}},
		{name: "agent error", event: agent.Event{Type: agent.EventAgentError, Error: "boom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(Config{})
			model.timeline = model.timeline.StartUserTurn("prompt")
			model = model.appendLiveChunk(BlockAssistant, "complete\nunfinished", time.Time{})

			model = model.ApplyAgentEvent(tt.event)

			if model.agent.LiveStream != nil || model.agent.LiveDetail != "" {
				t.Fatalf("boundary left live state active: stream=%#v detail=%q", model.agent.LiveStream, model.agent.LiveDetail)
			}
			count := 0
			for _, block := range model.timeline.Turns[len(model.timeline.Turns)-1].Blocks {
				if block.Kind == BlockAssistant && block.Body == "complete\nunfinished" {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("boundary committed live stream %d times; timeline=%#v", count, model.timeline)
			}
		})
	}
}

func TestTextDegenerationSplitsAssistantBlocks(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("prompt")
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "before"})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventTextDegeneration, Message: "recovery notice"})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "after"})
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventAgentFinished})

	blocks := model.timeline.Turns[len(model.timeline.Turns)-1].Blocks
	if len(blocks) != 3 || blocks[0].Kind != BlockAssistant || blocks[0].Body != "before" ||
		blocks[1].Kind != BlockCommand || blocks[1].Title != "recovering" ||
		blocks[2].Kind != BlockAssistant || blocks[2].Body != "after" {
		t.Fatalf("degeneration boundary ordering = %#v", blocks)
	}
}

func TestLiveAssistantLongLineWrapsToTimelineWidth(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model = applyChunkEvent(t, model, "alpha beta gamma delta\n")

	lines := model.renderLiveStreamLines(12)
	if len(lines) < 2 {
		t.Fatalf("long live line should wrap instead of being truncated, got %#v", lines)
	}
	joined := ansi.Strip(strings.Join(lines, "\n"))
	for _, word := range []string{"alpha", "beta", "gamma", "delta"} {
		if !strings.Contains(joined, word) {
			t.Fatalf("wrapped live output lost %q: %q", word, joined)
		}
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > 12 {
			t.Fatalf("wrapped line width = %d, want <= 12: %q", got, ansi.Strip(line))
		}
	}
}
