package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
)

func TestMouseClickFocusesComposerAndScrollback(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 28)
	m.timeline = m.timeline.StartUserTurn("hello")
	m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", "world")
	m.focus = FocusScrollback

	composerY := -1
	for y := 0; y < m.height; y++ {
		if m.resolveMouseHit(5, y).kind == mouseHitComposer {
			composerY = y
			break
		}
	}
	if composerY < 0 {
		t.Fatal("composer hit band not found")
	}
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: 5, Y: composerY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if next.focus != FocusComposer {
		t.Fatalf("expected click on composer to focus composer, focus=%s", next.focus)
	}

	next, _ = next.handleMouseMsg(tea.MouseMsg{
		X: 5, Y: 2,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	// Mouse click on the transcript keeps the composer focused so the prompt
	// does not grey out (BlurredStyle). Keyboard Tab still enters scrollback.
	if next.focus != FocusComposer {
		t.Fatalf("expected click on scrollback to keep composer focus, focus=%s", next.focus)
	}
}

func TestMouseHoverComposerSetsHoverState(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 28)
	m.timeline = m.timeline.StartUserTurn("hi")
	composerY := -1
	for y := 0; y < m.height; y++ {
		if m.resolveMouseHit(3, y).kind == mouseHitComposer {
			composerY = y
			break
		}
	}
	if composerY < 0 {
		t.Fatal("composer hit band not found")
	}
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: 3, Y: composerY,
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonNone,
	})
	if next.hover.kind != mouseHitComposer {
		t.Fatalf("expected composer hover, got kind=%d", next.hover.kind)
	}
	_ = next.View() // must not panic with hover styling
}

