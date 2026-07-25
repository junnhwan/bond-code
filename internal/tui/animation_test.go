package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestAnimSpinnerCycles(t *testing.T) {
	a := animSpinnerFrame(0)
	b := animSpinnerFrame(1)
	if a == "" || b == "" {
		t.Fatal("empty spinner frames")
	}
	// Over a full period we should see more than one distinct glyph.
	seen := map[string]bool{}
	for i := 0; i < len(brailleSpinner)*2; i++ {
		seen[animSpinnerFrame(i)] = true
	}
	if len(seen) < 4 {
		t.Fatalf("expected multiple spinner glyphs, got %d", len(seen))
	}
}

func TestFormatTurnStatusRowAnimatedHasSpinnerAndDots(t *testing.T) {
	row0 := ansi.Strip(FormatTurnStatusRowAnimated(0, "thinking", "1.2s", 80))
	row1 := ansi.Strip(FormatTurnStatusRowAnimated(1, "thinking", "1.2s", 80))
	if !strings.Contains(row0, "thinking") {
		t.Fatalf("missing activity: %q", row0)
	}
	if row0 == row1 {
		t.Fatalf("adjacent frames should differ for animation:\n%q\n%q", row0, row1)
	}
	// Trailing dots cycle.
	if !strings.Contains(row0, "thinking.") {
		t.Fatalf("expected activity dots on frame 0: %q", row0)
	}
}

func TestAnimDockSeparatorTravelsWhenBusy(t *testing.T) {
	idle := ansi.Strip(animDockSeparator(20, 0, false))
	busy0 := ansi.Strip(animDockSeparator(20, 0, true))
	busy5 := ansi.Strip(animDockSeparator(20, 5, true))
	if idle == "" || busy0 == "" {
		t.Fatal("empty separators")
	}
	// Busy frames differ as the highlight moves.
	if busy0 == busy5 {
		// Both may still be all dashes if strip removes color only — check raw.
		raw0 := animDockSeparator(20, 0, true)
		raw5 := animDockSeparator(20, 5, true)
		if raw0 == raw5 {
			t.Fatal("busy separator should move highlight across frames")
		}
	}
}

func TestUIFlashExpires(t *testing.T) {
	f := newUIFlash(mouseHitComposer)
	if !f.active() {
		t.Fatal("fresh flash should be active")
	}
	f.until = time.Now().Add(-time.Millisecond)
	if f.active() {
		t.Fatal("expired flash should be inactive")
	}
}

func TestNeedsAnimationTick(t *testing.T) {
	m := NewModel(Config{}).SetSize(40, 20)
	// Empty welcome keeps shimmer ticks (Grok logo sheen).
	if !m.needsAnimationTick() {
		t.Fatal("empty welcome should need anim ticks for logo shimmer")
	}
	// After a turn exists and idle, no tick needed.
	m.timeline = m.timeline.StartUserTurn("hi")
	if m.needsAnimationTick() {
		t.Fatal("idle non-empty model should not need anim ticks")
	}
	m.agent.Busy = true
	if !m.needsAnimationTick() {
		t.Fatal("busy should need anim ticks")
	}
	m.agent.Busy = false
	m.hover = mouseHover{kind: mouseHitComposer}
	if !m.needsAnimationTick() {
		t.Fatal("hover should need anim ticks")
	}
	m.hover = mouseHover{}
	m.flash = newUIFlash(mouseHitComposer)
	if !m.needsAnimationTick() {
		t.Fatal("flash should need anim ticks")
	}
}

func TestSpinnerTickAdvancesAnimFrame(t *testing.T) {
	m := NewModel(Config{}).SetSize(40, 20)
	m.agent.Busy = true
	frame := m.animFrame
	updated, cmd := m.Update(spinner.TickMsg{Time: time.Now(), ID: m.spinner.ID()})
	um, ok := updated.(Model)
	if !ok {
		t.Fatalf("expected Model, got %T", updated)
	}
	if um.animFrame != frame+1 {
		t.Fatalf("animFrame want %d got %d", frame+1, um.animFrame)
	}
	if cmd == nil {
		t.Fatal("busy model should keep spinning")
	}
}

// TestMouseMotionDoesNotAmplifyAnimTicks pins the busy-spinner race: every
// mouse motion used to return a fresh spinner.Tick while Busy, stacking
// concurrent timers so the thinking indicator spun faster under a moving
// pointer. Motion must not re-arm an already-running tick chain.
func TestMouseMotionDoesNotAmplifyAnimTicks(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 24)
	m.agent.Busy = true
	m.animTickArmed = true
	m.timeline = m.timeline.StartUserTurn("hi")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "ok")

	// Hover motion over the composer while busy.
	var composerY int
	for y := 0; y < m.height; y++ {
		if m.resolveMouseHit(5, y).kind == mouseHitComposer {
			composerY = y
			break
		}
	}
	next, cmd := m.handleMouseMsg(tea.MouseMsg{
		X: 5, Y: composerY,
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonNone,
	})
	if cmd != nil {
		t.Fatal("mouse motion while animTickArmed must not schedule another spinner.Tick")
	}
	if !next.animTickArmed {
		t.Fatal("busy hover should leave the existing tick chain armed")
	}

	// ensureAnimTick alone must also be a no-op when already armed.
	_, cmd = next.ensureAnimTick()
	if cmd != nil {
		t.Fatal("ensureAnimTick must not re-arm an in-flight tick chain")
	}
}


