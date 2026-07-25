package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TestBlitScrollbarThumbPosition checks the proportional thumb parks at the
// bottom when scroll=0 (newest) and at the top when scroll=max (oldest).
func TestBlitScrollbarThumbPosition(t *testing.T) {
	// Build a 5-line block at width 6.
	block := "aaaaaa\nbbbbbb\ncccccc\ndddddd\neeeeee"
	const h = 5
	const w = 6

	// scroll=0 (showing newest): thumb includes the bottom line.
	out := blitScrollbar(block, 0, 10, h, w)
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("expected %d lines, got %d", h, len(lines))
	}
	if !strings.HasSuffix(ansi.Strip(lines[h-1]), "█") {
		t.Errorf("expected thumb covering bottom line at scroll=0:\n%s", out)
	}

	// scroll=max (showing oldest): thumb includes the top line.
	out = blitScrollbar(block, 10, 10, h, w)
	lines = strings.Split(out, "\n")
	if !strings.HasSuffix(ansi.Strip(lines[0]), "█") {
		t.Errorf("expected thumb covering top line at scroll=max:\n%s", out)
	}

	// maxScroll=0 (no overflow): the whole track is thumb, no crash.
	out = blitScrollbar(block, 0, 0, h, w)
	if out == "" {
		t.Fatal("expected non-empty output when there is nothing to scroll")
	}
	for i, l := range strings.Split(out, "\n") {
		if !strings.HasSuffix(ansi.Strip(l), "█") {
			t.Errorf("line %d should be full thumb when maxScroll=0: %q", i, l)
		}
	}
}

// TestBlitScrollbarPreservesContentWidth verifies content is truncated to
// width-1 so the scrollbar never overwrites it.
func TestBlitScrollbarPreservesContentWidth(t *testing.T) {
	block := "abcdef\nghijkl"
	out := blitScrollbar(block, 0, 5, 2, 6)
	lines := strings.Split(out, "\n")
	// Each line: 5 content cells + 1 scrollbar cell = 6 wide.
	for i, l := range lines {
		if got := lipgloss.Width(l); got != 6 {
			t.Errorf("line %d width = %d, want 6 (5 content + 1 scrollbar): %q", i, got, l)
		}
	}
}

// TestBlitScrollbarFillsFullHeight pads short blocks so the track is continuous
// top→bottom (no mid-pane "broken" bar after dock re-fit).
func TestBlitScrollbarFillsFullHeight(t *testing.T) {
	block := "aaaa\nbbbb" // only 2 content lines
	const h, w = 5, 6
	out := blitScrollbar(block, 0, 3, h, w)
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("expected %d lines, got %d:\n%s", h, len(lines), out)
	}
	for i, l := range lines {
		stripped := ansi.Strip(l)
		if !strings.HasSuffix(stripped, "│") && !strings.HasSuffix(stripped, "█") {
			t.Errorf("line %d missing scrollbar glyph: %q", i, l)
		}
		if got := lipgloss.Width(l); got != w {
			t.Errorf("line %d width = %d, want %d", i, got, w)
		}
	}
}

func TestScrollbarMetricsProportionalToOverflow(t *testing.T) {
	const height = 20
	// Slight overflow: thumb should be most of the track, travel short.
	_, slightSize := scrollbarMetrics(0, 2, height)
	if slightSize < height/2 {
		t.Fatalf("slight overflow thumb too small: size=%d height=%d", slightSize, height)
	}
	// Heavy overflow: thumb shorter than slight, but still >= min grab.
	_, heavySize := scrollbarMetrics(0, 200, height)
	minThumb := scrollbarMinThumb(height)
	if heavySize < minThumb {
		t.Fatalf("heavy overflow thumb below min: size=%d min=%d", heavySize, minThumb)
	}
	if heavySize >= slightSize {
		t.Fatalf("heavy overflow should shrink thumb: slight=%d heavy=%d", slightSize, heavySize)
	}
	// Travel for slight overflow is short (don't force a long drag for 2 lines).
	slightTravel := height - slightSize
	heavyTravel := height - heavySize
	if slightTravel >= heavyTravel {
		t.Fatalf("slight overflow should have shorter travel: slight=%d heavy=%d", slightTravel, heavyTravel)
	}
}

func TestScrollFromScrollbarYInvertsThumbPos(t *testing.T) {
	const height = 20
	const maxScroll = 30
	// Bottom of track → scroll 0 (newest).
	if got := scrollFromScrollbarY(height-1, height, maxScroll); got != 0 {
		t.Fatalf("bottom y → scroll = %d, want 0", got)
	}
	// Top of track → scroll max.
	if got := scrollFromScrollbarY(0, height, maxScroll); got != maxScroll {
		t.Fatalf("top y → scroll = %d, want %d", got, maxScroll)
	}
	// Round-trip via thumb top for a mid scroll.
	for _, scroll := range []int{0, 5, 10, 15, 20, 30} {
		top, size := scrollbarMetrics(scroll, maxScroll, height)
		// Click the center of the thumb should recover approximately scroll.
		center := top + size/2
		got := scrollFromScrollbarY(center, height, maxScroll)
		// Integer division may not be exact; allow a small window.
		if got < scroll-2 || got > scroll+2 {
			t.Fatalf("round-trip scroll=%d top=%d size=%d center=%d got=%d", scroll, top, size, center, got)
		}
	}
}

