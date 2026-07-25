package tui

import (
	"strings"
	"testing"
	"time"
)

func TestRenderToolActivityLineShowsCompactSummary(t *testing.T) {
	tool := &ToolBlock{
		Name:     "run_command",
		Status:   ToolDone,
		Risk:     "low",
		Input:    `{"command":"go test ./internal/tui"}`,
		Output:   "ok\tgithub.com/junnhwan/bond-code/internal/tui\t0.12s",
		Summary:  "go test: ok 1",
		Duration: 120 * time.Millisecond,
	}

	view := renderToolActivity(tool, 100)
	// Grok single-line header: ◆ Run <cmd> · meta (no Claude ⎿ row).
	for _, want := range []string{"◆", "Run", "go test ./internal/tui", "120ms"} {
		if !strings.Contains(view, want) {
			t.Fatalf("tool activity missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "⎿") {
		t.Fatalf("Grok tool row must not use Claude ⎿ chrome:\n%s", view)
	}
	if strings.Contains(view, `{"command"`) || strings.Contains(view, "low risk") {
		t.Fatalf("tool activity should not show raw input JSON or card metadata by default:\n%s", view)
	}
	// Collapsed default is one visual line for short tools.
	if strings.Count(strings.TrimSpace(view), "\n") > 2 {
		t.Fatalf("expected compact header-first layout, got:\n%s", view)
	}
}

func TestRenderToolActivityLineShowsCollapsedOutputHint(t *testing.T) {
	tool := &ToolBlock{
		Name:      "run_command",
		Status:    ToolDone,
		Output:    strings.Repeat("line\n", 30),
		Collapsed: true,
	}

	view := renderToolActivity(tool, 100)
	// Collapsed: single header with line count meta, no body dump.
	if !strings.Contains(view, "◆") || !strings.Contains(view, "lines") {
		t.Fatalf("expected diamond header with line-count meta, got:\n%s", view)
	}
	if strings.Contains(view, "line\nline") || strings.Count(view, "\n") > 0 && strings.Contains(view, "padding") {
		// body must not spill
	}
	if strings.Count(strings.TrimRight(view, "\n"), "\n") > 0 {
		// allow only header line when collapsed
		bodyLines := 0
		for _, ln := range strings.Split(view, "\n") {
			if strings.TrimSpace(ln) != "" {
				bodyLines++
			}
		}
		if bodyLines > 1 {
			t.Fatalf("collapsed tool should be one header line, got:\n%s", view)
		}
	}
	for _, forbidden := range []string{"⎿", "Ctrl+E", "ctrl+e", "Ctrl+X", "<leader>", "? help", "/session"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("collapsed tool leaked forbidden chrome %q:\n%s", forbidden, view)
		}
	}
}

func TestCtrlODetailsModeRevealsCollapsedToolOutput(t *testing.T) {
	previous := renderVerbose
	setRenderVerbose(true)
	defer setRenderVerbose(previous)

	tool := &ToolBlock{
		Name:      "run_command",
		Status:    ToolDone,
		Output:    "visible detail line\n" + strings.Repeat("padding line\n", 20),
		Collapsed: true,
	}
	view := renderToolActivity(tool, 100)
	if !strings.Contains(view, "visible detail line") {
		t.Fatalf("Ctrl+O details mode should reveal collapsed tool output:\n%s", view)
	}
	if strings.Contains(view, "⎿") {
		t.Fatalf("expanded details must not use Claude ⎿ chrome:\n%s", view)
	}
}

func TestRenderExpandedToolDetailsSplitsAndLimitsOutputLines(t *testing.T) {
	outputLines := make([]string, 0, 25)
	for i := 0; i < 25; i++ {
		outputLines = append(outputLines, "output line "+string(rune('a'+i)))
	}
	tool := &ToolBlock{
		Name:      "run_command",
		Status:    ToolDone,
		Output:    strings.Join(outputLines, "\n"),
		Collapsed: false,
	}

	view := renderToolActivity(tool, 120)
	// Grok expanded body hangs under ◆ without an "output:" label.
	if strings.Contains(view, "output:") {
		t.Fatalf("expanded body should not use labeled output: chrome, got:\n%s", view)
	}
	if !strings.Contains(view, "output line a") || !strings.Contains(view, "output line l") {
		t.Fatalf("expected expanded output to render individual output lines, got:\n%s", view)
	}
	if strings.Contains(view, "output line m") {
		t.Fatalf("expected expanded output to be capped before line m, got:\n%s", view)
	}
	if !strings.Contains(view, "… +") && !strings.Contains(view, "+13") {
		t.Fatalf("expected expanded output to show +N lines truncation, got:\n%s", view)
	}
}

func TestRenderToolActivityLineShowsBlockedDistinctly(t *testing.T) {
	tool := &ToolBlock{
		Name:    "run_command",
		Status:  ToolBlocked,
		Risk:    "high",
		Input:   `{"command":"rm -rf tmp"}`,
		Error:   "blocked destructive shell command",
		Summary: "blocked destructive shell command",
	}

	view := renderToolActivity(tool, 100)
	for _, want := range []string{"Run", "rm -rf tmp", "blocked"} {
		if !strings.Contains(view, want) {
			t.Fatalf("blocked activity missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, `{"command"`) || strings.Contains(view, "high risk") {
		t.Fatalf("blocked activity should stay compact by default:\n%s", view)
	}
}

func TestRenderPendingMediumConfirmationShowsActions(t *testing.T) {
	tool := &ToolBlock{
		Name:    "write_file",
		Status:  ToolPending,
		Risk:    "medium",
		Input:   `{"path":"README.md"}`,
		Summary: "will modify README.md",
	}

	view := renderToolActivity(tool, 100)
	for _, want := range []string{"◆", "Write", "README.md", "confirm", "y approve", "n reject"} {
		if !strings.Contains(view, want) {
			t.Fatalf("pending medium activity missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "⎿") {
		t.Fatalf("pending tool must not use Claude ⎿ chrome:\n%s", view)
	}
}

func TestRenderToolActivityLineUsesToolSpecificLabels(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "read_file", in: `{"path":"README.md"}`, want: "README.md"},
		{name: "write_file", in: `{"path":"internal/agent/loop.go","content":"abc"}`, want: "internal/agent/loop.go"},
		{name: "search_text", in: `{"pattern":"Agent Loop","path":"internal"}`, want: `"Agent Loop"`},
		{name: "run_command", in: `{"command":"go test ./..."}`, want: "go test ./..."},
	}

	for _, tc := range cases {
		view := renderToolActivity(&ToolBlock{Name: tc.name, Status: ToolDone, Input: tc.in}, 100)
		if !strings.Contains(view, tc.want) {
			t.Fatalf("%s activity missing %q:\n%s", tc.name, tc.want, view)
		}
		if strings.Contains(view, tc.in) {
			t.Fatalf("%s activity should not render raw input JSON:\n%s", tc.name, view)
		}
	}
}

func TestRenderToolDetailsColorCodesDiffLines(t *testing.T) {
	// Output must be long enough to be collapsible, with Collapsed=false so the
	// expanded details (which apply diff coloring) actually render.
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n+added line\n-removed line\n"
	tool := &ToolBlock{
		Name:      "run_command",
		Status:    ToolDone,
		Input:     `{"command":"git diff"}`,
		Output:    diff + strings.Repeat("padding line\n", 20),
		Collapsed: false,
	}
	view := renderToolActivity(tool, 100)
	for _, want := range []string{"added line", "removed line"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected colored diff output to keep %q:\n%s", want, view)
		}
	}
}
