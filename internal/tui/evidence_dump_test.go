package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// TestEvidenceViewDump writes stripped View samples for goal verification.
// Output dir: GROK_EVIDENCE_DIR or default implementer scratch path.
func TestEvidenceViewDump(t *testing.T) {
	dir := os.Getenv("GROK_EVIDENCE_DIR")
	if dir == "" {
		dir = `C:\Users\zjh\AppData\Local\Temp\grok-goal-c34a99563e6a\implementer`
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write := func(name, content string) {
		stripped := ansi.Strip(content)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(stripped), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Welcome (idle black surface).
	m := NewModel(Config{}).SetSize(72, 22)
	write("view-welcome.txt", m.View())

	// Focused vs blurred composer chrome.
	write("view-composer-focused.txt", m.composerViewForWidth(60))
	mBlur, _ := m.withFocus(FocusScrollback)
	write("view-composer-blurred.txt", mBlur.composerViewForWidth(60))

	// /theme structured panel inside a session View.
	m2 := NewModel(Config{}).SetSize(72, 24)
	m2.timeline = m2.timeline.StartUserTurn("show theme")
	m2, _ = m2.runThemeCommand(nil)
	write("view-theme-panel.txt", m2.View())
	// Also dump raw theme body for assertions.
	blocks := m2.timeline.Turns[0].Blocks
	if len(blocks) > 0 {
		write("view-theme-body.txt", blocks[len(blocks)-1].Body)
	}

	// Busy compact tool row.
	tool := &ToolBlock{
		Name:      "read_file",
		Status:    ToolRunning,
		Input:     `{"path":"internal/tui/view.go"}`,
		Collapsed: true,
		StartedAt: time.Now(),
	}
	write("view-tool-row.txt", renderToolActivity(tool, 72))

	// Folded thinking.
	write("view-thinking-folded.txt", strings.Join(renderReasoningPreviewLines(
		"line one of thought\nline two of thought\nline three", 72), "\n"))
}
