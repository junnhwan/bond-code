package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/session"
)

// TestParseFileToolInput covers the JSON extraction for write_file (content
// only) and edit_file (old + new), plus the non-mutating tool rejection.
func TestParseFileToolInput(t *testing.T) {
	path, oldS, newS, ok := parseFileToolInput("write_file", `{"path":"x.go","content":"abc"}`)
	if !ok || path != "x.go" || oldS != "" || newS != "abc" {
		t.Fatalf("write_file parse: ok=%v path=%q old=%q new=%q", ok, path, oldS, newS)
	}
	path, oldS, newS, ok = parseFileToolInput("edit_file", `{"path":"y.go","old_string":"a","new_string":"b"}`)
	if !ok || path != "y.go" || oldS != "a" || newS != "b" {
		t.Fatalf("edit_file parse: ok=%v path=%q old=%q new=%q", ok, path, oldS, newS)
	}
	if _, _, _, ok := parseFileToolInput("read_file", `{"path":"z.go"}`); ok {
		t.Fatal("expected read_file to be rejected (not a file mutation)")
	}
	if _, _, _, ok := parseFileToolInput("write_file", ``); ok {
		t.Fatal("expected empty input to parse as not-ok")
	}
}

// TestBuildDiffEntriesFromEvents checks the session-source aggregation:
// per-file grouping, first-touch ordering, count accumulation, and that
// read-only tools and failed writes are excluded.
func TestBuildDiffEntriesFromEvents(t *testing.T) {
	events := []session.Event{
		{Type: "tool", ToolCall: &session.ToolCall{Name: "write_file", Input: `{"path":"a.go","content":"hello\nworld"}`}},
		{Type: "tool", ToolCall: &session.ToolCall{Name: "edit_file", Input: `{"path":"a.go","old_string":"world","new_string":"world!\nfoo"}`}},
		{Type: "tool", ToolCall: &session.ToolCall{Name: "edit_file", Input: `{"path":"b.go","old_string":"x","new_string":"y"}`}},
		{Type: "tool", ToolCall: &session.ToolCall{Name: "read_file", Input: `{"path":"c.go"}`}},
		{Type: "tool", ToolCall: &session.ToolCall{Name: "write_file", Input: `{"path":"d.go","content":"x"}`, Error: "denied"}},
	}
	entries := buildDiffEntriesFromEvents(events)
	if len(entries) != 2 {
		t.Fatalf("expected 2 file entries (a.go, b.go), got %d: %+v", len(entries), entries)
	}
	if entries[0].path != "a.go" || len(entries[0].ops) != 2 {
		t.Fatalf("a.go should have 2 ops, got path=%q ops=%d", entries[0].path, len(entries[0].ops))
	}
	if entries[0].additions != 4 || entries[0].deletions != 1 {
		t.Fatalf("a.go counts wrong: +%d -%d (want +4 -1)", entries[0].additions, entries[0].deletions)
	}
	if entries[1].path != "b.go" || entries[1].additions != 1 || entries[1].deletions != 1 {
		t.Fatalf("b.go counts wrong: %+v", entries[1])
	}
}

// TestBuildDiffEntriesEmptyInputGuarded makes sure a malformed JSON input does
// not panic but is skipped.
func TestBuildDiffEntriesMalformedInput(t *testing.T) {
	events := []session.Event{
		{Type: "tool", ToolCall: &session.ToolCall{Name: "write_file", Input: `not json`}},
		{Type: "tool", ToolCall: &session.ToolCall{Name: "edit_file", Input: `{"old_string":"a","new_string":"b"}`}}, // no path
	}
	entries := buildDiffEntriesFromEvents(events)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries from malformed/no-path inputs, got %d", len(entries))
	}
}

// TestSplitGitDiffByFile checks the git unified-diff carving into per-file
// bodies keyed by path, without needing a real git invocation.
func TestSplitGitDiffByFile(t *testing.T) {
	raw := strings.Join([]string{
		"diff --git a/foo.go b/foo.go",
		"index abc..def 100644",
		"--- a/foo.go",
		"+++ b/foo.go",
		"@@ -1,1 +1,1 @@",
		"-old",
		"+new",
		"diff --git a/bar.go b/bar.go",
		"--- a/bar.go",
		"+++ b/bar.go",
		"@@ -1,1 +1,1 @@",
		"-x",
		"+y",
		"",
	}, "\n")
	bodies := splitGitDiffByFile(raw)
	if _, ok := bodies["foo.go"]; !ok {
		t.Fatalf("expected foo.go in carved bodies, got %v", keysOf(bodies))
	}
	if !strings.Contains(bodies["foo.go"], "+new") || !strings.Contains(bodies["foo.go"], "-old") {
		t.Fatalf("foo.go body missing diff lines: %q", bodies["foo.go"])
	}
	if _, ok := bodies["bar.go"]; !ok {
		t.Fatalf("expected bar.go in carved bodies, got %v", keysOf(bodies))
	}
	if strings.Contains(bodies["foo.go"], "diff --git") {
		t.Fatal("body should exclude the diff --git header (used as delimiter)")
	}
}

// TestDiffViewerOpensWithoutSessionController checks the viewer opens even when
// no session history is wired (fake/test mode), surfacing the reason as an
// in-view error rather than crashing.
func TestDiffViewerOpensWithoutSessionController(t *testing.T) {
	model := NewModel(Config{})
	model = model.openDiffViewer()
	if !model.overlay.active() || model.overlay.kind != overlayDiff {
		t.Fatalf("expected diff viewer overlay active, got kind=%v", model.overlay.kind)
	}
	if model.overlay.diff.loadErr == "" {
		t.Fatal("expected a load error when no session controller is configured")
	}
	// Esc closes it.
	next, _, handled := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !handled || next.overlay.active() {
		t.Fatal("expected esc to close the diff viewer")
	}
}

// TestDiffViewerSourceSwitchDoesNotCrash verifies 'd' toggles the source even
// from an errored state (git source will also likely error without a repo, but
// the switch itself must not panic).
func TestDiffViewerSourceSwitchDoesNotCrash(t *testing.T) {
	model := NewModel(Config{})
	model = model.openDiffViewer()
	if model.overlay.diff.source != diffSourceSession {
		t.Fatalf("expected default session source, got %v", model.overlay.diff.source)
	}
	next, _, handled := model.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if !handled {
		t.Fatal("expected d to be handled")
	}
	if next.overlay.diff.source != diffSourceGit {
		t.Fatalf("expected source switched to git, got %v", next.overlay.diff.source)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