func TestWelcomeMenuHitAndClick(t *testing.T) {
	m := NewModel(Config{
		MouseCapture: true,
		Status:       Status{ProjectRoot: "bond-code", Model: "fake"},
	}).SetSize(80, 30)
	if len(m.timeline.Turns) != 0 {
		t.Fatal("expected empty timeline")
	}

	dock := m.measureBottomDock()
	layout := CalculateLayout(m.width, m.height, dock.reservedHeight())
	left, right := welcomeMenuColumnBounds(layout.TimelineW)
	if right <= left {
		t.Fatalf("invalid menu bounds left=%d right=%d", left, right)
	}
	// Probe X must land inside the painted column (not full-row).
	menuX := left + (right-left)/2

	// Locate welcome menu hits by scanning the column center.
	var menuHits []mouseHit
	var menuYs []int
	for y := 0; y < m.height; y++ {
		hit := m.resolveMouseHit(menuX, y)
		if hit.kind == mouseHitWelcomeMenu {
			menuHits = append(menuHits, hit)
			menuYs = append(menuYs, y)
		}
	}
	if len(menuHits) < 3 {
		// Geometry fallback: pure welcome rows must still map labels.
		rows := welcomeMenuRowYs(WelcomeChromeInput{
			Width: layout.TimelineW, Height: layout.TimelineH,
			Project: "bond-code", Version: "v1.0.0",
		})
		if len(rows) < 3 {
			t.Fatalf("expected 3 menu hits or rows, hits=%d rows=%v", len(menuHits), rows)
		}
		// Absolute Y = body start (0) + row
		for _, row := range rows {
			hit := m.resolveMouseHit(menuX, row)
			if hit.kind == mouseHitWelcomeMenu {
				menuHits = append(menuHits, hit)
				menuYs = append(menuYs, row)
			}
		}
	}
	if len(menuHits) == 0 {
		t.Fatal("no welcome menu hits resolved")
	}

	// Hover second item if present.
	idx := 0
	y := menuYs[0]
	if len(menuHits) > 1 {
		idx = 1
		y = menuYs[1]
	}
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: menuX, Y: y,
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonNone,
	})
	if next.hover.kind != mouseHitWelcomeMenu {
		t.Fatalf("expected welcome menu hover, got %d", next.hover.kind)
	}
	if next.welcomeMenuActive != idx && next.hover.index != idx {
		t.Fatalf("hover index want %d active=%d hover=%d", idx, next.welcomeMenuActive, next.hover.index)
	}

	// Click fires runCommand path (unknown without registry → timeline block or no panic).
	next, _ = next.handleMouseMsg(tea.MouseMsg{
		X: menuX, Y: y,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	_ = next.View()
}

// TestWelcomeMenuHitIsColumnOnly pins that padding left/right of the centered
// menu bar is not interactive — only the painted column cells count.
func TestWelcomeMenuHitIsColumnOnly(t *testing.T) {
	m := NewModel(Config{
		MouseCapture: true,
		Status:       Status{ProjectRoot: "bond-code", Model: "fake"},
	}).SetSize(100, 30)

	dock := m.measureBottomDock()
	layout := CalculateLayout(m.width, m.height, dock.reservedHeight())
	left, right := welcomeMenuColumnBounds(layout.TimelineW)
	rows := welcomeMenuRowYs(WelcomeChromeInput{
		Width: layout.TimelineW, Height: layout.TimelineH,
		Project: "bond-code", Version: "v1.0.0", Model: "fake",
	})
	if len(rows) < 1 {
		t.Fatal("expected welcome menu rows")
	}
	y := rows[0]

	// Inside column → hit.
	inside := m.resolveMouseHit(left, y)
	if inside.kind != mouseHitWelcomeMenu {
		t.Fatalf("x=%d (column start) want welcome menu, got kind=%d", left, inside.kind)
	}
	inside = m.resolveMouseHit(right-1, y)
	if inside.kind != mouseHitWelcomeMenu {
		t.Fatalf("x=%d (column end-1) want welcome menu, got kind=%d", right-1, inside.kind)
	}

	// Outside column on the same row → not a menu hit.
	if left > 0 {
		outside := m.resolveMouseHit(left-1, y)
		if outside.kind == mouseHitWelcomeMenu {
			t.Fatalf("x=%d (left of column) must not hit welcome menu", left-1)
		}
	}
	if right < layout.TimelineW {
		outside := m.resolveMouseHit(right, y)
		if outside.kind == mouseHitWelcomeMenu {
			t.Fatalf("x=%d (right of column) must not hit welcome menu", right)
		}
	}
	// Far left gutter of a wide terminal must stay inert.
	far := m.resolveMouseHit(0, y)
	if far.kind == mouseHitWelcomeMenu {
		t.Fatal("x=0 on menu row must not hit welcome menu")
	}
	far = m.resolveMouseHit(layout.TimelineW-1, y)
	if far.kind == mouseHitWelcomeMenu {
		t.Fatal("far-right cell on menu row must not hit welcome menu")
	}
}

func TestPermissionOptionIndexAt(t *testing.T) {
	panel := renderPermissionPanel(&agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "run_command",
		Message:  "ls",
		Risk:     "medium",
	}, choiceOnce, false, "", true, 80, 0)
	h := renderedHeight(panel)
	for i := 0; i < 3; i++ {
		y := h - 1 - 3 + i
		idx, ok := permissionOptionIndexAt(panel, y, false, true)
		if !ok || idx != i {
			t.Fatalf("opt %d: ok=%v idx=%d\n%s", i, ok, idx, panel)
		}
	}
}

func TestPermissionOptionClickClearsPending(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(80, 28)
	m.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "run_command",
		Message:  "echo hi",
		Risk:     "medium",
	}
	m.agent.ConfirmChoice = choiceReject

	optY := -1
	for y := 0; y < m.height; y++ {
		hit := m.resolveMouseHit(4, y)
		if hit.kind == mouseHitPermissionOption && hit.index == 0 {
			optY = y
			break
		}
	}
	if optY < 0 {
		t.Fatal("permission option hit not found")
	}
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: 4, Y: optY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if next.agent.Pending != nil {
		t.Fatal("expected permission click to clear Pending via confirm path")
	}
}

func TestQuestionOptionIndexAt(t *testing.T) {
	q := &ask.Question{
		Prompt: "Pick one",
		Options: []ask.Option{
			{Label: "Alpha"},
			{Label: "Beta"},
			{Label: "Gamma"},
		},
	}
	panel := renderQuestionPanel(q, 0, nil, 80)
	lines := strings.Split(panel, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Beta") {
			idx, ok := questionOptionIndexAt(panel, i, q)
			if !ok || idx != 1 {
				t.Fatalf("Beta: ok=%v idx=%d line=%q", ok, idx, line)
			}
			return
		}
	}
	t.Fatalf("Beta not in panel:\n%s", panel)
}

