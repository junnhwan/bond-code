package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHandleViewportMessageUsesCurrentMouseWheelAPI(t *testing.T) {
	model := NewModel(Config{MouseCapture: true}).SetSize(100, 30)
	for i := 0; i < 6; i++ {
		model.timeline = model.timeline.StartUserTurn(fmt.Sprintf("user %d", i))
		model.timeline = model.timeline.AppendBlock(BlockAssistant, "agent", strings.Repeat("reply line\n", 6))
	}

	updated, _ := model.handleViewportMessage(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	if updated.scroll == 0 {
		t.Fatal("current Bubble Tea wheel-up event did not scroll toward older messages")
	}

	updated, _ = updated.handleViewportMessage(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	if updated.scroll != 0 {
		t.Fatalf("current Bubble Tea wheel-down event left scroll=%d, want 0", updated.scroll)
	}
}
