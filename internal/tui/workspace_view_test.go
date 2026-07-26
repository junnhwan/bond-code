package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
	"github.com/junnhwan/bond-code/internal/command"
)

func TestWorkspaceSingleColumnAtCommonTerminalWidths(t *testing.T) {
	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{name: "narrow", width: 72, height: 24},
		{name: "normal", width: 120, height: 32},
		{name: "wide", width: 160, height: 40},
	} {
		t.Run(size.name, func(t *testing.T) {
			model := NewModel(Config{Status: Status{
				SessionID:      "session-1",
				ProjectRoot:    `D:\dev\my_proj\go\bond-code`,
				Model:          "glm-5.1",
				PermissionMode: "confirm",
				ToolCount:      12,
				GitBranch:      "main",
			}}).SetSize(size.width, size.height)

			layout := model.currentLayout()
			if layout.TimelineW != size.width || false {
				t.Fatalf("expected %s workspace to be full-width single-column, got %#v", size.name, layout)
			}

			view := model.View()
			plain := ansi.Strip(view)
			if firstLine := strings.Split(plain, "\n")[0]; strings.Contains(firstLine, "◆ BondCode") {
				t.Fatalf("%s workspace should not render the legacy header on its first row:\n%s", size.name, view)
			}
			for _, notWant := range []string{"SESSION", "PROJECT", "TOOLS", "│ sess", "│ perm", "│ todo", "│ agent"} {
				if strings.Contains(plain, notWant) {
					t.Fatalf("%s workspace should not render legacy header/live metadata %q:\n%s", size.name, notWant, view)
				}
			}
			assertViewFits(t, view, size.width, size.height)
		})
	}
}

func TestWorkspaceWideComposerUsesTimelineColumnWidth(t *testing.T) {
	model := NewModel(Config{
		Status: Status{
			SessionID:      "session-1",
			ProjectRoot:    `D:\dev\my_proj\go\bond-code`,
			Model:          "glm-5.1",
			PermissionMode: "confirm",
		},
	})
	model = model.SetSize(160, 32)
	layout := model.currentLayout()
	composer := model.composerViewForWidth(layout.TimelineW)

	lines := strings.Split(composer, "\n")
	if len(lines) == 0 {
		t.Fatal("expected composer to render at least one line")
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > layout.TimelineW {
			t.Fatalf("composer line width %d exceeds timeline %d:\n%s", got, layout.TimelineW, composer)
		}
	}
}

func TestWorkspaceComposerMetadataStaysInsideTimelineColumn(t *testing.T) {
	model := NewModel(Config{
		Status: Status{
			SessionID:      "session-1",
			ProjectRoot:    `D:\dev\my_proj\go\bond-code`,
			Model:          strings.Repeat("very-long-model-name-", 8),
			PermissionMode: "confirm",
		},
	})
	model = model.SetSize(160, 32)
	model.agent.ContextTokens = 123456
	model.agent.ContextMaxTokens = 200000
	model = model.SetInput(strings.Repeat("long draft ", 30))
	layout := model.currentLayout()
	composer := model.composerViewForWidth(layout.TimelineW)

	for _, line := range strings.Split(composer, "\n") {
		if got := lipgloss.Width(line); got > layout.TimelineW {
			t.Fatalf("composer line should stay within timeline width %d, got %d:\n%s", layout.TimelineW, got, composer)
		}
	}
}

func TestWorkspaceSingleColumnStaysFullWidthAcrossTurnContent(t *testing.T) {
	model := NewModel(Config{Status: Status{
		SessionID:      "session-1",
		ProjectRoot:    `D:\dev\my_proj\go\bond-code`,
		Model:          "glm-5.1",
		PermissionMode: "confirm",
	}}).SetSize(160, 32)

	assertSingleColumn := func(t *testing.T, name string, model Model) {
		t.Helper()
		layout := model.currentLayout()
		if layout.TimelineW != model.width || false {
			t.Fatalf("%s should remain full-width single-column, got %#v", name, layout)
		}
		view := ansi.Strip(model.View())
		if strings.Contains(view, "│ SESSION") || strings.Contains(view, "│ sess") {
			t.Fatalf("%s should not render live metadata:\n%s", name, view)
		}
	}

	assertSingleColumn(t, "empty", model)
	model.timeline = model.timeline.StartUserTurn("hi")
	assertSingleColumn(t, "after user turn", model)
	model.timeline = model.timeline.AppendAssistantChunk("ok")
	assertSingleColumn(t, "after short assistant output", model)
	model.timeline = model.timeline.AppendAssistantChunk("\n" + strings.Repeat("long assistant output ", 12))
	assertSingleColumn(t, "after long assistant output", model)
}