func TestScrollbarAutoShowsWhenOverflowAndIsDraggable(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 12)
	// Short session: no overflow → no auto scrollbar, preference still off.
	m.timeline = m.timeline.StartUserTurn("hi")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "ok")
	layout := m.currentLayout()
	if m.scrollbarVisible(layout) {
		t.Fatal("short session should not auto-show scrollbar")
	}

	// Overflow content so maxScroll > 0.
	for i := 0; i < 40; i++ {
		m = appendTestAssistant(m, "message line "+string(rune('a'+i%26)))
	}
	layout = m.currentLayout()
	if !m.scrollbarVisible(layout) {
		t.Fatal("overflowing transcript should auto-show scrollbar")
	}
	view := m.View()
	if !strings.Contains(ansi.Strip(view), "│") && !strings.Contains(ansi.Strip(view), "█") {
		t.Fatalf("expected scrollbar track/thumb glyphs in view:\n%s", view)
	}

	// Click top of right edge → jump toward oldest (higher scroll).
	bodyTop := 0
	rightX := m.width - 1
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: rightX, Y: bodyTop,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if !next.scrollbarDragging {
		t.Fatal("press on scrollbar should start drag")
	}
	if next.scroll <= 0 {
		t.Fatalf("click near top of track should scroll up, scroll=%d", next.scroll)
	}

	// Drag toward bottom → approach newest (scroll → 0).
	next = next.applyScrollbarDrag(layout.TimelineH - 1)
	if next.scroll != 0 {
		t.Fatalf("drag to bottom should follow newest, scroll=%d", next.scroll)
	}

	// Release ends drag.
	next, _ = next.handleMouseMsg(tea.MouseMsg{
		X: rightX, Y: layout.TimelineH - 1,
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
	})
	if next.scrollbarDragging {
		t.Fatal("release should end scrollbar drag")
	}
}

// Clicking the scrollbar gutter (including a near-miss one column left of the
// painted track) must not steal focus into scrollback — that greys the
// composer via BlurredStyle and feels like scrolling broke the input box.
func TestScrollbarInteractionKeepsComposerFocused(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 16)
	for i := 0; i < 40; i++ {
		m = appendTestAssistant(m, "line "+string(rune('a'+i%26)))
	}
	layout := m.currentLayout()
	nearX := layout.TimelineW - 2
	if m.resolveMouseHit(nearX, 0).kind != mouseHitScrollbar {
		t.Fatalf("gutter column should hit scrollbar, got kind=%d", m.resolveMouseHit(nearX, 0).kind)
	}
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: nearX, Y: 0,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if next.focus != FocusComposer || !next.composer.Input.Focused() {
		t.Fatalf("scrollbar press must keep composer focused, focus=%s focused=%v", next.focus, next.composer.Input.Focused())
	}

	// Body click also keeps composer (mouse no longer greys the prompt).
	body, _ := next.handleMouseMsg(tea.MouseMsg{
		X: 5, Y: 1,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if body.focus != FocusComposer || !body.composer.Input.Focused() {
		t.Fatalf("transcript body click must keep composer focused, focus=%s focused=%v", body.focus, body.composer.Input.Focused())
	}

	// Wheel while temporarily in scrollback should restore composer focus.
	m, _ = m.withFocus(FocusScrollback)
	if m.composer.Input.Focused() {
		t.Fatal("setup: scrollback should blur composer")
	}
	wheeled, _ := m.handleMouseMsg(tea.MouseMsg{
		X: 5, Y: 2,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	if wheeled.focus != FocusComposer || !wheeled.composer.Input.Focused() {
		t.Fatalf("wheel scroll should restore composer focus, focus=%s focused=%v", wheeled.focus, wheeled.composer.Input.Focused())
	}
	if wheeled.scroll <= 0 {
		t.Fatalf("wheel up should increase scroll, got %d", wheeled.scroll)
	}
}

// Clicking the current thumb must start a drag without jumping scroll
// (Grok pattern: track click jumps, thumb press holds).
func TestScrollbarThumbPressDoesNotJump(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 16)
	for i := 0; i < 40; i++ {
		m = appendTestAssistant(m, "line "+string(rune('a'+i%26)))
	}
	layout := m.currentLayout()
	maxScroll := m.maxScroll(layout)
	if maxScroll < 4 {
		t.Fatalf("need overflow for thumb test, maxScroll=%d", maxScroll)
	}
	// Park mid-scroll so the thumb is away from both ends.
	m.scroll = maxScroll / 2
	m.scrollPaused = true
	thumbTop, thumbSize := scrollbarMetrics(m.scroll, maxScroll, layout.TimelineH)
	if thumbSize < 1 {
		t.Fatal("expected non-empty thumb")
	}
	thumbY := thumbTop + thumbSize/2
	before := m.scroll
	rightX := m.width - 1
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: rightX, Y: thumbY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if !next.scrollbarDragging {
		t.Fatal("thumb press should start drag")
	}
	if next.scroll != before {
		t.Fatalf("thumb press must not jump scroll: before=%d after=%d", before, next.scroll)
	}
	if next.focus != FocusComposer {
		t.Fatalf("thumb press must keep composer focus, got %s", next.focus)
	}
}
