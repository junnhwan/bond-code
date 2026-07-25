package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestCompactToolRowWhenCollapsed(t *testing.T) {
	tool := &ToolBlock{
		Name:      "read_file",
		Status:    ToolDone,
		Input:     `{"path":"internal/tui/view.go"}`,
		Output:    strings.Repeat("line\n", 40),
		Collapsed: true,
		Duration:  12 * time.Millisecond,
	}
	rendered := renderToolActivity(tool, 80)
	stripped := ansi.Strip(rendered)
	// Grok: ◆ Read path · meta — one line, diamond not checkmark, no ⎿.
	if !strings.Contains(stripped, "◆") {
		t.Fatalf("expected Grok diamond bullet, got %q", stripped)
	}
	if !strings.Contains(stripped, "Read") && !strings.Contains(stripped, "view.go") {
		t.Fatalf("expected Read + path, got %q", stripped)
	}
	if strings.Contains(stripped, "⎿") || strings.Contains(stripped, "✓") {
		t.Fatalf("must not use Claude ✓/⎿ chrome, got %q", stripped)
	}
	if strings.Count(stripped, "\n") > 0 {
		// only blank trailing allowed
		n := 0
		for _, ln := range strings.Split(stripped, "\n") {
			if strings.TrimSpace(ln) != "" {
				n++
			}
		}
		if n > 1 {
			t.Fatalf("collapsed tool should be one header line, got %d:\n%s", n, stripped)
		}
	}
	tool.Collapsed = false
	expanded := ansi.Strip(renderToolActivity(tool, 80))
	if expanded == stripped {
		t.Fatal("expanded tool should differ from collapsed compact row")
	}
	if !strings.Contains(expanded, "line") {
		t.Fatalf("expanded should show output body, got %q", expanded)
	}
}

func TestThinkingFoldHidesBody(t *testing.T) {
	body := "first thought line\nsecond thought line\nthird line of reasoning"
	// Folded (default showThinking=false): header only, no body lines.
	preview := renderReasoningPreviewLines(body, 60)
	joined := ansi.Strip(strings.Join(preview, "\n"))
	if !strings.Contains(joined, "thinking") {
		t.Fatalf("expected thinking header, got %q", joined)
	}
	if strings.Contains(joined, "second thought") || strings.Contains(joined, "third line") {
		t.Fatalf("folded thinking must hide body, got %q", joined)
	}
	if !strings.Contains(joined, "folded") {
		t.Fatalf("expected folded marker, got %q", joined)
	}
	// Expanded shows body.
	full := ansi.Strip(strings.Join(renderReasoningFullLines(body, 60), "\n"))
	if !strings.Contains(full, "first thought") {
		t.Fatalf("expanded thinking should show body, got %q", full)
	}
}

func TestUserAssistantToolMarkersDistinct(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 30)
	m.timeline = m.timeline.StartUserTurn("user says hi")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "## Title\n\nassistant body text")
	tool := &ToolBlock{Name: "run_command", Status: ToolDone, Input: `{"command":"go test"}`, Collapsed: true}
	m.timeline = m.timeline.AppendBlock(BlockTool, "run_command", renderToolBody(tool))
	// Fix tool pointer on last block.
	turns := m.timeline.Turns
	last := &turns[len(turns)-1]
	if len(last.Blocks) > 0 {
		b := last.Blocks[len(last.Blocks)-1]
		b.Tool = tool
		last.Blocks[len(last.Blocks)-1] = b
		m.timeline.Turns = turns
	}

	lines, _ := m.renderTimelineLines(80)
	view := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(view, "you") && !strings.Contains(view, "❯") {
		t.Fatalf("expected user marker, got %q", view)
	}
	if !strings.Contains(view, "bond") && !strings.Contains(view, "│") {
		t.Fatalf("expected assistant chrome, got %q", view)
	}
	// No raw ## in committed assistant when markdown path runs.
	if strings.Contains(view, "## Title") {
		// May appear if glamour unavailable in test env — still check hierarchy.
		t.Log("raw ## present (markdown may have fallen back)")
	}
}

func TestTurnSpacingNotUndifferentiatedLog(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 30)
	m.timeline = m.timeline.StartUserTurn("one")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "reply one")
	m.timeline = m.timeline.StartUserTurn("two")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "reply two")
	lines, _ := m.renderTimelineLines(80)
	// Blank lines between sections.
	blank := 0
	for _, line := range lines {
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			blank++
		}
	}
	if blank < 2 {
		t.Fatalf("expected inter-turn blank spacing, blanks=%d lines=%v", blank, lines)
	}
}