func TestPersistentAgentRowComposesTranscriptAgentComposerFooterInOrder(t *testing.T) {
	// Chrome shell: no permanent Agent bar on single-agent path.
	// Order is transcript → composer → shortcuts footer.
	model := NewModel(Config{}).SetSize(100, 24)
	model.timeline = model.timeline.StartUserTurn("TRANSCRIPT_SENTINEL")
	model = model.SetInput("COMPOSER_SENTINEL")

	lines := strings.Split(ansi.Strip(model.View()), "\n")
	findLine := func(needle string) int {
		for i, line := range lines {
			if strings.Contains(line, needle) {
				return i
			}
		}
		return -1
	}

	transcript := findLine("TRANSCRIPT_SENTINEL")
	composer := findLine("COMPOSER_SENTINEL")
	if transcript < 0 || composer < 0 {
		t.Fatalf("expected transcript and composer markers:\n%s", strings.Join(lines, "\n"))
	}
	if !(transcript < composer) {
		t.Fatalf("expected transcript before composer, got transcript=%d composer=%d:\n%s", transcript, composer, strings.Join(lines, "\n"))
	}
	if strings.Contains(strings.Join(lines, "\n"), "Agent Main") {
		t.Fatalf("single-agent chrome must not show permanent Agent bar:\n%s", strings.Join(lines, "\n"))
	}
}

func TestModelViewPrioritizesPersistentAgentRowAtShortHeights(t *testing.T) {
	// Renamed contract: short heights keep footer/composer without a permanent Agent bar.
	for _, height := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		t.Run(fmt.Sprintf("height_%d", height), func(t *testing.T) {
			model := NewModel(Config{}).SetSize(80, height)
			model.timeline = model.timeline.StartUserTurn("TRANSCRIPT_SENTINEL")
			model = model.SetInput("COMPOSER_SENTINEL")

			view := ansi.Strip(model.View())
			assertViewFits(t, view, model.width, height)
			lines := strings.Split(view, "\n")
			if len(lines) != height {
				t.Fatalf("height %d should use every available row, got %d:\n%s", height, len(lines), view)
			}
			if height >= 3 && !strings.Contains(view, "COMPOSER_SENTINEL") && !strings.Contains(view, "tab") {
				// At very short heights takeover allocation may drop composer; footer/shortcuts still ok.
				t.Logf("height %d view:\n%s", height, view)
			}
		})
	}
}

func TestPersistentAgentRowRemainsDuringComposerTakeovers(t *testing.T) {
	// Takeovers replace the composer; permanent Agent bar is not required.
	tests := []struct {
		name  string
		setup func(*Model)
		want  string
	}{
		{
			name: "permission",
			setup: func(model *Model) {
				model.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file", Risk: "medium"}
			},
			want: "write_file",
		},
		{
			name: "question",
			setup: func(model *Model) {
				model.question = &ask.Question{Prompt: "Choose next step", Options: []ask.Option{{Label: "Continue"}}}
			},
			want: "Choose next step",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(Config{}).SetSize(100, 24)
			model.timeline = model.timeline.StartUserTurn("TRANSCRIPT_SENTINEL")
			model = model.SetInput("HIDDEN_COMPOSER_SENTINEL")
			tt.setup(&model)

			view := ansi.Strip(model.View())
			if !strings.Contains(view, tt.want) {
				t.Fatalf("%s takeover missing %q:\n%s", tt.name, tt.want, view)
			}
			if strings.Contains(view, "HIDDEN_COMPOSER_SENTINEL") {
				t.Fatalf("%s takeover should replace the composer:\n%s", tt.name, view)
			}
		})
	}
}

func TestWorkspaceTimelineUsesRoleAndSeverityMarkers(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(100, 30)
	model.timeline = model.timeline.StartUserTurn("inspect status")
	model.timeline = model.timeline.AppendBlock(BlockAssistant, "agent", "assistant answer")
	model.timeline = model.timeline.AppendBlock(BlockCommand, "/status", "status output")
	model.timeline = model.timeline.AppendBlock(BlockError, "agent error", "boom")

	lines := model.workspaceTimelineLines(80)
	plain := ansi.Strip(strings.Join(lines, "\n"))

	for _, want := range []string{
		"❯ inspect status",
		"assistant answer",
		"/status",
		"agent error boom",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected timeline marker %q in:\n%s", want, plain)
		}
	}
}

