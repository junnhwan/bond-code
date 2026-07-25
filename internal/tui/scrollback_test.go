package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestScrollbackSelectionMovesWithArrows(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 30)
	m.timeline = m.timeline.StartUserTurn("alpha")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "reply alpha")
	m.timeline = m.timeline.StartUserTurn("beta")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "reply beta")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.focus != FocusScrollback {
		t.Fatalf("want scrollback, got %s", m.focus)
	}
	entries := m.scrollEntries()
	if len(entries) < 2 {
		t.Fatalf("want ≥2 entries, got %d", len(entries))
	}
	// Tab into scrollback seeds selection at latest.
	if m.scrollSel < 0 {
		t.Fatal("entering scrollback should seed selection")
	}
	before := m.scrollSel
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(Model)
	if m.scrollSel >= before && before > 0 {
		t.Fatalf("up should move selection earlier, before=%d after=%d", before, m.scrollSel)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.scrollSel < 0 {
		t.Fatal("selection lost after down")
	}
}

func TestScrollbackFoldTogglesToolAndAssistant(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 30)
	m.timeline = m.timeline.StartUserTurn("do work")
	tool := &ToolBlock{
		ID:        "tool-1",
		Name:      "read_file",
		Status:    ToolDone,
		Input:     `{"path":"a.go"}`,
		Output:    strings.Repeat("x\n", 30),
		Collapsed: false,
	}
	m.timeline = m.timeline.AppendBlock(BlockTool, "read_file", renderToolBody(tool))
	// Attach tool pointer with ID.
	turn := m.timeline.Turns[0]
	block := turn.Blocks[0]
	block.ID = "tool-1"
	block.Tool = tool
	turn.Blocks[0] = block
	m.timeline.Turns[0] = turn
	m.timeline.Version++

	// Select the tool entry and fold.
	m.focus = FocusScrollback
	m.scrollSel = 1 // user=0, tool=1 typically
	entries := m.scrollEntries()
	for i, e := range entries {
		if e.kind == string(BlockTool) {
			m.scrollSel = i
			break
		}
	}
	before := ansi.Strip(renderToolActivity(tool, 80))
	m = m.toggleSelectedFold()
	// Tool collapsed should flip.
	entry, ok := m.selectedScrollEntry()
	if !ok {
		t.Fatal("lost selection")
	}
	if entry.blockIdx >= 0 {
		tb := m.timeline.Turns[entry.turnIdx].Blocks[entry.blockIdx].Tool
		if tb == nil || !tb.Collapsed {
			t.Fatal("fold should collapse tool output")
		}
		after := ansi.Strip(renderToolActivity(tb, 80))
		if after == before && strings.Count(after, "\n") > 3 {
			t.Fatalf("collapsed tool should be more compact")
		}
	}

	// Assistant manual fold via foldedEntries.
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "long\nassistant\nbody")
	turn = m.timeline.Turns[0]
	ab := turn.Blocks[len(turn.Blocks)-1]
	ab.ID = "asst-1"
	turn.Blocks[len(turn.Blocks)-1] = ab
	m.timeline.Turns[0] = turn
	m.timeline.Version++
	for i, e := range m.scrollEntries() {
		if e.key == "asst-1" {
			m.scrollSel = i
			break
		}
	}
	m = m.toggleSelectedFold()
	if !m.isEntryFolded("asst-1") {
		t.Fatal("assistant entry should be folded")
	}
	lines := m.renderTimelineBlockLines(m.timeline.Turns[0].Blocks[len(m.timeline.Turns[0].Blocks)-1], 80)
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "folded") {
		t.Fatalf("folded assistant should show fold summary, got %q", joined)
	}
}

