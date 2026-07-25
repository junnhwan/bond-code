package tui

import (
	"strings"
	"testing"
)

func TestFormatExitResumeHintBare(t *testing.T) {
	out := FormatExitResumeHint(ExitInfo{SessionID: "sess-abc"}, 80, "bondcode")
	want := "\nResume this session with:\n  bondcode --resume sess-abc\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestFormatExitResumeHintSkipsLocalAndEmpty(t *testing.T) {
	if got := FormatExitResumeHint(ExitInfo{SessionID: ""}, 80, "bondcode"); got != "" {
		t.Fatalf("empty session should print nothing, got %q", got)
	}
	if got := FormatExitResumeHint(ExitInfo{SessionID: "local"}, 80, "bondcode"); got != "" {
		t.Fatalf("local session should print nothing, got %q", got)
	}
}

func TestFormatExitResumeHintIncludesSummary(t *testing.T) {
	out := FormatExitResumeHint(ExitInfo{
		SessionID:    "sess-abc",
		Title:        "Fix flaky CI test",
		LastPrompt:   "make the suite deterministic",
		LastResponse: "Pinned the seed; 200 consecutive green runs.",
	}, 80, "bondcode")
	want := strings.Join([]string{
		"",
		"Fix flaky CI test",
		"> make the suite deterministic",
		"  Pinned the seed; 200 consecutive green runs.",
		"",
		"Resume this session with:",
		"  bondcode --resume sess-abc",
		"",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%q\nwant:\n%q", out, want)
	}
}

func TestFormatExitResumeHintTruncatesSummary(t *testing.T) {
	out := FormatExitResumeHint(ExitInfo{
		SessionID:    "sess-abc",
		Title:        strings.Repeat("t", 50),
		LastPrompt:   strings.Repeat("p", 50),
		LastResponse: strings.Repeat("r", 50),
	}, 20, "bondcode")
	wantTitle := truncateRunes(strings.Repeat("t", 50), 20)
	if !strings.Contains(out, wantTitle) {
		t.Fatalf("title not truncated to %q:\n%s", wantTitle, out)
	}
	if !strings.Contains(out, "bondcode --resume sess-abc") {
		t.Fatalf("missing resume command:\n%s", out)
	}
}

func TestModelExitInfoFromTimeline(t *testing.T) {
	m := NewModel(Config{Status: Status{SessionID: "sess-xyz"}})
	m.timeline = m.timeline.StartUserTurn("first question\nmore detail")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "first answer line\nsecond")
	m.timeline = m.timeline.StartUserTurn("follow up")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "final reply")

	info := m.ExitInfo()
	if info.SessionID != "sess-xyz" {
		t.Fatalf("session id = %q", info.SessionID)
	}
	if info.Title != "first question" {
		t.Fatalf("title = %q, want first user prompt line", info.Title)
	}
	if info.LastPrompt != "follow up" {
		t.Fatalf("last prompt = %q", info.LastPrompt)
	}
	if info.LastResponse != "final reply" {
		t.Fatalf("last response = %q", info.LastResponse)
	}
}