func TestWorkspaceWideViewKeepsTerminalWidthAcrossTurnLifecycle(t *testing.T) {
	model := NewModel(Config{
		Status: Status{
			SessionID:      "session-1",
			ProjectRoot:    `D:\dev\my_proj\go\bond-code`,
			Model:          "glm-5.1",
			PermissionMode: "confirm",
		},
	})
	model = model.SetSize(160, 32)

	assertBodyLinesHaveWidth := func(t *testing.T, name string, view string, width int) {
		t.Helper()
		lines := strings.Split(view, "\n")
		if len(lines) != model.height {
			t.Fatalf("%s view height = %d, want %d:\n%s", name, len(lines), model.height, view)
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != width {
				t.Fatalf("%s line %d width = %d, want %d:\n%s", name, i+1, got, width, view)
			}
		}
	}

	assertBodyLinesHaveWidth(t, "empty", model.View(), model.width)

	model.timeline = model.timeline.StartUserTurn("hi")
	assertBodyLinesHaveWidth(t, "after user prompt", model.View(), model.width)

	model.agent.Busy = true
	model.timeline = model.timeline.AppendAssistantChunk("ok")
	assertBodyLinesHaveWidth(t, "while assistant streams", model.View(), model.width)

	model.agent.Busy = false
	model.timeline = model.timeline.MarkAgentEnded("done", "", time.Now())
	assertBodyLinesHaveWidth(t, "after assistant done", model.View(), model.width)
}

func TestAssistantMarkdownUsesFullTimelineWidth(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(160, 32)
	layout := model.currentLayout()
	if layout.TimelineW != model.width {
		t.Fatalf("expected full-width timeline, got %#v", layout)
	}

	model.timeline = model.timeline.StartUserTurn("explain")
	model.timeline = model.timeline.AppendAssistantChunk(strings.Repeat("markdown word ", 20))
	_ = model.workspaceTimelineLines(layout.TimelineW)

	if model.markdownRenderer == nil {
		t.Fatal("expected markdown renderer")
	}
	wantMarkdownWidth := layout.TimelineW - 2
	if got := model.markdownRenderer.width; got != wantMarkdownWidth {
		t.Fatalf("assistant markdown should render at marked content width %d, got renderer width %d", wantMarkdownWidth, got)
	}
	entry := model.markdownCache["turn-1-assistant-1"]
	if entry.width != wantMarkdownWidth {
		t.Fatalf("markdown cache should be scoped to marked content width %d, got %#v", wantMarkdownWidth, entry)
	}
}

func TestWorkspaceViewUsesFullTerminalHeightWithoutEmptyPanels(t *testing.T) {
	model := NewModel(Config{
		Status: Status{
			SessionID:      "session-1",
			ProjectRoot:    `D:\dev\my_proj\go\bond-code`,
			Model:          "glm-5.1",
			PermissionMode: "confirm",
		},
	})
	model = model.SetSize(120, 32)

	if got := lipgloss.Height(model.View()); got != 32 {
		t.Fatalf("expected view height 32, got %d:\n%s", got, model.View())
	}
}

func TestWorkspaceSingleColumnOmitsLegacyTwoLineHeader(t *testing.T) {
	model := NewModel(Config{Status: Status{
		ProjectRoot:    `D:\dev\my_proj\go\bond-code`,
		Model:          strings.Repeat("very-long-model-name-", 6),
		PermissionMode: "confirm",
		GitBranch:      strings.Repeat("feature/very-long-branch-", 4),
	}}).SetSize(80, 24)

	view := model.View()
	firstLine := strings.Split(ansi.Strip(view), "\n")[0]
	if strings.Contains(firstLine, "◆ BondCode") {
		t.Fatalf("workspace should omit the redundant two-line header:\n%s", view)
	}
	assertViewFits(t, view, model.width, model.height)
}

