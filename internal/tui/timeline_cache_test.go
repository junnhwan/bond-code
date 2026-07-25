package tui

import (
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestTimelineLinesCacheHitsUnchangedRecomputesOnChange(t *testing.T) {
	m := NewModel(Config{})
	m.timeline = m.timeline.StartUserTurn("hello")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "world")

	l1, _ := m.renderTimelineLines(80)
	if len(l1) == 0 {
		t.Fatal("expected rendered timeline lines")
	}
	l2, _ := m.renderTimelineLines(80)
	if &l1[0] != &l2[0] {
		t.Fatal("unchanged timeline should reuse cached lines")
	}

	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "more")
	l3, _ := m.renderTimelineLines(80)
	if !strings.Contains(strings.Join(l3, "\n"), "more") {
		t.Fatalf("changed timeline was not rendered:\n%s", strings.Join(l3, "\n"))
	}
}

func TestTimelineLinesCacheDoesNotLeakAcrossEquivalentSessionReplacement(t *testing.T) {
	m := NewModel(Config{})
	m.timeline = SeedTimeline([]SeedMessage{{Role: "user", Content: "first prompt"}, {Role: "assistant", Content: "first answer"}})
	_, _ = m.renderTimelineLines(80)

	m.timeline = SeedTimeline([]SeedMessage{{Role: "user", Content: "second prompt"}, {Role: "assistant", Content: "second answer"}})
	lines, _ := m.renderTimelineLines(80)
	view := strings.Join(lines, "\n")
	if !strings.Contains(view, "second answer") || strings.Contains(view, "first answer") {
		t.Fatalf("session replacement reused stale history:\n%s", view)
	}
}

func TestTimelineBlockLinesCacheReusesStableCommittedBlock(t *testing.T) {
	m := NewModel(Config{})
	m.timeline = m.timeline.StartUserTurn("multi-step prompt")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "stable analysis")
	stable := m.timeline.Turns[0].Blocks[0]
	before := m.renderCachedTimelineBlockLines(stable, 80)
	if len(before) == 0 {
		t.Fatal("expected stable block lines")
	}

	m.timeline = m.timeline.AppendBlock(BlockTool, "tool", "stable tool result")
	after := m.renderCachedTimelineBlockLines(stable, 80)
	if len(after) == 0 || &before[0] != &after[0] {
		t.Fatal("unchanged committed block should reuse its rendered lines")
	}
}

func TestTimelineHistoryAndLiveCacheHitOnNoNewlineGrowth(t *testing.T) {
	m := modelWithLiveStream(BlockAssistant, "shown\nhidden", len("shown\n"))
	before, _ := m.renderTimelineLines(80)
	if len(before) == 0 || len(m.timelineLinesCache.lines) == 0 || len(m.timelineLinesCache.live.lines) == 0 {
		t.Fatal("expected committed history, composed output, and live overlay cache")
	}
	historyData := unsafe.StringData(m.timelineLinesCache.lines[0])
	liveData := unsafe.StringData(m.timelineLinesCache.live.lines[0])
	composedData := unsafe.StringData(before[0])
	beforeHistoryKey := m.timelineLinesCache.key
	beforeLiveKey := m.timelineLinesCache.live.key

	live := *m.agent.LiveStream
	live.body += " tail"
	m.agent.LiveStream = &live
	after, _ := m.renderTimelineLines(80)

	if m.timelineLinesCache.key != beforeHistoryKey {
		t.Fatal("live body growth must not enter the committed-history cache key")
	}
	if m.timelineLinesCache.live.key != beforeLiveKey {
		t.Fatal("no-newline growth must hit the live cache")
	}
	if unsafe.StringData(m.timelineLinesCache.lines[0]) != historyData {
		t.Fatal("no-newline growth must reuse committed history storage")
	}
	if unsafe.StringData(m.timelineLinesCache.live.lines[0]) != liveData {
		t.Fatal("no-newline growth must reuse live overlay storage")
	}
	if unsafe.StringData(after[0]) != composedData {
		t.Fatal("no-newline growth must reuse composed prefix storage")
	}
	if strings.Contains(strings.Join(after, "\n"), "hidden") {
		t.Fatalf("unfinished tail became visible:\n%s", strings.Join(after, "\n"))
	}
}

func TestTimelineLiveCacheRefreshesWhenLineCompletes(t *testing.T) {
	m := modelWithLiveStream(BlockAssistant, "shown\nhidden", len("shown\n"))
	_, _ = m.renderTimelineLines(80)
	beforeHistoryData := unsafe.StringData(m.timelineLinesCache.lines[0])
	beforeVisibleLen := m.timelineLinesCache.live.key.visibleLen

	live := *m.agent.LiveStream
	live.body = "shown\nhidden tail\nnext"
	live.visibleLen = len("shown\nhidden tail\n")
	m.agent.LiveStream = &live
	lines, _ := m.renderTimelineLines(80)
	view := strings.Join(lines, "\n")

	if m.timelineLinesCache.live.key.visibleLen == beforeVisibleLen {
		t.Fatal("line completion must invalidate the live overlay cache")
	}
	if unsafe.StringData(m.timelineLinesCache.lines[0]) != beforeHistoryData {
		t.Fatal("line completion must retain committed history storage")
	}
	if !strings.Contains(view, "hidden tail") || strings.Contains(view, "next") {
		t.Fatalf("complete-line visibility mismatch:\n%s", view)
	}
}

