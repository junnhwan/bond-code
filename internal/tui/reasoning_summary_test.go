package tui

import (
	"strings"
	"testing"
)

func TestReasoningSummaryFirstNonEmptyLine(t *testing.T) {
	got := reasoningSummary("\n\nLet me analyze the loop.\nmore detail")
	if got != "Let me analyze the loop." {
		t.Fatalf("first non-empty line should win, got %q", got)
	}
}

func TestReasoningSummaryCapped(t *testing.T) {
	got := reasoningSummary(strings.Repeat("y", 200))
	// truncatePlain caps to the requested width (plus an ellipsis), so the
	// summary never overflows the folded header line.
	if len(got) > 64 {
		t.Fatalf("summary should be capped to ~60 chars, got %d (%q)", len(got), got)
	}
}

func TestReasoningSummaryEmpty(t *testing.T) {
	if got := reasoningSummary(""); got != "" {
		t.Fatalf("empty body should yield empty, got %q", got)
	}
	if got := reasoningSummary("\n\n  \n"); got != "" {
		t.Fatalf("all-blank body should yield empty, got %q", got)
	}
}

// TestRenderReasoningPreviewShowsSummary confirms the folded view surfaces the
// extracted summary on its header line (Phase 5E).
func TestRenderReasoningPreviewShowsSummary(t *testing.T) {
	out := renderReasoningPreview("Analyze the safety policy.\ndetail line\nmore detail", 80)
	if !strings.Contains(out, "Analyze the safety policy.") {
		t.Fatalf("folded header should surface the summary, got:\n%s", out)
	}
}