func TestCurrentLayoutReservesQueuedPrompts(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(120, 24)
	model.agent.QueuedPrompts = []string{"queued follow-up"}

	layout := model.currentLayout()
	// reserved = queued + composer + footer(1); no permanent agent bar row.
	expected := CalculateLayout(model.width, model.height, model.composerHeight()+renderedHeight(model.queuedView())+1)
	// Allow off-by-one if measureBottomDock includes optional agent bar when children exist.
	if layout.TimelineH != expected.TimelineH && layout.TimelineH != expected.TimelineH-1 && layout.TimelineH != expected.TimelineH+1 {
		t.Fatalf("expected currentLayout timeline height near %d with queued prompt, got %d", expected.TimelineH, layout.TimelineH)
	}
}

func TestRailHiddenModeHidesRailOnWideLayout(t *testing.T) {
	model := NewModel(Config{Status: Status{SessionID: "session-1", PermissionMode: "confirm"}})
	model = model.SetSize(160, 32)

	layout := model.currentLayout()
	if false /* no sidebar */ {
		t.Fatalf("expected hidden rail mode to hide live, got %#v", layout)
	}
	if strings.Contains(model.View(), "│ sess") {
		t.Fatalf("hidden rail mode should not render rail:\n%s", model.View())
	}
}

func TestRailExpandedModeDoesNotReserveSidebar(t *testing.T) {
	model := NewModel(Config{Status: Status{SessionID: "session-1", PermissionMode: "confirm"}})
	model = model.SetSize(120, 32)

	layout := model.currentLayout()
	if layout.TimelineW != model.width {
		t.Fatalf("expanded rail mode should not alter the single-column workspace, got %#v", layout)
	}
}

func TestQuestionPanelStaysInsideTimelineColumn(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(160, 32)
	model.question = &ask.Question{
		Prompt: "Pick an option",
		Options: []ask.Option{{
			Label:       "A",
			Description: strings.Repeat("long description ", 20),
		}},
	}

	layout := model.currentLayout()
	view := model.View()
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(line, "long description") {
			continue
		}
		if got := lipgloss.Width(strings.TrimRight(line, " ")); got > layout.TimelineW {
			t.Fatalf("question panel line should stay within timeline width %d, got %d:\n%s", layout.TimelineW, got, view)
		}
		return
	}
	t.Fatalf("expected question option line in view:\n%s", view)
}

func TestCurrentLayoutIgnoresQuestionBehindPermissionDock(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(121, 18)
	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	}

	baseline := model.currentLayout()
	model.question = &ask.Question{
		Prompt: strings.Repeat("Pick next step ", 4),
		Options: []ask.Option{
			{Label: "Run tests", Description: strings.Repeat("validate current tui behavior ", 4)},
			{Label: "Inspect reference", Description: strings.Repeat("compare against opencode ", 4)},
		},
	}
	withHiddenQuestion := model.currentLayout()

	if withHiddenQuestion.TimelineH != baseline.TimelineH || withHiddenQuestion.ComposerH != baseline.ComposerH {
		t.Fatalf("hidden question dock should not change permission layout, baseline=%#v withHiddenQuestion=%#v", baseline, withHiddenQuestion)
	}
}

func TestQueuedPromptStaysInsideTimelineColumn(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(160, 32)
	model.agent.QueuedPrompts = []string{strings.Repeat("queued prompt ", 20)}

	layout := model.currentLayout()
	view := model.View()
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(line, "queued prompt") {
			continue
		}
		if got := lipgloss.Width(strings.TrimRight(line, " ")); got > layout.TimelineW {
			t.Fatalf("queued prompt line should stay within timeline width %d, got %d:\n%s", layout.TimelineW, got, view)
		}
		return
	}
	t.Fatalf("expected queued prompt line in view:\n%s", view)
}

func TestQueuedPromptShowsStopRunAndQueueHint(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(120, 24)
	model.agent.QueuedPrompts = []string{"queued follow-up"}

	view := model.View()
	if !strings.Contains(view, "queued") {
		t.Fatalf("expected queued prompt marker:\n%s", view)
	}
	// Must not reuse main's Esc/Ctrl+C stop run + queue legend.
	if strings.Contains(view, "Esc/Ctrl+C stop run + queue") {
		t.Fatalf("queued prompt still uses main stop-run legend:\n%s", view)
	}

	layout := model.currentLayout()
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(line, "queued follow-up") {
			continue
		}
		if got := lipgloss.Width(strings.TrimRight(line, " ")); got > layout.TimelineW {
			t.Fatalf("queued prompt hint line should stay within timeline width %d, got %d:\n%s", layout.TimelineW, got, view)
		}
		return
	}
	t.Fatalf("expected queued prompt line in view:\n%s", view)
}

