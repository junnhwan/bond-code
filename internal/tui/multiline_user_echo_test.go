package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Multiline user prompts must keep every line in the transcript. Regression for
// storing renderUserEcho's joined string as one timeline row: height/scroll
// math then treated it as a single line, and fitBodyWindow clipped to the last
// visual line only.
func TestMultilineUserPromptRendersAllLines(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.height = 30
	m.timeline = m.timeline.StartUserTurn("first line\nsecond line\nthird line")

	// Timeline line list must expand one row per visual line (no embedded \n).
	lines := m.workspaceTimelineLines(80)
	var userRows []string
	for _, line := range lines {
		if strings.Contains(line, "\n") {
			t.Fatalf("timeline row must be a single visual line, got embedded newline: %q", ansi.Strip(line))
		}
		plain := strings.TrimSpace(ansi.Strip(line))
		if plain == "" {
			continue
		}
		userRows = append(userRows, plain)
	}
	if len(userRows) < 3 {
		t.Fatalf("expected at least 3 user rows, got %d: %#v", len(userRows), userRows)
	}
	joined := strings.Join(userRows, "\n")
	for _, want := range []string{"first line", "second line", "third line"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("timeline missing %q in %#v", want, userRows)
		}
	}

	// Full View path (including fitBodyWindow) must not drop earlier lines.
	view := ansi.Strip(m.View())
	for _, want := range []string{"first line", "second line", "third line"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}
