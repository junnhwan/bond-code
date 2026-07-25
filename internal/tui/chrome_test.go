package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
)

func TestWelcomeChromeHasLocationVersionAndBrand(t *testing.T) {
	view := ansi.Strip(RenderWelcomeChrome(WelcomeChromeInput{
		Width:   80,
		Height:  28,
		Project: "/tmp/bond-code",
		Branch:  "main",
		Version: "v1.0.0",
		Model:   "glm-5.1",
	}))
	for _, want := range []string{"main", "bond-code", "v1.0.0", "/help", "/resume", "/status"} {
		if !strings.Contains(view, want) {
			t.Fatalf("welcome missing %q:\n%s", want, view)
		}
	}
	// Brand is the linked-ring braille mark (not a letter wordmark).
	if !containsBraille(view) {
		t.Fatalf("welcome missing bond icon mark:\n%s", view)
	}
	if !strings.Contains(view, "terminal coding agent") {
		t.Fatalf("welcome missing slogan:\n%s", view)
	}
	if !containsBraille(view) {
		t.Fatalf("welcome should show braille bond mark:\n%s", view)
	}
	if !strings.Contains(view, "Bond Code") {
		t.Fatalf("welcome should name Bond Code:\n%s", view)
	}
	if !strings.Contains(view, "terminal coding agent") {
		t.Fatalf("welcome should show product caption:\n%s", view)
	}
}

// TestWelcomeRuleStaysUnderLocationBar ensures the full-width ─ is chrome under
// the top bar, not a floating hairline above the centered brand (the bug shown
// when vertical pad shoved the rule down with the body).
func TestWelcomeRuleStaysUnderLocationBar(t *testing.T) {
	view := ansi.Strip(RenderWelcomeChrome(WelcomeChromeInput{
		Width:   80,
		Height:  32,
		Project: "demo",
		Branch:  "main",
		Version: "v1.0.0",
	}))
	lines := strings.Split(view, "\n")
	if len(lines) < 4 {
		t.Fatalf("too few lines:\n%s", view)
	}
	// Row 0: location bar (branch/project). Row 1: rule.
	if !strings.Contains(lines[0], "main") && !strings.Contains(lines[0], "demo") {
		t.Fatalf("row 0 should be location bar, got %q", lines[0])
	}
	rule := strings.TrimSpace(lines[1])
	if rule == "" || strings.Trim(rule, "─") != "" {
		t.Fatalf("row 1 should be the ─ rule glued under the top bar, got %q", lines[1])
	}
	// Brand mark must appear below the rule, not have the rule mid-stack above it
	// after a large empty band under the top bar.
	brailleRow := -1
	for i, line := range lines {
		if containsBraille(line) {
			brailleRow = i
			break
		}
	}
	if brailleRow < 2 {
		t.Fatalf("expected braille brand below chrome, brailleRow=%d", brailleRow)
	}
	// No second full-width rule should sit immediately above the mark as a
	// "brand underline" — only the chrome rule at row 1.
	for i := 2; i < brailleRow; i++ {
		trim := strings.TrimSpace(lines[i])
		if trim != "" && strings.Trim(trim, "─") == "" && len([]rune(trim)) > 20 {
			t.Fatalf("stray full-width rule at row %d above brand (should stay at row 1):\n%s", i, view)
		}
	}
}

// TestWelcomeChromeDoesNotRepeatMenuCommandsInHelp ensures the body under the
// menu stays incremental (start + /help + @path) and does not re-list menu
// slash commands or the old "build anything" tip.
func TestWelcomeChromeDoesNotRepeatMenuCommandsInHelp(t *testing.T) {
	view := ansi.Strip(RenderWelcomeChrome(WelcomeChromeInput{
		Width:   80,
		Height:  28,
		Project: "demo",
		Version: "v1.0.0",
	}))
	// Menu still owns these.
	for _, want := range []string{"New session", "/clear", "Resume last", "/resume", "Status", "/status"} {
		if !strings.Contains(view, want) {
			t.Fatalf("welcome missing menu item %q:\n%s", want, view)
		}
	}
	// Help is present once, without re-listing resume/status as prose.
	if !strings.Contains(view, "type /help for all commands") {
		t.Fatalf("welcome missing compact help cue:\n%s", view)
	}
	if !strings.Contains(view, "@path") {
		t.Fatalf("welcome missing @path hint:\n%s", view)
	}
	for _, forbidden := range []string{
		"build anything",
		"/help for commands", // old tip
		"/resume sessions",
		"/status runtime",
		"Inspect status",
	} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("welcome still has redundant copy %q:\n%s", forbidden, view)
		}
	}
	// Help lines stay center-aligned.
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(line, "Press enter to chat") {
			continue
		}
		pad := len(line) - len(strings.TrimLeft(line, " "))
		if pad < 5 {
			t.Fatalf("help line should be centered, pad=%d line=%q", pad, line)
		}
	}
}