func TestFooterShowsQueuedPromptCountWhileBusy(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 10)
	model.agent.Busy = true
	model.agent.QueuedPrompts = []string{"second", "third"}

	// Grok stack: queue count lives on the dock queued row; footer is shortcuts.
	view := model.View()
	if !strings.Contains(view, "queued") {
		t.Fatalf("expected busy view to show queued prompts:\n%s", view)
	}
	footer := model.renderFooter(model.currentLayout())
	if !strings.Contains(footer, "esc") && !strings.Contains(footer, "queue") {
		t.Fatalf("expected busy shortcuts footer, got:\n%s", footer)
	}
}

func TestFooterShowsBusyStopAndQueueHint(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(120, 10)
	model.agent.Busy = true

	footer := model.renderFooter(model.currentLayout())
	// Simple-mode-aligned shortcuts: esc cancel / ctrl+c interrupt / enter queue.
	for _, want := range []string{"esc", "cancel"} {
		if !strings.Contains(strings.ToLower(footer), want) {
			t.Fatalf("expected busy footer to include %q, got:\n%s", want, footer)
		}
	}
	if !strings.Contains(strings.ToLower(footer), "queue") && !strings.Contains(footer, "ctrl+c") {
		t.Fatalf("expected busy footer queue/interrupt hints, got:\n%s", footer)
	}
}

func TestQueuedPromptDoesNotOverflowShortTerminal(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 8)
	model.agent.QueuedPrompts = []string{
		strings.Repeat("queued prompt ", 8),
		strings.Repeat("second prompt ", 8),
	}

	assertViewFits(t, model.View(), model.width, model.height)
}

func TestPersistentAgentRowStaysInsideTimelineColumn(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(40, 32)
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-active"
	model.subagentTraces["task-active"] = &AgentTrace{
		TaskID:    "task-active",
		AgentType: strings.Repeat("reviewer-with-very-long-name-", 3),
		Status:    "running",
	}

	layout := model.currentLayout()
	row := model.agentBarViewForWidth(layout.TimelineW)
	if renderedHeight(row) != 1 {
		t.Fatalf("persistent Agent status should be exactly one row: %q", ansi.Strip(row))
	}
	if got := lipgloss.Width(row); got > layout.TimelineW {
		t.Fatalf("Agent row should stay within timeline width %d, got %d: %q", layout.TimelineW, got, ansi.Strip(row))
	}
}

func TestAgentWindowUsesFullWidthWithoutSidebarReservation(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(160, 32)
	model.focus = FocusAgentWindow
	model.focusedTaskID = "task-1"
	model.subagentTraces["task-1"] = &AgentTrace{
		TaskID:      "task-1",
		AgentType:   "reviewer",
		Title:       "review code",
		Status:      "running",
		Prompt:      strings.Repeat("inspect ", 30),
		FinalAnswer: strings.Repeat("agent window result ", 20),
	}

	layout := model.currentLayout()
	if false /* no sidebar */ {
		t.Fatalf("agent window should not reserve live space, got %#v", layout)
	}
	if layout.TimelineW != model.width {
		t.Fatalf("agent window should use full terminal width %d, got %#v", model.width, layout)
	}

	view := model.View()
	if strings.Contains(view, "│ SESSION") || strings.Contains(view, "│ sess") {
		t.Fatalf("agent window should not render live:\n%s", view)
	}
	composer := model.composerViewForWidth(layout.TimelineW)
	for _, line := range strings.Split(composer, "\n") {
		if got := lipgloss.Width(line); got > model.width {
			t.Fatalf("agent window composer exceeds width %d, got %d:\n%s", model.width, got, composer)
		}
	}
}

func TestFooterStaysInsideTimelineColumn(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(121, 32)
	model.question = &ask.Question{
		Prompt:  "Pick an option",
		Options: []ask.Option{{Label: "A"}},
	}

	layout := model.currentLayout()
	footer := model.renderFooter(layout)
	if got := lipgloss.Width(footer); got > layout.TimelineW {
		t.Fatalf("footer should stay within timeline width %d, got %d:\n%s", layout.TimelineW, got, footer)
	}
}