func TestTimelineLiveCacheInvalidatesForWidthAccentAndThinkingToggle(t *testing.T) {
	m := modelWithLiveStream(BlockReasoning, "first\nsecond\n", len("first\nsecond\n"))
	_, _ = m.renderTimelineLines(80)
	if got := m.timelineLinesCache.live.key.width; got != 80 {
		t.Fatalf("live width key = %d, want 80", got)
	}

	_, _ = m.renderTimelineLines(42)
	if got := m.timelineLinesCache.live.key.width; got != 42 {
		t.Fatalf("width change did not invalidate live cache, key=%d", got)
	}

	original := AccentPresets[0]
	defer ApplyAccent(original.Color)
	preset := AccentPresets[1]
	ApplyAccent(preset.Color)
	m.accent = preset.Name
	_, _ = m.renderTimelineLines(42)
	if got := m.timelineLinesCache.live.key.accent; got != preset.Name {
		t.Fatalf("accent change did not invalidate live cache, key=%q", got)
	}

	m.showThinking = true
	_, _ = m.renderTimelineLines(42)
	if !m.timelineLinesCache.live.key.showThinking {
		t.Fatal("thinking visibility toggle did not invalidate live cache")
	}
}

func TestTimelineCommitClearsLiveOverlayAndInvalidatesHistoryOnce(t *testing.T) {
	m := modelWithLiveStream(BlockAssistant, "shown\nhidden", len("shown\n"))
	_, _ = m.renderTimelineLines(80)
	beforeVersion := m.timeline.Version

	m = m.commitLiveStream()
	if m.timeline.Version != beforeVersion+1 {
		t.Fatalf("commit changed timeline version by %d, want 1", m.timeline.Version-beforeVersion)
	}
	lines, _ := m.renderTimelineLines(80)
	view := strings.Join(lines, "\n")
	if m.agent.LiveStream != nil || m.timelineLinesCache.live.initialized || len(m.timelineLinesCache.live.lines) != 0 {
		t.Fatal("commit must clear live overlay output")
	}
	if strings.Count(view, "shown") != 1 || strings.Count(view, "hidden") != 1 {
		t.Fatalf("committed body should render exactly once:\n%s", view)
	}
}

func TestTimelineLinesCacheRefreshesLiveRunStatusOnSpinnerTick(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 24)
	for i := 0; i < 64; i++ {
		m.timeline = m.timeline.StartUserTurn("historical prompt")
		m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "historical answer")
	}
	m.timeline = m.timeline.StartUserTurn("long-running task")
	m.timeline = m.timeline.MarkAgentStarted(time.Now().Add(-time.Second))
	m.agent.Busy = true
	m.agent.LiveDetail = "responding"

	// Grok stack: live busy status is the dock turn-status row, not the
	// scrollback suffix. Spinner ticks must still refresh that dock row while
	// committed timeline history storage stays stable.
	beforeLines, _ := m.renderTimelineLines(80)
	historyData := unsafe.StringData(m.timelineLinesCache.lines[0])
	beforeStatus := m.renderTurnStatusLine(80)
	beforeSpinner := m.spinner.View()

	next, _ := m.Update(m.spinner.Tick())
	m = next.(Model)
	afterLines, _ := m.renderTimelineLines(80)
	afterStatus := m.renderTurnStatusLine(80)
	if m.spinner.View() == beforeSpinner {
		t.Fatal("spinner tick must advance the spinner")
	}
	if beforeStatus == afterStatus {
		t.Fatal("spinner tick must refresh dock turn-status")
	}
	if !strings.Contains(afterStatus, m.spinner.View()) || !strings.Contains(afterStatus, "responding") {
		t.Fatalf("dock turn-status did not use spinner and LiveDetail:\n%s", afterStatus)
	}
	if unsafe.StringData(m.timelineLinesCache.lines[0]) != historyData {
		t.Fatal("spinner tick must retain committed history storage")
	}
	if len(afterLines) == 0 || len(beforeLines) == 0 {
		t.Fatal("expected timeline lines")
	}
}

func modelWithLiveStream(kind BlockKind, body string, visibleLen int) Model {
	m := NewModel(Config{})
	m.timeline = m.timeline.StartUserTurn("active prompt")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "committed history")
	m.timeline = m.timeline.MarkAgentStarted(time.Now().Add(-time.Second))
	m.agent.Busy = true
	m.agent.LiveDetail = "responding"
	m.agent.LiveGeneration = 1
	m.agent.LiveStream = &liveStreamState{kind: kind, body: body, visibleLen: visibleLen, generation: 1}
	return m
}