func TestWelcomeMenuRowsMatchRenderedLabels(t *testing.T) {
	in := WelcomeChromeInput{Width: 80, Height: 24, Project: "demo", Version: "v1.0.0", ActiveMenu: 1}
	lines, rows := buildWelcomeChromeLines(in)
	items := welcomeMenuItems()
	if len(rows) != len(items) {
		t.Fatalf("rows=%v items=%d", rows, len(items))
	}
	for i, row := range rows {
		if row < 0 || row >= len(lines) {
			t.Fatalf("row %d out of range", row)
		}
		if !strings.Contains(lines[row], items[i].Label) {
			t.Fatalf("row %d %q missing label %q", row, lines[row], items[i].Label)
		}
	}
}

func TestMouseWheelStillScrolls(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(100, 30)
	for i := 0; i < 6; i++ {
		m.timeline = m.timeline.StartUserTurn(fmt.Sprintf("user %d", i))
		m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", strings.Repeat("reply line\n", 6))
	}
	updated, _ := m.handleMouseMsg(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	if updated.scroll == 0 {
		t.Fatal("wheel-up did not scroll")
	}
}

func TestMouseClickAgentStripOpensSwitcher(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(100, 30)
	m.subagentTraces["task-a"] = &AgentTrace{TaskID: "task-a", AgentType: "coder", Status: "running"}
	m.traceMembershipVersion++

	// Find agent band Y by scanning hits.
	var agentY int = -1
	for y := 0; y < m.height; y++ {
		if m.resolveMouseHit(5, y).kind == mouseHitAgentStrip {
			agentY = y
			break
		}
	}
	if agentY < 0 {
		t.Fatal("expected passive agent strip hit target")
	}
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: 5, Y: agentY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if next.focus != FocusAgentBar {
		t.Fatalf("click strip should open switcher, focus=%s", next.focus)
	}
	if next.agentBarSelected != "task-a" {
		t.Fatalf("selected=%q want task-a", next.agentBarSelected)
	}
}

func TestMouseClickAgentListOpensWindow(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(100, 30)
	m.focus = FocusAgentBar
	m.agentBarSelected = "task-a"
	m.subagentTraces["task-a"] = &AgentTrace{TaskID: "task-a", AgentType: "coder", Status: "running"}
	m.subagentTraces["task-b"] = &AgentTrace{TaskID: "task-b", AgentType: "reviewer", Status: "completed"}
	m.traceMembershipVersion++

	// List row for task-b is under pills (relY>=1). Scan for mouseHitAgentList with command task-b.
	var hitX, hitY int = -1, -1
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			hit := m.resolveMouseHit(x, y)
			if hit.kind == mouseHitAgentList && hit.command == "task-b" {
				hitX, hitY = x, y
				break
			}
		}
		if hitX >= 0 {
			break
		}
	}
	if hitX < 0 {
		t.Fatal("expected agent list hit for task-b")
	}
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: hitX, Y: hitY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if next.focus != FocusAgentWindow || next.focusedTaskID != "task-b" {
		t.Fatalf("click list should open window: focus=%s id=%q", next.focus, next.focusedTaskID)
	}
}

func TestMouseClickTimelineSubagentOpensWindow(t *testing.T) {
	m := NewModel(Config{MouseCapture: true}).SetSize(100, 30)
	m.timeline = m.timeline.StartUserTurn("delegate")
	m.timeline = m.timeline.UpsertSubagentBlock("task-x", "subagent coder", "running", "working")
	m.subagentTraces["task-x"] = &AgentTrace{TaskID: "task-x", AgentType: "coder", Status: "running"}
	m.traceMembershipVersion++

	var hitX, hitY int = -1, -1
	for y := 0; y < m.height; y++ {
		for x := 0; x < 40; x++ {
			hit := m.resolveMouseHit(x, y)
			if hit.kind == mouseHitSubagent && hit.command == "task-x" {
				hitX, hitY = x, y
				break
			}
		}
		if hitX >= 0 {
			break
		}
	}
	if hitX < 0 {
		t.Fatal("expected timeline subagent hit for task-x")
	}
	next, _ := m.handleMouseMsg(tea.MouseMsg{
		X: hitX, Y: hitY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if next.focus != FocusAgentWindow || next.focusedTaskID != "task-x" {
		t.Fatalf("click subagent row should open window: focus=%s id=%q", next.focus, next.focusedTaskID)
	}
}
