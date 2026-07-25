package tui

import (
	"fmt"
	"testing"
)

// newModelWithTurns builds a model with n user turns, each followed by a
// multi-line assistant reply, so the timeline overflows the viewport and
// message navigation has somewhere to scroll to.
func newModelWithTurns(n int) Model {
	model := NewModel(Config{})
	for i := 0; i < n; i++ {
		model.timeline = model.timeline.StartUserTurn(fmt.Sprintf("user prompt %d", i+1))
		model.timeline = model.timeline.AppendBlock(BlockAssistant, "agent",
			fmt.Sprintf("reply %d line1\nreply %d line2\nreply %d line3\nreply %d line4", i+1, i+1, i+1, i+1))
	}
	return model
}

func TestRenderTimelineLinesRecordsTurnStarts(t *testing.T) {
	model := newModelWithTurns(3)
	model = model.SetSize(100, 30)

	lines, starts := model.renderTimelineLines(80)
	if len(starts) != 3 {
		t.Fatalf("expected 3 turn starts, got %d (lines=%v)", len(starts), starts)
	}
	if starts[0] != 0 {
		t.Fatalf("first turn must start at line 0, got %d", starts[0])
	}
	for i := 1; i < len(starts); i++ {
		if starts[i] <= starts[i-1] {
			t.Fatalf("turn %d start %d must follow turn %d start %d", i, starts[i], i-1, starts[i-1])
		}
	}
	if starts[len(starts)-1] >= len(lines) {
		t.Fatalf("last turn start %d is beyond lines length %d", starts[len(starts)-1], len(lines))
	}
}

func TestNavigateTurnPreviousFromBottomJumpsToLastTurn(t *testing.T) {
	model := newModelWithTurns(3)
	model = model.SetSize(100, 30)

	model = model.navigateTurn(-1)

	if model.navTurnIdx != 2 {
		t.Fatalf("expected navTurnIdx=2 (most recent turn), got %d", model.navTurnIdx)
	}
	if !model.scrollPaused {
		t.Fatal("expected scrollPaused=true after navigation")
	}
	maxScroll := model.maxScroll(model.currentLayout())
	if model.scroll < 0 || model.scroll > maxScroll {
		t.Fatalf("scroll %d out of [0, %d]", model.scroll, maxScroll)
	}
}

func TestNavigateTurnWalksBackwardThroughTurns(t *testing.T) {
	model := newModelWithTurns(3)
	model = model.SetSize(100, 30)

	model = model.navigateTurn(-1)
	if model.navTurnIdx != 2 {
		t.Fatalf("step 1: expected navTurnIdx=2, got %d", model.navTurnIdx)
	}
	model = model.navigateTurn(-1)
	if model.navTurnIdx != 1 {
		t.Fatalf("step 2: expected navTurnIdx=1, got %d", model.navTurnIdx)
	}
	model = model.navigateTurn(-1)
	if model.navTurnIdx != 0 {
		t.Fatalf("step 3: expected navTurnIdx=0, got %d", model.navTurnIdx)
	}
	// One more "previous" at the top must clamp (no negative index, no crash).
	before := model
	model = model.navigateTurn(-1)
	if model.navTurnIdx != 0 {
		t.Fatalf("expected clamped at 0, got %d", model.navTurnIdx)
	}
	if model.scroll != before.scroll {
		t.Fatalf("expected scroll unchanged when clamped, got %d (was %d)", model.scroll, before.scroll)
	}
}

func TestNavigateTurnForwardFromBottomGoesToFirstTurn(t *testing.T) {
	model := newModelWithTurns(3)
	model = model.SetSize(100, 30)

	// From -1 (bottom), "next" jumps to the first turn (earliest).
	model = model.navigateTurn(+1)
	if model.navTurnIdx != 0 {
		t.Fatalf("expected navTurnIdx=0 (first turn), got %d", model.navTurnIdx)
	}

	// Then forward walks toward the most recent.
	model = model.navigateTurn(+1)
	if model.navTurnIdx != 1 {
		t.Fatalf("expected navTurnIdx=1, got %d", model.navTurnIdx)
	}
	// Forward past the last turn clamps.
	model = model.navigateTurn(+1) // -> 2
	model = model.navigateTurn(+1) // clamp
	if model.navTurnIdx != 2 {
		t.Fatalf("expected clamped at last turn (2), got %d", model.navTurnIdx)
	}
}

func TestNavigateTurnEmptyTimelineIsNoOp(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(100, 30)

	model = model.navigateTurn(-1)
	if model.navTurnIdx != -1 {
		t.Fatalf("expected navTurnIdx to stay -1 on empty timeline, got %d", model.navTurnIdx)
	}
	if model.scrollPaused {
		t.Fatal("expected scrollPaused to stay false on empty timeline")
	}
}

func TestNavigateTurnPinsTargetTurnAtViewportTop(t *testing.T) {
	model := newModelWithTurns(5)
	model = model.SetSize(100, 30)

	model = model.navigateTurn(-1) // most recent turn = index 4
	layout := model.currentLayout()
	lines, starts := model.renderTimelineLines(layout.TimelineW)
	maxScroll := len(lines) - layout.TimelineH
	if maxScroll < 0 {
		maxScroll = 0
	}
	wantScroll := maxScroll - starts[4]
	if wantScroll < 0 {
		wantScroll = 0
	}
	if model.scroll != wantScroll {
		t.Fatalf("expected scroll=%d (maxScroll %d - target %d), got %d",
			wantScroll, maxScroll, starts[4], model.scroll)
	}
}

func TestLiveOverlayAndStatusDoNotCreatePhantomTurnStarts(t *testing.T) {
	model := newModelWithTurns(3)
	model.agent.Busy = true
	model.agent.LiveDetail = "responding"
	model.agent.LiveStream = &liveStreamState{kind: BlockAssistant, body: "live\ntail", visibleLen: len("live\n"), generation: 7}

	lines, starts := model.renderTimelineLines(80)
	if len(starts) != 3 {
		t.Fatalf("live overlay/status created phantom turn starts: %v", starts)
	}
	before := append([]int(nil), starts...)
	live := *model.agent.LiveStream
	live.body = "live\ntail done\nnext"
	live.visibleLen = len("live\ntail done\n")
	model.agent.LiveStream = &live
	_, starts = model.renderTimelineLines(80)
	for i := range before {
		if starts[i] != before[i] {
			t.Fatalf("live overlay shifted turn start %d: before=%v after=%v lines=%v", i, before, starts, lines)
		}
	}
}

func TestTranscriptSearchSeesCommittedHistoryWithCachedTurnStarts(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = SeedTimeline([]SeedMessage{
		{Role: "user", Content: "old prompt"},
		{Role: "assistant", Content: "the old needle answer"},
		{Role: "user", Content: "new prompt"},
		{Role: "assistant", Content: "regular answer"},
	})
	model = model.SetSize(80, 10)
	model.search.Query = "needle"

	matches := model.searchMatches(model.currentLayout())
	if len(matches) != 1 {
		t.Fatalf("committed history search matches = %v, want one", matches)
	}
	_, starts := model.renderTimelineLines(model.currentLayout().TimelineW)
	if len(starts) != 2 || starts[0] >= starts[1] {
		t.Fatalf("cached turn starts corrupted during search: %v", starts)
	}
}