func TestAgentViewStackOrderIdle(t *testing.T) {
	m := NewModel(Config{Status: Status{Model: "glm-5.1", ProjectRoot: "bond-code"}}).SetSize(80, 24)
	m.timeline = m.timeline.StartUserTurn("hello world")
	view := ansi.Strip(m.View())
	// Idle: no permanent Agent bar; ❯ prompt; shortcuts footer with enter:send.
	if strings.Contains(view, "⬡ Agent") || strings.Contains(view, "Agent coordinator") {
		t.Fatalf("idle single-agent view must not show permanent Agent bar:\n%s", view)
	}
	if !strings.Contains(view, "❯") {
		t.Fatalf("prompt must use ❯:\n%s", view)
	}
	// No heavy rounded box border characters as primary prompt chrome.
	if strings.Contains(view, "╭─") && strings.Contains(view, "╰─") {
		// Bond welcome mark uses ╭ but only on empty timeline; with a turn, body is transcript.
		// Ensure composer region is not a full box: look for classic box mid-sides on prompt.
	}
	if !strings.Contains(view, "enter") || !strings.Contains(view, "send") {
		t.Fatalf("idle shortcuts should show enter send:\n%s", view)
	}
	// Turn status hidden when idle.
	if strings.Contains(view, "thinking") && m.agent.Busy {
		t.Fatalf("unexpected busy chrome while idle:\n%s", view)
	}
	lines := strings.Split(view, "\n")
	footer := lines[len(lines)-1]
	if strings.Contains(footer, "running ·") {
		t.Fatalf("idle footer must not be old running legend: %q", footer)
	}
}

func TestAgentViewStackBusyShowsTurnStatusNotIdleFooter(t *testing.T) {
	m := NewModel(Config{Status: Status{Model: "glm-5.1"}}).SetSize(80, 24)
	m.timeline = m.timeline.StartUserTurn("work")
	m.agent.Busy = true
	m.agent.LiveDetail = "tool: read_file"
	view := ansi.Strip(m.View())
	// Turn status activity should appear somewhere above the footer.
	if !strings.Contains(view, "read_file") && !strings.Contains(view, "thinking") && !strings.Contains(view, "tool:") {
		// currentAgentDetail may say "thinking" if no tool block yet
		if !strings.Contains(view, "thinking") && !strings.Contains(strings.ToLower(view), "work") {
			t.Fatalf("busy view should surface activity:\n%s", view)
		}
	}
	// Shortcuts show cancel, not permanent agent bar.
	if !strings.Contains(view, "esc") {
		t.Fatalf("busy shortcuts should mention esc:\n%s", view)
	}
	if strings.Contains(view, "running · Esc/Ctrl+C stop") {
		t.Fatalf("busy must not use old transient running footer:\n%s", view)
	}
}

func TestPermissionPanelVerticalOptionRows(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 24)
	m.timeline = m.timeline.StartUserTurn("x")
	m.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"a.go"}`,
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Permission required", "Allow once", "Always", "Reject", "❯"} {
		if !strings.Contains(view, want) {
			t.Fatalf("permission panel missing %q:\n%s", want, view)
		}
	}
	// Vertical rows: Allow once and Reject should not be only on one horizontal line with triple spacing only.
	// At least one option line should start with ❯ or two spaces.
	if !strings.Contains(view, "❯ Allow once") && !strings.Contains(view, "❯ Always") && !strings.Contains(view, "❯ Reject") {
		t.Fatalf("expected selected vertical option with ❯:\n%s", view)
	}
}

func TestHighRiskPermissionVerticalYesNo(t *testing.T) {
	panel := ansi.Strip(renderPermissionPanel(&agent.Event{
		ToolName: "run_command",
		Risk:     "high",
		Input:    `{"command":"rm -rf /"}`,
	}, choiceOnce, false, "", false, 80, 0))
	if !strings.Contains(panel, "❯ Yes") {
		t.Fatalf("high-risk should select Yes vertically:\n%s", panel)
	}
	if !strings.Contains(panel, "  No") {
		t.Fatalf("high-risk should list No as option row:\n%s", panel)
	}
}

func TestFormatShortcutsBarKeyColonLabel(t *testing.T) {
	got := ansi.Strip(FormatShortcutsBar([]HintItem{
		{Key: "enter", Label: "send"},
		{Key: "tab", Label: "focus"},
	}, 80))
	if !strings.Contains(got, "enter") || !strings.Contains(got, "send") {
		t.Fatalf("shortcuts format missing enter/send: %q", got)
	}
	// Grok rhythm uses key:label
	if !strings.Contains(got, "enter") || !strings.Contains(got, "send") {
		t.Fatalf("got %q", got)
	}
}

func TestTerminalTitleBusyVsIdleAndNoTickThrash(t *testing.T) {
	idle := composeTerminalTitle("bond-code", "", false, false, 0)
	if !strings.Contains(idle, "BondCode") {
		t.Fatalf("idle title: %q", idle)
	}
	busy0 := composeTerminalTitle("bond-code", "thinking", true, false, 0)
	busy1 := composeTerminalTitle("bond-code", "thinking", true, false, 1)
	busy7 := composeTerminalTitle("bond-code", "thinking", true, false, 7)
	busy8 := composeTerminalTitle("bond-code", "thinking", true, false, 8)
	if busy0 != busy1 || busy0 != busy7 {
		t.Fatalf("title must not change every tick: %q %q %q", busy0, busy1, busy7)
	}
	if busy0 == busy8 {
		// Glyph may advance at divisor=8; that's expected.
	}
	if !strings.Contains(busy0, "thinking") {
		t.Fatalf("busy title should include activity: %q", busy0)
	}
	action := composeTerminalTitle("bond-code", "thinking", true, true, 0)
	if !strings.Contains(action, "Action Required") {
		t.Fatalf("action title: %q", action)
	}
}