func TestLiveStreamLineGated(t *testing.T) {
	if got := liveVisibleLen("partial"); got != 0 {
		t.Fatalf("incomplete line should be hidden, visibleLen=%d", got)
	}
	if got := liveVisibleLen("complete\npartial"); got != len("complete\n") {
		t.Fatalf("only complete lines visible, got %d", got)
	}
	if got := liveVisibleLen("a\nb\n"); got != len("a\nb\n") {
		t.Fatalf("all complete lines visible, got %d", got)
	}
	m := NewModel(Config{}).SetSize(80, 24)
	m.timeline = m.timeline.StartUserTurn("q")
	m = m.appendLiveChunk(BlockAssistant, "no newline yet", timeNowForTest())
	if m.agent.LiveStream == nil || m.agent.LiveStream.visibleLen != 0 {
		t.Fatalf("live visibleLen should stay 0 without newline, got %+v", m.agent.LiveStream)
	}
	m = m.appendLiveChunk(BlockAssistant, " done\n", timeNowForTest())
	if m.agent.LiveStream.visibleLen == 0 {
		t.Fatal("after newline, complete line should become visible")
	}
}

// timeNowForTest is unused by appendLiveChunk (timestamp ignored) but keeps
// call sites explicit.
func timeNowForTest() time.Time { return time.Time{} }

func TestReasoningLeftRightExpandsAtDefaultDensity(t *testing.T) {
	// Default showThinking=false: left/right on a selected reasoning entry must
	// reveal body (was a no-op when expanded := showThinking && !folded).
	m := NewModel(Config{}).SetSize(80, 30)
	if m.showThinking {
		t.Fatal("setup: default showThinking must be false")
	}
	m.timeline = m.timeline.StartUserTurn("think please")
	m.timeline = m.timeline.AppendBlock(BlockReasoning, "thinking", "alpha thought line\nbeta thought line\ngamma thought line")
	turn := m.timeline.Turns[0]
	block := turn.Blocks[0]
	block.ID = "reason-1"
	block.Kind = BlockReasoning
	turn.Blocks[0] = block
	m.timeline.Turns[0] = turn
	m.timeline.Version++

	// Select the reasoning entry via real Update path (Tab + arrows).
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.focus != FocusScrollback {
		t.Fatalf("want scrollback, got %s", m.focus)
	}
	// Move selection onto the reasoning block.
	for i, e := range m.scrollEntries() {
		if e.key == "reason-1" || e.kind == string(BlockReasoning) {
			m.scrollSel = i
			break
		}
	}
	entry, ok := m.selectedScrollEntry()
	if !ok || entry.kind != string(BlockReasoning) {
		t.Fatalf("setup: must select reasoning entry, got %+v ok=%v", entry, ok)
	}

	// Folded at default density: body hidden.
	foldedLines := m.renderTimelineBlockLines(m.timeline.Turns[0].Blocks[0], 80)
	foldedView := ansi.Strip(strings.Join(foldedLines, "\n"))
	if strings.Contains(foldedView, "beta thought") {
		t.Fatalf("default density must hide body, got %q", foldedView)
	}

	// Left expands via real Update path.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(Model)
	if !m.reasoningExpanded("reason-1") {
		t.Fatal("left should expand reasoning at default showThinking=false")
	}
	expandedLines := m.renderTimelineBlockLines(m.timeline.Turns[0].Blocks[0], 80)
	expandedView := ansi.Strip(strings.Join(expandedLines, "\n"))
	if !strings.Contains(expandedView, "alpha thought") {
		t.Fatalf("expanded reasoning must show body, got %q", expandedView)
	}
	if strings.Contains(expandedView, "(folded)") {
		t.Fatalf("expanded view must not say folded, got %q", expandedView)
	}

	// Right collapses again.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.reasoningExpanded("reason-1") {
		t.Fatal("right should re-collapse reasoning")
	}
	reFolded := ansi.Strip(strings.Join(m.renderTimelineBlockLines(m.timeline.Turns[0].Blocks[0], 80), "\n"))
	if strings.Contains(reFolded, "beta thought") {
		t.Fatalf("re-collapsed must hide body, got %q", reFolded)
	}
}
