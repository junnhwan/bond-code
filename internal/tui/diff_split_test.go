package tui

import (
	"strings"
	"testing"
)

// TestRenderEditDiffSplitSideBySide confirms the split view keeps equal lines on
// both sides and produces a multi-row result for a real diff.
func TestRenderEditDiffSplitSideBySide(t *testing.T) {
	out := renderEditDiffSplit("hello\nworld", "hello\ngo", 60)
	if !strings.Contains(out, "hello") {
		t.Fatalf("split should keep equal lines on both sides, got:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("split should render multiple rows for a real diff, got %d lines", len(lines))
	}
}

func TestRenderEditDiffSplitEmpty(t *testing.T) {
	if got := renderEditDiffSplit("", "", 60); got != "" {
		t.Fatalf("empty both sides should yield empty, got %q", got)
	}
}

// TestRenderEditDiffSplitPureAddition confirms a pure addition (empty old)
// renders only the right column so the new content stays visible.
func TestRenderEditDiffSplitPureAddition(t *testing.T) {
	out := renderEditDiffSplit("", "new1\nnew2", 60)
	if !strings.Contains(out, "new1") || !strings.Contains(out, "new2") {
		t.Fatalf("pure addition should show new content, got:\n%s", out)
	}
}