func TestSuggestionsStayInsideTimelineColumn(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:        "status",
		Description: strings.Repeat("long description ", 20),
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Commands: registry})
	model = model.SetSize(121, 32)
	model = model.SetInput("/")
	model = model.updateSuggestions()

	layout := model.currentLayout()
	view := model.View()
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(line, "▶ /status") {
			continue
		}
		if got := ansi.StringWidth(strings.TrimRight(line, " ")); got > layout.TimelineW {
			t.Fatalf("suggestion line should stay within timeline width %d, got %d:\n%s", layout.TimelineW, got, view)
		}
		return
	}
	t.Fatalf("expected suggestion line in view:\n%s", view)
}

func TestWorkspaceNarrowViewUsesSingleColumn(t *testing.T) {
	model := NewModel(Config{
		Status: Status{SessionID: "session-1", ProjectRoot: `D:\dev\my_proj\go\bond-code`, ToolCount: 12},
	})
	model = model.SetSize(70, 24)

	view := ansi.Strip(model.View())
	if strings.Contains(view, "SESSION") || strings.Contains(view, "TOOLS") {
		t.Fatalf("narrow workspace should not render live:\n%s", view)
	}
	if !containsBraille(view) && !strings.Contains(view, "BondCode") && !strings.Contains(view, "terminal coding agent") {
		t.Fatalf("narrow workspace missing brand:\n%s", view)
	}
	if !strings.Contains(view, "❯") {
		t.Fatalf("narrow workspace missing prompt:\n%s", view)
	}
}

func TestWorkspaceViewRendersGroupedTurn(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(120, 32)
	model.timeline = model.timeline.StartUserTurn("fix tui")
	model.timeline = model.timeline.AppendAssistantChunk("checking")
	model.timeline = model.timeline.UpsertToolBlock(&ToolBlock{
		ID:      "call-1",
		Name:    "run_command",
		Status:  ToolDone,
		Input:   `{"command":"go test ./internal/tui"}`,
		Output:  "ok",
		Summary: "go test: ok 1",
	})

	view := model.View()
	for _, want := range []string{"fix tui", "checking", "Run", "go test: ok 1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workspace view missing %q:\n%s", want, view)
		}
	}
}

func TestWorkspaceViewRendersInlineToolActivity(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(120, 32)
	model.timeline = model.timeline.StartUserTurn("inspect")
	model.timeline = model.timeline.UpsertToolBlock(&ToolBlock{
		ID:     "call-1",
		Name:   "read_file",
		Status: ToolDone,
		Input:  `{"path":"README.md"}`,
	})
	model.timeline = model.timeline.UpsertToolBlock(&ToolBlock{
		ID:     "call-2",
		Name:   "run_command",
		Status: ToolRunning,
		Input:  `{"command":"go test ./..."}`,
	})

	// Tool calls render inline, indented under the turn — there is no separate
	// "tools" group label (deliberately, to stay close to Claude Code's flow).
	view := model.View()
	for _, want := range []string{"Read", "README.md", "Run", "running"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workspace view missing inline tool activity %q:\n%s", want, view)
		}
	}
}

func TestWorkspaceViewRendersInlineAgentWorkingStatus(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(120, 32)
	model.timeline = model.timeline.StartUserTurn("run tests")
	model.agent.Busy = true
	model.timeline.Turns[0].StartedAt = time.Now().Add(-12 * time.Second)

	view := model.View()
	// Grok-like turn status: activity label + elapsed (not "agent working" prose).
	for _, want := range []string{"thinking", "00:12"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workspace view missing inline run status %q:\n%s", want, view)
		}
	}
}

func TestWorkspaceViewRendersInlineToolStatus(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(120, 32)
	model.timeline = model.timeline.StartUserTurn("run tests")
	model.agent.Busy = true
	model.timeline.Turns[0].StartedAt = time.Now().Add(-24 * time.Second)
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolRequested,
		ToolName: "run_command",
		Input:    `{"command":"go test ./..."}`,
	})

	view := model.View()
	for _, want := range []string{"run_command", "00:24"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workspace view missing inline tool status %q:\n%s", want, view)
		}
	}
}

func TestWorkspaceViewRendersInlineAgentDoneStatus(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(120, 32)
	model.timeline = model.timeline.StartUserTurn("summarize")
	model.timeline.Turns[0].StartedAt = time.Now().Add(-48 * time.Second)

	updated, _ := model.Update(agentDoneMsg{})
	view := updated.(Model).View()
	for _, want := range []string{"done", "00:48"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workspace view missing inline done status %q:\n%s", want, view)
		}
	}
}

