package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMouseDefaultsOnForWheelScroll(t *testing.T) {
	m := NewModel(Config{})
	// Empty Config leaves MouseCapture false as the zero value — seed the
	// runtime flag the same way cli does after resolving config.TUI.Mouse().
	// NewModel uses cfg.MouseCapture directly; production passes resolved true.
	m = NewModel(Config{MouseCapture: true})
	if !m.mouseEnabled {
		t.Fatal("with MouseCapture true, mouse must start enabled for wheel scroll")
	}
}

func TestMouseToggleViaCommand(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 24)
	if !m.mouseEnabled {
		t.Fatal("setup: mouse on")
	}

	// Toggle off for free select/copy.
	next, cmd := m.runMouseCommand(nil)
	m = next
	if m.mouseEnabled {
		t.Fatal("/mouse toggle should disable mouse")
	}
	if cmd == nil {
		t.Fatal("disabling mouse should return DisableMouse cmd")
	}

	next, cmd = m.toggleMouseCapture()
	m = next
	if !m.mouseEnabled {
		t.Fatal("toggle should re-enable mouse")
	}
	if cmd == nil {
		t.Fatal("enabling mouse should return EnableMouseCellMotion cmd")
	}

	m.mouseEnabled = true
	next, _ = m.runMouseCommand([]string{"on"})
	if !next.mouseEnabled {
		t.Fatal("already on should stay on")
	}

	next, cmd = m.runMouseCommand([]string{"off"})
	if next.mouseEnabled {
		t.Fatal("/mouse off should disable")
	}
	if cmd == nil {
		t.Fatal("/mouse off should emit disable cmd")
	}

	next, _ = m.runMouseCommand([]string{"nope"})
	last := next.timeline.Turns[len(next.timeline.Turns)-1]
	block := last.Blocks[len(last.Blocks)-1]
	if block.Kind != BlockError {
		t.Fatalf("bad arg should error, got %v", block.Kind)
	}
}

func TestMouseSlashCommandViaRunCommand(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 24)
	m, cmd := m.runCommand(m.cfg.Context, "/mouse")
	if m.mouseEnabled {
		t.Fatal("/mouse via runCommand should disable when starting on")
	}
	if cmd == nil {
		t.Fatal("expected disable cmd")
	}
}

func TestHandleMouseIgnoredWhenDisabled(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 28)
	m.mouseEnabled = false
	m.timeline = m.timeline.StartUserTurn("hi")
	before := m.scroll
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
		Y:      2,
	})
	if next.scroll != before {
		t.Fatal("wheel must not scroll when mouseEnabled is false")
	}
}
