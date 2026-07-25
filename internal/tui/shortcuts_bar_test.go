package tui

import (
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
)

func TestShortcutsBarIdlePrompt(t *testing.T) {
	m := NewModel(Config{})
	got := m.shortcutsBarLine(80)
	if !strings.Contains(got, "tab") {
		t.Fatalf("idle hints should mention tab, got %q", got)
	}
}

func TestShortcutsBarBusyShowsCancel(t *testing.T) {
	m := NewModel(Config{})
	m.agent.Busy = true
	got := m.shortcutsBarLine(80)
	if !strings.Contains(strings.ToLower(got), "esc") {
		t.Fatalf("busy hints should mention esc cancel, got %q", got)
	}
}

func TestShortcutsBarPermission(t *testing.T) {
	m := NewModel(Config{})
	m.agent.Pending = &agent.Event{ToolName: "write_file", Risk: "medium"}
	got := m.shortcutsBarLine(80)
	if !strings.Contains(got, "allow") {
		t.Fatalf("permission hints should mention allow, got %q", got)
	}
}

func TestTurnStatusHiddenWhenIdle(t *testing.T) {
	m := NewModel(Config{})
	if got := m.renderTurnStatusLine(80); got != "" {
		t.Fatalf("idle must render empty, got %q", got)
	}
}

func TestTurnStatusShowsWhenBusy(t *testing.T) {
	m := NewModel(Config{})
	m.agent.Busy = true
	m.timeline = m.timeline.StartUserTurn("x")
	got := m.renderTurnStatusLine(80)
	if got == "" {
		t.Fatal("expected turn status while busy")
	}
}