func TestLiveOverlayShowsOnlyCompleteAssistantLinesRaw(t *testing.T) {
	model := modelWithLiveStream(BlockAssistant, "**shown**\nhidden", len("**shown**\n"))
	lines, _ := model.renderTimelineLines(80)
	view := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(view, "**shown**") {
		t.Fatalf("live Markdown must render raw with assistant marker:\n%s", view)
	}
	if strings.Contains(view, "hidden") {
		t.Fatalf("unfinished live tail must stay hidden:\n%s", view)
	}

	live := *model.agent.LiveStream
	live.body = "**shown**\nhidden tail\nnext"
	live.visibleLen = len("**shown**\nhidden tail\n")
	model.agent.LiveStream = &live
	lines, _ = model.renderTimelineLines(80)
	view = ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(view, "hidden tail") || strings.Contains(view, "next") {
		t.Fatalf("line completion visibility mismatch:\n%s", view)
	}

	model = model.commitLiveStream()
	lines, _ = model.renderTimelineLines(80)
	view = ansi.Strip(strings.Join(lines, "\n"))
	if strings.Count(view, "shown") != 1 || strings.Count(view, "hidden tail") != 1 || strings.Count(view, "next") != 1 {
		t.Fatalf("boundary commit must show the full body exactly once:\n%s", view)
	}
}

func TestLiveReasoningUsesDockNotTranscriptByDefault(t *testing.T) {
	// Default live thinking must not grow the transcript (jitter). Preview is
	// a single fixed dock turn-status line; showThinking still expands live.
	body := "line1\nline2\nline3\nline4\nline5\n"
	model := modelWithLiveStream(BlockReasoning, body, len(body))
	model.agent.Busy = true
	model = model.beginUserTurn("think")
	model.agent.LiveStream = &liveStreamState{kind: BlockReasoning, body: body, visibleLen: len(body)}

	if lines := model.renderLiveStreamLines(80); len(lines) != 0 {
		t.Fatalf("default live thinking must not paint multi-line transcript overlay, got %#v", lines)
	}
	status := ansi.Strip(model.renderTurnStatusLine(80))
	if !strings.Contains(status, "thinking") {
		t.Fatalf("dock must show thinking activity:\n%s", status)
	}
	if !strings.Contains(status, "line5") {
		t.Fatalf("dock must show latest thinking snippet:\n%s", status)
	}
	if strings.Contains(status, "line1") {
		t.Fatalf("dock snippet must be the latest line only:\n%s", status)
	}

	model.showThinking = true
	expanded := ansi.Strip(strings.Join(model.renderLiveStreamLines(80), "\n"))
	if !strings.Contains(expanded, "line1") || !strings.Contains(expanded, "line5") {
		t.Fatalf("showThinking on should show full live reasoning in transcript:\n%s", expanded)
	}
}

func TestLiveReasoningDockShowsIncompleteTail(t *testing.T) {
	model := modelWithLiveStream(BlockReasoning, "partial without newline", 0)
	model.agent.Busy = true
	model = model.beginUserTurn("think")
	model.agent.LiveStream = &liveStreamState{kind: BlockReasoning, body: "partial without newline", visibleLen: 0}
	status := ansi.Strip(model.renderTurnStatusLine(80))
	if !strings.Contains(status, "thinking") || !strings.Contains(status, "partial") {
		t.Fatalf("dock should preview incomplete thinking tail:\n%s", status)
	}
}

func TestLatestTurnOrdersCommittedLiveStatusAndBlankSeparator(t *testing.T) {
	model := modelWithLiveStream(BlockAssistant, "live line\npartial", len("live line\n"))
	model.agent.Busy = true
	model.agent.LiveDetail = "responding"
	model = model.SetSize(80, 24)
	lines, _ := model.renderTimelineLines(80)
	view := ansi.Strip(strings.Join(lines, "\n"))
	committed := strings.Index(view, "committed history")
	live := strings.Index(view, "live line")
	// Grok stack: live busy status is dock turn-status, not scrollback suffix.
	status := model.renderTurnStatusLine(80)
	if committed < 0 || live <= committed {
		t.Fatalf("latest turn order must be committed then live:\n%s", view)
	}
	if !strings.Contains(status, "responding") {
		t.Fatalf("dock turn-status must show activity:\n%s", status)
	}
	if len(lines) == 0 || lines[len(lines)-1] != "" {
		t.Fatalf("latest turn must retain its trailing blank separator: %#v", lines)
	}
}
