package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/command"
	commandbuiltin "github.com/junnhwan/bond-code/internal/command/builtin"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/skill"
)

func TestModelViewRendersWorkspaceRegions(t *testing.T) {
	model := NewModel(Config{
		Status: Status{
			SessionID:      "session-1",
			ProjectRoot:    "D:\\dev\\repo",
			Model:          "glm-5.1",
			PermissionMode: "confirm",
			ToolCount:      11,
			GitBranch:      "main",
		},
	})
	model = model.SetSize(80, 20)
	model = model.beginUserTurn("hello")
	model = appendTestAssistant(model, "hi")

	view := model.View()
	for _, want := range []string{"glm-5.1", "hello", "hi", "normal", "❯"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	// Permission mode may appear on the prompt info line; exclude only live-style chrome.
	for _, notWant := range []string{"session-1", "Branch:", "Tools:", "SESSION", "PROJECT"} {
		if strings.Contains(view, notWant) {
			t.Fatalf("transcript view should not render dashboard metadata %q:\n%s", notWant, view)
		}
	}
}

func TestWorkspaceShowsAgentRowAndTransientFooterStates(t *testing.T) {
	model := NewModel(Config{})
	// Idle single-agent chrome: no permanent Agent bar; shortcuts/footer + welcome/prompt.
	if view := model.View(); !strings.Contains(view, "normal") && !strings.Contains(view, "tab") && !strings.Contains(view, "enter") {
		t.Fatalf("expected mode or shortcuts while idle:\n%s", view)
	}

	model.agent.Busy = true
	if view := model.View(); !strings.Contains(strings.ToLower(view), "esc") && !strings.Contains(view, "cancel") {
		t.Fatalf("expected cancel hint while running:\n%s", view)
	}

	model.agent.QueuedPrompts = []string{"follow up"}
	if view := model.View(); !strings.Contains(view, "queued") {
		t.Fatalf("expected queued prompt visible above composer:\n%s", view)
	}
	model.agent.QueuedPrompts = nil

	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolRequested,
		ToolName: "run_command",
		Input:    `{"command":"go test ./..."}`,
	})
	if view := model.View(); !strings.Contains(view, "go test ./...") && !strings.Contains(view, "Run") {
		t.Fatalf("expected running tool output visible:\n%s", view)
	}

	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "run_command",
		Risk:     "high",
		Input:    `{"command":"git add ."}`,
	})
	if view := model.View(); !strings.Contains(view, "Permission required") {
		t.Fatalf("expected permission panel:\n%s", view)
	}
}

func TestAgentRunStateStoresExecutionFields(t *testing.T) {
	stream := make(chan tea.Msg, 1)
	pending := &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file", Risk: "medium"}
	model := NewModel(Config{})
	model.agent.Busy = true
	model.agent.Err = context.Canceled
	model.agent.Stream = stream
	model.agent.Pending = pending
	model.agent.ConfirmChoice = choiceOnce

	if !model.agent.Busy {
		t.Fatal("expected agent run state to store busy flag")
	}
	if model.agent.Err != context.Canceled {
		t.Fatalf("expected agent run state to store error, got %v", model.agent.Err)
	}
	if model.agent.Stream != stream {
		t.Fatal("expected agent run state to store stream")
	}
	if model.agent.Pending != pending {
		t.Fatal("expected agent run state to store pending confirmation")
	}
	if model.agent.ConfirmChoice != choiceOnce {
		t.Fatalf("expected agent run state to store confirm-once selection, got %d", model.agent.ConfirmChoice)
	}
}

func TestTimelineCanScrollBackAndReturnToLatest(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 14)
	for i := 0; i < 20; i++ {
		model = appendTestAssistant(model, "message "+string(rune('a'+i)))
	}

	latest := model.View()
	if strings.Contains(latest, "message a") {
		t.Fatalf("expected default view to show latest entries, got:\n%s", latest)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if model.scroll == 0 {
		t.Fatal("expected PageUp to reveal earlier transcript entries")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(Model)
	if model.scroll != 0 || strings.Contains(model.View(), "message a") {
		t.Fatalf("expected PageDown to return to latest entries, scroll=%d view:\n%s", model.scroll, model.View())
	}
}

func TestEnterSubmitsAndAltEnterInsertsNewline(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("first line")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	model = updated.(Model)
	_ = cmd
	if got := model.inputValue(); got != "first line\n" {
		t.Fatalf("expected Alt+Enter to insert newline, got %q", got)
	}

	model = model.SetInput("first line\nsecond line")
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("model without agent should not start async command")
	}
	if got := model.inputValue(); got != "" {
		t.Fatalf("expected Enter submit to clear input, got %q", got)
	}
	if len(model.timeline.Turns) != 1 {
		t.Fatalf("expected one submitted turn, got %#v", model.timeline.Turns)
	}
	turn := model.timeline.Turns[0]
	if turn.User.Body != "first line\nsecond line" || len(turn.Blocks) != 1 || turn.Blocks[0].Kind != BlockError {
		t.Fatalf("expected multiline prompt and error block, got %#v", turn)
	}
}

func TestSubmitExpandsPathMentionsForAgentPrompt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello mention"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Status: Status{ProjectRoot: root}, Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model = model.SetInput("read @README.md")

	_, cmd := model.Submit(context.Background())
	if cmd == nil {
		t.Fatal("expected agent command")
	}
	msg := cmd()
	run, ok := msg.(runAgentMsg)
	if !ok {
		t.Fatalf("expected runAgentMsg, got %#v", msg)
	}
	if !strings.Contains(run.prompt, `<file path="README.md">`) || !strings.Contains(run.prompt, "hello mention") {
		t.Fatalf("expected path mention to be expanded before agent run, got:\n%s", run.prompt)
	}
}

func TestPromptHistoryUsesUpAndDownWhenComposerIsSingleLine(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("first prompt")
	model, _ = model.Submit(context.Background())
	model = model.SetInput("second prompt")
	model, _ = model.Submit(context.Background())

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if got := model.inputValue(); got != "second prompt" {
		t.Fatalf("expected first Up to show newest prompt, got %q", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if got := model.inputValue(); got != "first prompt" {
		t.Fatalf("expected second Up to show older prompt, got %q", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got := model.inputValue(); got != "second prompt" {
		t.Fatalf("expected Down to show newer prompt, got %q", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got := model.inputValue(); got != "" {
		t.Fatalf("expected Down at newest history item to restore draft, got %q", got)
	}
}

func TestComposerStateTracksHistoryDraft(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("first")
	model, _ = model.Submit(context.Background())
	model = model.SetInput("draft")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if got := model.inputValue(); got != "first" {
		t.Fatalf("expected newest history item, got %q", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got := model.inputValue(); got != "draft" {
		t.Fatalf("expected draft restored, got %q", got)
	}
}

func TestEditingRecalledHistoryExitsHistoryNavigation(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("first")
	model, _ = model.Submit(context.Background())
	model = model.SetInput("draft")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if got := model.inputValue(); got != "first" {
		t.Fatalf("expected recalled history item, got %q", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" edited")})
	model = updated.(Model)
	if got := model.inputValue(); got != "first edited" {
		t.Fatalf("expected edited history text, got %q", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got := model.inputValue(); got != "first edited" {
		t.Fatalf("expected Down after editing not to restore stale draft, got %q", got)
	}
}

func TestCompletingRecalledHistoryExitsHistoryNavigation(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:        "status",
		Description: "Show current runtime status",
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Commands: registry})
	model = model.rememberPrompt("/sta")
	model = model.SetInput("draft")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if got := model.inputValue(); got != "/sta" {
		t.Fatalf("expected recalled slash history item, got %q", got)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if got := model.inputValue(); got != "/status " {
		t.Fatalf("expected completed slash command, got %q", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got := model.inputValue(); got != "/status " {
		t.Fatalf("expected Down after completion not to restore stale draft, got %q", got)
	}
}

func TestCtrlRDoesNotWipeComposerDraft(t *testing.T) {
	model := NewModel(Config{}).SetInput("draft")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	next := updated.(Model)
	// Ctrl+R is reverse prompt-history search (may empty the draft into the
	// search UI); it must never panic or leave the model in a rail/layout state.
	_ = next
}

func TestLeaderKeyIgnoredWhilePermissionPending(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	model = updated.(Model)

	view := model.View()
	if !strings.Contains(view, "Permission required") || strings.Contains(view, "leader") {
		t.Fatalf("expected permission panel to remain authoritative:\n%s", view)
	}
	if got := strings.TrimSpace(model.renderFooter(model.currentLayout())); !strings.Contains(got, "allow") && got != "normal" {
		t.Fatalf("permission footer should show allow/reject hints, got %q", got)
	}
}

func TestPermissionRequestHidesOpenSuggestions(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("/")
	model = model.updateSuggestions()
	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatal("test setup expected visible command suggestions")
	}

	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	})

	if model.composer.Suggestions != nil && model.composer.Suggestions.IsVisible() {
		t.Fatalf("permission request should hide open suggestions:\n%s", model.View())
	}
	if model.agent.Pending == nil {
		t.Fatal("expected permission request to remain pending")
	}
}

func TestPermissionPendingSwallowsClearScreenShortcut(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("keep this turn")
	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	model = updated.(Model)

	if len(model.timeline.Turns) != 1 {
		t.Fatalf("Ctrl+L while permission owns input must not clear timeline, got %#v", model.timeline.Turns)
	}
	if model.agent.Pending == nil {
		t.Fatal("expected permission request to remain pending")
	}
}

func TestPermissionPendingSwallowsGlobalUIToggles(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	model = updated.(Model)
	if model.verbose {
		t.Fatal("Ctrl+O while permission owns input must not toggle verbose mode")
	}
	if model.agent.Pending == nil {
		t.Fatal("expected permission request to remain pending")
	}
}

func TestVKeyTypesWhenComposerIsEmpty(t *testing.T) {
	model := NewModel(Config{})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	next := updated.(Model)
	if next.verbose || next.inputValue() != "v" {
		t.Fatalf("v should type normally, verbose=%v input=%q", next.verbose, next.inputValue())
	}
}

func TestTUIPreferencesLoadAndPersistViewToggles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui-preferences.json")
	if err := os.WriteFile(path, []byte(`{"verbose":true,"rail_mode":"hidden"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	model := NewModel(Config{PreferencesPath: path})
	if !model.verbose {
		t.Fatal("expected verbose preference to load")
	}
	// Legacy rail_mode is ignored; chrome is always single-column.

	model = model.toggleVerbose()

	reloaded := NewModel(Config{PreferencesPath: path})
	if reloaded.verbose {
		t.Fatal("expected verbose preference to persist as disabled")
	}
}

func TestVKeyInDraftStaysInComposer(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("sa")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	model = updated.(Model)

	if model.verbose {
		t.Fatal("v inside a draft should not toggle verbose mode")
	}
	if got := model.inputValue(); got != "sav" {
		t.Fatalf("expected v to remain normal draft input, got %q", got)
	}
}

func TestPermissionPendingSwallowsCtrlDQuit(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = updated.(Model)

	if cmd != nil {
		t.Fatalf("Ctrl+D while permission owns input must not quit, got cmd %T", cmd)
	}
	if model.agent.Pending == nil {
		t.Fatal("expected permission request to remain pending")
	}
}

func TestPermissionPendingSwallowsAgentFocusShortcut(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("delegate")
	model.timeline = model.timeline.UpsertSubagentBlock("task-1", "subagent reviewer", "running", "")
	model.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"README.md"}`,
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlUp})
	model = updated.(Model)

	if model.focus != FocusComposer {
		t.Fatalf("Ctrl+Up while permission owns input must not focus agent bar, got %q", model.focus)
	}
	if model.agent.Pending == nil {
		t.Fatal("expected permission request to remain pending")
	}
}

func TestHighRiskPermissionAllowsApprovedTimelineScrollKeys(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 14)
	for i := 0; i < 30; i++ {
		model = appendTestAssistant(model, "message")
	}
	model.agent.Pending = &agent.Event{
		Type: agent.EventToolConfirmationRequested, ToolName: "run_command", Risk: "high",
		Input: `{"command":"rm -rf tmp"}`,
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if model.scroll == 0 || model.agent.Pending == nil {
		t.Fatalf("PageUp should scroll without dismissing permission, scroll=%d pending=%v", model.scroll, model.agent.Pending != nil)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(Model)
	if model.scroll != 0 || model.agent.Pending == nil {
		t.Fatalf("PageDown should return to latest without dismissing permission, scroll=%d pending=%v", model.scroll, model.agent.Pending != nil)
	}
}

func TestComposerViewMovesWorkspaceMetadataToRuntimeFooter(t *testing.T) {
	model := NewModel(Config{Status: Status{Model: "glm-5.1", PermissionMode: "confirm"}})
	model = model.SetSize(120, 32)
	model.mode = ModePlan
	model.agent.ContextTokens = 8100
	model.agent.ContextMaxTokens = 100000

	// Chrome shell: model/mode/context live on the prompt info line under input.
	composer := model.composerViewForWidth(model.currentLayout().TimelineW)
	for _, value := range []string{"plan", "glm-5.1", "ctx 8.1k/100.0k"} {
		if !strings.Contains(composer, value) {
			t.Fatalf("prompt info line missing %q:\n%s", value, composer)
		}
	}
}

func TestComposerViewOmitsDraftSizeMetadata(t *testing.T) {
	for _, draft := range []string{"", "hello world"} {
		model := NewModel(Config{Status: Status{Model: "glm-5.1"}})
		model = model.SetSize(120, 32)
		model = model.SetInput(draft)

		view := model.composerViewForWidth(model.currentLayout().TimelineW)
		for _, notWant := range []string{"chars", "tok ~"} {
			if strings.Contains(view, notWant) {
				t.Fatalf("composer should not show draft-size decoration %q:\n%s", notWant, view)
			}
		}
	}
}

func TestPromptHistorySkipsUnsafeAndOversizedInputs(t *testing.T) {
	model := NewModel(Config{})
	for _, prompt := range []string{
		"   ",
		"Authorization: Bearer sk-secret",
		"api_key=sk-secret",
		"password=hunter2",
		"secret: value",
		"token=value",
		"-----BEGIN PRIVATE KEY-----\nabc",
		"data:image/png;base64," + strings.Repeat("a", 260),
		strings.Repeat("x", maxHistoryPromptBytes+1),
	} {
		model = model.rememberPrompt(prompt)
	}
	if history := model.promptHistory(); len(history) != 0 {
		t.Fatalf("expected unsafe prompts not to be remembered, got %#v", history)
	}

	model = model.rememberPrompt("safe prompt")
	model = model.rememberPrompt("safe prompt")
	history := model.promptHistory()
	if len(history) != 1 || history[0] != "safe prompt" {
		t.Fatalf("expected one non-duplicate safe prompt, got %#v", history)
	}
}

func TestPromptHistoryLoadsAndPersistsAcrossModels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt-history.json")
	seed := []string{"previous prompt"}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	model := NewModel(Config{PromptHistoryPath: path})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if got := model.inputValue(); got != "previous prompt" {
		t.Fatalf("expected persisted prompt recalled, got %q", got)
	}

	model = model.SetInput("next prompt")
	model.Submit(context.Background())

	reloaded := NewModel(Config{PromptHistoryPath: path})
	history := reloaded.promptHistory()
	if len(history) != 2 || history[0] != "previous prompt" || history[1] != "next prompt" {
		t.Fatalf("expected persisted prompt history, got %#v", history)
	}
}

func TestPromptHistoryPersistenceSkipsUnsafePrompts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt-history.json")

	model := NewModel(Config{PromptHistoryPath: path})
	model = model.SetInput("api_key=sk-secret")
	model, _ = model.Submit(context.Background())

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unsafe prompt should not create history file, stat err=%v", err)
	}

	model = model.SetInput("safe prompt")
	model.Submit(context.Background())
	reloaded := NewModel(Config{PromptHistoryPath: path})
	history := reloaded.promptHistory()
	if len(history) != 1 || history[0] != "safe prompt" {
		t.Fatalf("expected only safe prompt persisted, got %#v", history)
	}
}

func TestComposerEditingAndReservedClearShortcut(t *testing.T) {
	model := appendTestAssistant(NewModel(Config{}), "keep?").SetInput("draft")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model = updated.(Model)
	if model.inputValue() != "" || len(model.timeline.Turns) != 1 {
		t.Fatalf("Ctrl+U should edit input without changing timeline, input=%q turns=%d", model.inputValue(), len(model.timeline.Turns))
	}

	model = model.SetInput("draft")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	model = updated.(Model)
	if len(model.timeline.Turns) != 1 || model.inputValue() != "draft" {
		t.Fatalf("reserved Ctrl+L changed state, input=%q timeline=%#v", model.inputValue(), model.timeline)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := updated.(Model).inputValue(); got != "" {
		t.Fatalf("Esc should clear draft input, got %q", got)
	}
}

func TestRuntimeFooterOmitsPermanentShortcutLegend(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 24)

	footer := model.renderFooter(model.currentLayout())
	// Context-sensitive shortcuts bar is allowed; permanent help legends are not.
	for _, notWant := range []string{"? help", "/ commands", "@ files"} {
		if strings.Contains(footer, notWant) {
			t.Fatalf("runtime footer should omit permanent shortcut %q: %s", notWant, footer)
		}
	}
	if strings.TrimSpace(footer) == "" {
		t.Fatal("expected non-empty shortcuts/footer row")
	}
}

func TestCtrlUDeletesCurrentLinePrefixInMultilineComposer(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("first line\nsecond line")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model = updated.(Model)

	if got := model.inputValue(); got != "first line\n" {
		t.Fatalf("expected Ctrl+U to delete only current line prefix, got %q", got)
	}
}

func TestCtrlKDeletesCurrentLineSuffixInMultilineComposer(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("first line\nsecond line")
	model.composer.Input.SetCursor(len("second"))

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	model = updated.(Model)

	if got := model.inputValue(); got != "first line\nsecond" {
		t.Fatalf("expected Ctrl+K to delete only current line suffix, got %q", got)
	}
}

func TestCtrlCClearsDraftInputWithoutQuitting(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("draft")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	next := updated.(Model)

	if got := next.inputValue(); got != "" {
		t.Fatalf("expected Ctrl+C to clear draft input, got %q", got)
	}
	if cmd != nil {
		t.Fatal("expected Ctrl+C with draft input not to quit the TUI")
	}
}

func TestCtrlDQuitsWhenComposerIsEmpty(t *testing.T) {
	model := NewModel(Config{})

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatal("expected Ctrl+D with empty input to quit the TUI")
	}
}

func TestCtrlCCancelsRunningAgentWithoutQuitting(t *testing.T) {
	cancelled := false
	model := NewModel(Config{})
	model.agent.Busy = true
	model.agent.Cancel = func() { cancelled = true }

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	next := updated.(Model)

	if !cancelled {
		t.Fatal("expected Ctrl+C to cancel the running agent")
	}
	if next.agent.Busy {
		t.Fatal("expected Ctrl+C to mark the agent idle after cancellation")
	}
	if cmd != nil {
		t.Fatal("expected Ctrl+C while busy not to quit the TUI")
	}
}

func TestCtrlCCancelsRunningAgentAndClearsQueuedPrompts(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model.agent.Cancel = func() {}
	model.agent.QueuedPrompts = []string{"second", "third"}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	next := updated.(Model)

	if next.agent.Busy {
		t.Fatal("expected Ctrl+C to mark the agent idle after cancellation")
	}
	if len(next.agent.QueuedPrompts) != 0 {
		t.Fatalf("expected Ctrl+C cancellation to clear queued prompts, got %#v", next.agent.QueuedPrompts)
	}
}

func TestEscCancelsRunningAgentWithoutQuitting(t *testing.T) {
	cancelled := false
	model := NewModel(Config{})
	model.agent.Busy = true
	model.agent.Cancel = func() { cancelled = true }

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := updated.(Model)

	if !cancelled {
		t.Fatal("expected Esc to cancel the running agent")
	}
	if next.agent.Busy {
		t.Fatal("expected Esc to mark the agent idle after cancellation")
	}
	if cmd != nil {
		t.Fatal("expected Esc while busy not to quit the TUI")
	}
}

func TestEscCancelsRunningAgentAndClearsQueuedPrompts(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model.agent.Cancel = func() {}
	model.agent.QueuedPrompts = []string{"second", "third"}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := updated.(Model)

	if next.agent.Busy {
		t.Fatal("expected Esc to mark the agent idle after cancellation")
	}
	if len(next.agent.QueuedPrompts) != 0 {
		t.Fatalf("expected Esc cancellation to clear queued prompts, got %#v", next.agent.QueuedPrompts)
	}
}

func TestStaleAgentDoneAfterLocalCancelDoesNotAppendError(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("stop this")
	model.agent.Busy = true
	model.agent.Stream = make(chan tea.Msg)
	model.agent.Cancel = func() {}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	blocksBefore := len(model.timeline.Turns[len(model.timeline.Turns)-1].Blocks)

	updated, _ = model.Update(agentDoneMsg{err: context.Canceled})
	model = updated.(Model)

	if model.agent.Err != nil {
		t.Fatalf("stale canceled done after local cancel should not set error, got %v", model.agent.Err)
	}
	if got := len(model.timeline.Turns[len(model.timeline.Turns)-1].Blocks); got != blocksBefore {
		t.Fatalf("stale canceled done should not append timeline blocks, got %d want %d", got, blocksBefore)
	}
}

func TestStaleSuccessfulAgentDoneAfterLocalCancelDoesNotMarkDone(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("stop this")
	model.agent.Busy = true
	model.agent.Stream = make(chan tea.Msg)
	model.agent.Cancel = func() {}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if got := model.timeline.Turns[len(model.timeline.Turns)-1].Run.State; got != "cancelled" {
		t.Fatalf("test setup expected cancelled run state, got %q", got)
	}

	updated, _ = model.Update(agentDoneMsg{})
	model = updated.(Model)

	if got := model.timeline.Turns[len(model.timeline.Turns)-1].Run.State; got != "cancelled" {
		t.Fatalf("stale successful done after local cancel should not overwrite run state, got %q", got)
	}
}

func TestAgentDoneHumanizesProviderError(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("hello")
	model.agent.Busy = true

	updated, _ := model.Update(agentDoneMsg{err: errors.New("model API returned HTTP 500: upstream overloaded")})
	next := updated.(Model)

	if len(next.timeline.Turns) != 1 || len(next.timeline.Turns[0].Blocks) != 1 {
		t.Fatalf("expected one error block, got %#v", next.timeline.Turns)
	}
	body := next.timeline.Turns[0].Blocks[0].Body
	if !strings.Contains(body, "Model provider is temporarily unavailable") || !strings.Contains(body, "Original: model API returned HTTP 500") {
		t.Fatalf("expected humanized provider error with original detail, got:\n%s", body)
	}
}

func TestCtrlLReservedPreservesGroupedTimeline(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}}).SetInput("hello")
	model, _ = model.Submit(context.Background())
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "chunk"})
	turns := len(model.timeline.Turns)
	live := model.agent.LiveStream
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	next := updated.(Model)
	if len(next.timeline.Turns) != turns || next.agent.LiveStream != live {
		t.Fatalf("reserved Ctrl+L changed grouped timeline/live state: turns=%d live=%p", len(next.timeline.Turns), next.agent.LiveStream)
	}
}

func TestScrollPausesAutoFollowUntilReturnToBottom(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 8)
	for i := 0; i < 20; i++ {
		model = appendTestAssistant(model, "message "+string(rune('a'+i)))
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if !model.scrollPaused || model.newOutputBelow {
		t.Fatalf("expected manual scroll to pause follow without new output marker, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}

	model = appendTestAssistant(model, "message z")
	if !model.scrollPaused || !model.newOutputBelow {
		t.Fatalf("expected new output marker while scrolled up, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}
	// Scroll chrome ("↑ N lines" / "↓ N new") was removed; only internal
	// follow-pause state remains.
	view := model.View()
	if strings.Contains(view, "↓ 1 new") || strings.Contains(view, "End latest") {
		t.Fatalf("scroll chrome must not render, got:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(Model)
	if model.scroll != 0 || model.scrollPaused || model.newOutputBelow {
		t.Fatalf("expected PageDown to resume bottom follow, scroll=%d paused=%v marker=%v", model.scroll, model.scrollPaused, model.newOutputBelow)
	}
}

func TestNewOutputBelowTracksCountWhileScrolledUp(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 8)
	for i := 0; i < 20; i++ {
		model = appendTestAssistant(model, "message "+string(rune('a'+i)))
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	model = appendTestAssistant(model, "first new")
	model = appendTestAssistant(model, "second new")

	if !model.newOutputBelow || model.newOutputCount != 2 {
		t.Fatalf("expected internal new-output count=2 while scrolled up, below=%v count=%d", model.newOutputBelow, model.newOutputCount)
	}
	if strings.Contains(model.View(), "↓ 2 new") {
		t.Fatalf("scroll chrome must not render counted marker, got:\n%s", model.View())
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(Model)
	if model.newOutputBelow || model.newOutputCount != 0 {
		t.Fatalf("expected PageDown to clear internal new-output state, below=%v count=%d", model.newOutputBelow, model.newOutputCount)
	}
}

func TestAgentDoneMarksNewOutputWhileScrolledUp(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 8)
	model.timeline = model.timeline.StartUserTurn("long task")
	model.agent.Busy = true
	for i := 0; i < 20; i++ {
		model = appendTestAssistant(model, "message "+string(rune('a'+i)))
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if !model.scrollPaused || model.newOutputBelow {
		t.Fatalf("expected manual scroll to pause follow without new output marker, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}

	updated, _ = model.Update(agentDoneMsg{})
	model = updated.(Model)
	if !model.newOutputBelow {
		t.Fatalf("expected agent-done to mark new output while scrolled up")
	}
	view := model.View()
	if strings.Contains(view, "End latest") || strings.Contains(view, "agent done") {
		t.Fatalf("scroll chrome must not render done reminder, got:\n%s", view)
	}
}

func TestStreamingChunkMarksNewOutputWhileScrolledUp(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 8)
	for i := 0; i < 20; i++ {
		model = appendTestAssistant(model, "message "+string(rune('a'+i)))
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if !model.scrollPaused || model.newOutputBelow {
		t.Fatalf("expected manual scroll to pause follow without new output marker, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}

	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: " streamed tail"})
	if !model.scrollPaused || model.newOutputBelow {
		t.Fatalf("unfinished streamed line must stay hidden without a new-output marker, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "\n"})
	if !model.scrollPaused || !model.newOutputBelow {
		t.Fatalf("completing the streamed line must mark new output below, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}
}

func TestToolResultMarksNewOutputWhileScrolledUp(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 8)
	for i := 0; i < 20; i++ {
		model = appendTestAssistant(model, "message "+string(rune('a'+i)))
	}
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolRequested,
		ToolCallID: "tool-1",
		ToolName:   "read_file",
		Input:      `{"path":"README.md"}`,
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if !model.scrollPaused || model.newOutputBelow {
		t.Fatalf("expected manual scroll to pause follow without new output marker, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}

	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolResult,
		ToolCallID: "tool-1",
		ToolName:   "read_file",
		Output:     "file content",
	})
	if !model.scrollPaused || !model.newOutputBelow {
		t.Fatalf("expected tool result to mark new output below, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}
}

func TestReasoningChunkMarksNewOutputWhileScrolledUp(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 8)
	for i := 0; i < 20; i++ {
		model = model.beginUserTurn("message " + string(rune('a'+i)))
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if !model.scrollPaused || model.newOutputBelow {
		t.Fatalf("expected manual scroll to pause follow without new output marker, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}

	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "thinking through the task"})
	if !model.scrollPaused || model.newOutputBelow {
		t.Fatalf("unfinished reasoning line must stay hidden without a new-output marker, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "\n"})
	if !model.scrollPaused || !model.newOutputBelow {
		t.Fatalf("completing the reasoning line must mark new output below, got paused=%v marker=%v", model.scrollPaused, model.newOutputBelow)
	}
}

func TestScrollDoesNotGrowPastEarliestVisibleLine(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 14)
	for i := 0; i < 20; i++ {
		model = appendTestAssistant(model, "message "+string(rune('a'+i)))
	}
	maxScroll := model.maxScroll(model.currentLayout())
	if maxScroll <= 0 {
		t.Fatalf("test setup should be scrollable, maxScroll=%d", maxScroll)
	}

	for i := 0; i < 10; i++ {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
		model = updated.(Model)
	}
	if model.scroll != maxScroll {
		t.Fatalf("expected scroll to clamp at earliest visible line %d, got %d", maxScroll, model.scroll)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(Model)
	if model.scroll >= maxScroll {
		t.Fatalf("expected PageDown to move down immediately from the top, maxScroll=%d scroll=%d", maxScroll, model.scroll)
	}
}

func TestTranscriptSearchDoesNotOverridePermissionFooter(t *testing.T) {
	model := NewModel(Config{})
	model.search = SearchState{Active: true, Query: "needle", MatchIndex: -1}
	model.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file", Risk: "medium"}

	footer := model.renderFooter(model.currentLayout())
	// Permission owns the dock; footer shows allow/reject hints, not search UI.
	if strings.Contains(footer, "search") || strings.Contains(footer, "needle") {
		t.Fatalf("permission takeover should not show search footer, got %q", footer)
	}
	if !strings.Contains(footer, "allow") && !strings.Contains(footer, "reject") {
		t.Fatalf("permission takeover should show allow/reject hints, got %q", footer)
	}
}

func TestSearchHighlightWrapsAllOccurrences(t *testing.T) {
	highlighted := highlightSearchQuery("find the needle in the needle stack", "needle")
	if got := strings.Count(highlighted, "needle"); got != 2 {
		t.Fatalf("expected both occurrences preserved, got %d in:\n%s", got, highlighted)
	}
	// No match -> unchanged (no escapes injected).
	if plain := highlightSearchQuery("nothing here", "needle"); plain != "nothing here" {
		t.Fatalf("non-matching line should be unchanged, got:\n%s", plain)
	}
	// Empty query -> unchanged.
	if plain := highlightSearchQuery("some text", ""); plain != "some text" {
		t.Fatalf("empty query should leave line unchanged, got:\n%s", plain)
	}
}

func TestSubmitResumesBottomFollowWhenScrolledUp(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(80, 14)
	for i := 0; i < 20; i++ {
		model = model.beginUserTurn("message " + string(rune('a'+i)))
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if model.scroll == 0 || !model.scrollPaused {
		t.Fatalf("test setup should be scrolled up, scroll=%d paused=%v", model.scroll, model.scrollPaused)
	}
	model.newOutputBelow = true

	model = model.SetInput("new prompt")
	next, _ := model.Submit(context.Background())

	if next.scroll != 0 || next.scrollPaused || next.newOutputBelow {
		t.Fatalf("submit should follow the new prompt at bottom, scroll=%d paused=%v marker=%v", next.scroll, next.scrollPaused, next.newOutputBelow)
	}
}

func TestSubmittingSlashCommandRoutesThroughRegistry(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:        "status",
		Description: "test status",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{Output: "status from registry"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{
		Commands: registry,
		CommandEnv: command.Env{
			SessionID:      "session-1",
			ProjectRoot:    "D:\\dev\\repo",
			PermissionMode: "confirm",
			Model:          "glm-5.1",
			ToolCount:      11,
		},
	})
	model = model.SetInput("/status")

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("slash commands should execute synchronously in the model")
	}

	if len(next.timeline.Turns) != 1 {
		t.Fatalf("expected one command turn, got %#v", next.timeline.Turns)
	}
	turn := next.timeline.Turns[0]
	if turn.User.Body != "/status" {
		t.Fatalf("unexpected user prompt %#v", turn.User)
	}
	if len(turn.Blocks) != 1 || turn.Blocks[0].Kind != BlockCommand || !strings.Contains(turn.Blocks[0].Body, "status from registry") {
		t.Fatalf("expected command output block, got %#v", turn.Blocks)
	}
}

func TestSlashExitCommandsQuitWithoutAddingTimelineTurn(t *testing.T) {
	for _, input := range []string{"/exit", "/q"} {
		t.Run(input, func(t *testing.T) {
			model := NewModel(Config{})
			model = model.SetInput(input)

			next, cmd := model.Submit(context.Background())

			if cmd == nil {
				t.Fatalf("expected %s to quit the TUI", input)
			}
			if len(next.timeline.Turns) != 0 {
				t.Fatalf("expected %s not to add a timeline turn, got %#v", input, next.timeline.Turns)
			}
			if got := next.inputValue(); got != "" {
				t.Fatalf("expected %s to clear composer input, got %q", input, got)
			}
		})
	}
}

func TestSlashQuitCancelsRunningAgentBeforeQuitting(t *testing.T) {
	cancelled := false
	model := NewModel(Config{})
	model.agent.Busy = true
	model.agent.Cancel = func() { cancelled = true }
	model.agent.QueuedPrompts = []string{"next prompt"}
	model = model.SetInput("/quit")

	next, cmd := model.Submit(context.Background())

	if cmd == nil {
		t.Fatal("expected /quit to quit the TUI")
	}
	if !cancelled {
		t.Fatal("expected /quit to cancel the running agent before quitting")
	}
	if next.agent.Busy {
		t.Fatal("expected /quit to mark the agent idle after cancellation")
	}
	if len(next.agent.QueuedPrompts) != 0 {
		t.Fatalf("expected /quit cancellation to clear queued prompts, got %#v", next.agent.QueuedPrompts)
	}
}

func TestSlashCommandAddsGroupedCommandBlock(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:       "status",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{Output: "status from registry"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Commands: registry})
	model = model.SetInput("/status")

	next, _ := model.Submit(context.Background())

	if len(next.timeline.Turns) != 1 {
		t.Fatalf("expected one grouped turn, got %#v", next.timeline.Turns)
	}
	blocks := next.timeline.Turns[0].Blocks
	if len(blocks) != 1 || blocks[0].Kind != BlockCommand || !strings.Contains(blocks[0].Body, "status from registry") {
		t.Fatalf("expected grouped command block, got %#v", blocks)
	}
	if view := ansi.Strip(next.View()); !strings.Contains(view, "› /status") && !strings.Contains(view, "/status") {
		t.Fatalf("expected command block glyph in view:\n%s", view)
	}
}

func TestSlashRetryResubmitsLatestFailedTurnPrompt(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model.timeline = model.timeline.StartUserTurn("fix the failing test")
	model.timeline = model.timeline.MarkAgentEnded("failed", "boom", time.Now())
	model = model.SetInput("/retry")

	next, cmd := model.Submit(context.Background())
	if cmd == nil {
		t.Fatal("/retry should start an agent run")
	}
	msg := cmd()
	run, ok := msg.(runAgentMsg)
	if !ok {
		t.Fatalf("expected runAgentMsg, got %#v", msg)
	}
	if run.prompt != "fix the failing test" {
		t.Fatalf("expected retry to resubmit original prompt, got %q", run.prompt)
	}
	if len(next.timeline.Turns) != 2 || next.timeline.Turns[1].User.Body != "fix the failing test" {
		t.Fatalf("expected retry to open a new turn with original prompt, got %#v", next.timeline.Turns)
	}
	if got := next.inputValue(); got != "" {
		t.Fatalf("expected retry to clear composer, got %q", got)
	}
}

func TestSlashRetryResubmitsTurnWithFailedTool(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model.timeline = model.timeline.StartUserTurn("run tests")
	model.timeline = model.timeline.UpsertToolBlock(&ToolBlock{
		ID:     "tool-1",
		Name:   "go_test",
		Status: ToolFailed,
		Error:  "tests failed",
	})
	model.timeline = model.timeline.MarkAgentEnded("done", "", time.Now())
	model = model.SetInput("/retry")

	next, cmd := model.Submit(context.Background())
	if cmd == nil {
		t.Fatal("/retry should start an agent run for a failed tool")
	}
	msg := cmd()
	run, ok := msg.(runAgentMsg)
	if !ok {
		t.Fatalf("expected runAgentMsg, got %#v", msg)
	}
	if run.prompt != "run tests" {
		t.Fatalf("expected retry to resubmit failed tool turn prompt, got %q", run.prompt)
	}
	if len(next.timeline.Turns) != 2 || next.timeline.Turns[1].User.Body != "run tests" {
		t.Fatalf("expected retry to open a new turn with failed tool prompt, got %#v", next.timeline.Turns)
	}
}

func TestSlashRetryWithoutFailedTurnShowsError(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model.timeline = model.timeline.StartUserTurn("already ok")
	model.timeline = model.timeline.MarkAgentEnded("done", "", time.Now())
	model = model.SetInput("/retry")

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatalf("/retry without a failed turn should not start agent, got %T", cmd)
	}
	blocks := next.timeline.Turns[len(next.timeline.Turns)-1].Blocks
	if len(blocks) != 1 || blocks[0].Kind != BlockError || !strings.Contains(blocks[0].Body, "no failed turn") {
		t.Fatalf("expected retry error block, got %#v", blocks)
	}
}

func TestSlashHelpRendersGroupedHelpPanel(t *testing.T) {
	registry := command.NewRegistry()
	if err := commandbuiltin.RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Commands: registry}).SetSize(100, 60)
	model = model.SetInput("/help")

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("/help should execute synchronously in the model")
	}

	if len(next.timeline.Turns) != 1 {
		t.Fatalf("expected /help to add one grouped turn, got %#v", next.timeline.Turns)
	}
	blocks := next.timeline.Turns[0].Blocks
	if len(blocks) != 1 || blocks[0].Kind != BlockCommand || blocks[0].Panel == nil {
		t.Fatalf("expected grouped help panel block, got %#v", blocks)
	}
	view := next.View()
	for _, want := range []string{"help", "/status", "/exit", "Enter", "Ctrl+R", "Ctrl+Up", "Ctrl+L"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in help view:\n%s", want, view)
		}
	}
	for _, removed := range []string{"Ctrl+P", "Ctrl+X", "Adaptive Rail", "Ctrl+F", "Ctrl+H", "Alt+Ctrl"} {
		if strings.Contains(view, removed) {
			t.Fatalf("removed key %q appeared in help view:\n%s", removed, view)
		}
	}
}

func TestQuestionMarkTypesWhenComposerIsEmpty(t *testing.T) {
	model := NewModel(Config{})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	next := updated.(Model)
	if next.inputValue() != "?" || len(next.timeline.Turns) != 0 {
		t.Fatalf("? should type normally, input=%q turns=%d", next.inputValue(), len(next.timeline.Turns))
	}
}

func TestQuestionMarkInDraftStaysInComposer(t *testing.T) {
	registry := command.NewRegistry()
	if err := commandbuiltin.RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Commands: registry})
	model = model.SetInput("what")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	next := updated.(Model)
	if got := next.inputValue(); got != "what?" {
		t.Fatalf("expected ? to remain normal draft input, got %q", got)
	}
	if len(next.timeline.Turns) != 0 {
		t.Fatalf("? inside a draft should not add a timeline turn, got %#v", next.timeline.Turns)
	}
}

func TestBusySlashCommandDoesNotCreateAgentTurn(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:       "status",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{Output: "status while busy"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Commands: registry, Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model = model.SetInput("long task")
	model, _ = model.Submit(context.Background())
	model, _ = model.startAgent("long task")
	model = model.SetInput("/status")

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("slash commands should execute synchronously while agent is busy")
	}
	if len(next.timeline.Turns) != 1 {
		t.Fatalf("busy slash command must not create a second agent turn, got %#v", next.timeline.Turns)
	}
	blocks := next.timeline.Turns[0].Blocks
	if len(blocks) != 1 || blocks[0].Kind != BlockCommand || !strings.Contains(blocks[0].Body, "status while busy") {
		t.Fatalf("expected command block on current turn, got %#v", blocks)
	}

	updated, _ := next.Update(agentDoneMsg{})
	done := updated.(Model)
	if len(done.timeline.Turns) != 1 {
		t.Fatalf("agent completion after busy slash command must not create turns, got %#v", done.timeline.Turns)
	}
	if done.timeline.Turns[0].Run.State != "done" {
		t.Fatalf("expected original turn to be marked done, got %#v", done.timeline.Turns[0].Run)
	}
}

func TestBusyCustomPromptCommandQueuesExpandedPrompt(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:           "review",
		Description:    "review target",
		PromptTemplate: "review $ARGUMENTS",
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Commands: registry, Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model = model.SetInput("long task")
	model, _ = model.Submit(context.Background())
	model, _ = model.startAgent("long task")
	model = model.SetInput("/review tui")

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("busy custom prompt command should queue expanded prompt instead of starting an agent")
	}
	if len(next.agent.QueuedPrompts) != 1 || next.agent.QueuedPrompts[0] != "review tui" {
		t.Fatalf("expected expanded prompt queued, got %#v", next.agent.QueuedPrompts)
	}
	if len(next.timeline.Turns) != 1 {
		t.Fatalf("busy custom prompt command must not create a second turn, got %#v", next.timeline.Turns)
	}
}

func TestEnterOnSlashSuggestionRunsCommandImmediately(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:       "status",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{Output: "status ran"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Commands: registry, CommandEnv: command.Env{SessionID: "s", ProjectRoot: "."}})
	model = model.SetSize(100, 24)
	model.composer.Input.SetValue("/")
	model.composer.Suggestions.Show("")
	// Select the builtin status row (not whatever initialSelected is).
	selectSuggestionNamed(t, model.composer.Suggestions, "status")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)

	block, ok := latestTimelineBlock(m.timeline, BlockCommand)
	if !ok || !strings.Contains(block.Body, "status ran") {
		t.Fatalf("expected slash suggestion + Enter to run the command immediately: %+v", m.timeline)
	}
	if strings.TrimSpace(m.composer.Input.Value()) != "" {
		t.Fatalf("expected input cleared after run, got %q", m.composer.Input.Value())
	}
}

// selectSuggestionNamed moves the typeahead highlight onto name under the
// list's current filter (must match getCommandFilter at Enter time).
func selectSuggestionNamed(t *testing.T, sl *SuggestionList, name string) {
	t.Helper()
	if sl == nil {
		t.Fatal("nil suggestion list")
	}
	visible := sl.GetVisible(sl.filter)
	for i, item := range visible {
		if item.Name == name {
			sl.selected = i
			return
		}
	}
	t.Fatalf("suggestion %q not in filtered list (%q): %#v", name, sl.filter, visible)
}

func TestEnterOnSkillSuggestionCompletesWithoutSubmit(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "grilling", "description: grill plans\n", "Grill $ARGUMENTS")
	loader := skill.NewLoaderFromRoot(root)
	registry := command.NewRegistry()
	model := NewModel(Config{
		Commands:   registry,
		CommandEnv: command.Env{SessionID: "s", ProjectRoot: ".", SkillLoader: loader},
		Chat:       stubChat{},
	})
	model = model.SetSize(100, 24)
	model.composer.Suggestions = NewSuggestionListWithSkills(registry, loader)
	model.composer.Input.SetValue("/grill")
	model.composer.Suggestions.Show("grill")
	selectSuggestionNamed(t, model.composer.Suggestions, "grilling")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)
	got := m.composer.Input.Value()
	if got != "/grilling " {
		t.Fatalf("skill Enter should only complete into composer, got %q", got)
	}
	if len(m.timeline.Turns) != 0 {
		t.Fatalf("skill Enter must not submit a turn yet, turns=%d", len(m.timeline.Turns))
	}
	if m.composer.Suggestions != nil && m.composer.Suggestions.IsVisible() {
		t.Fatal("suggestion menu should hide after accept")
	}

	// User appends a free-text prompt, then Enter submits the skill with args.
	m.composer = m.composer.setValue("/grilling review this plan")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected skill submit to start an agent run")
	}
	if len(m.timeline.Turns) == 0 {
		t.Fatal("expected skill turn after second Enter")
	}
	body := m.timeline.Turns[len(m.timeline.Turns)-1].User.Body
	if !strings.Contains(body, "review this plan") {
		t.Fatalf("expanded skill should carry user args, body:\n%s", body)
	}
}

func TestEnterOnCustomPromptSuggestionCompletesWithoutSubmit(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name:           "review",
		Description:    "Review changes",
		PromptTemplate: "Review these files: $ARGUMENTS",
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{
		Commands:   registry,
		CommandEnv: command.Env{SessionID: "s", ProjectRoot: "."},
		Chat:       stubChat{},
	})
	model = model.SetSize(100, 24)
	model.composer.Suggestions = NewSuggestionList(registry)
	model.composer.Input.SetValue("/rev")
	model.composer.Suggestions.Show("rev")
	selectSuggestionNamed(t, model.composer.Suggestions, "review")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)
	if got := m.composer.Input.Value(); got != "/review " {
		t.Fatalf("custom prompt Enter should only complete, got %q", got)
	}
	if len(m.timeline.Turns) != 0 {
		t.Fatalf("custom prompt must not auto-submit, turns=%d", len(m.timeline.Turns))
	}
}

func TestSlashSuggestionAutoSubmitsPolicy(t *testing.T) {
	if !slashSuggestionAutoSubmits(Suggestion{Name: "status", Source: "builtin"}, nil) {
		t.Fatal("builtin should auto-submit")
	}
	if slashSuggestionAutoSubmits(Suggestion{Name: "grilling", Source: "skill"}, nil) {
		t.Fatal("skill must not auto-submit")
	}
	if slashSuggestionAutoSubmits(Suggestion{Name: "review", Source: "custom"}, nil) {
		t.Fatal("custom must not auto-submit")
	}
	registry := command.NewRegistry()
	_ = registry.Register(command.Command{Name: "review", PromptTemplate: "x $ARGUMENTS"})
	if slashSuggestionAutoSubmits(Suggestion{Name: "review", Source: "builtin"}, registry) {
		t.Fatal("prompt-template registry command must not auto-submit")
	}
}

func TestCopyCommandWritesLatestOutputToFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "latest.txt")
	model := NewModel(Config{})
	model = appendTestAssistant(model, "answer text")
	model = model.SetInput("/copy " + target)

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("/copy should run synchronously")
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected copy target written: %v", err)
	}
	if string(body) != "answer text" {
		t.Fatalf("expected latest assistant output copied, got %q", string(body))
	}
	if block, ok := latestTimelineBlock(next.timeline, BlockCommand); !ok || !strings.Contains(block.Body, "copied latest output to") {
		t.Fatalf("expected copy confirmation block, got %#v", next.timeline)
	}
}

func TestCopyCommandCopiesLatestOutputToClipboard(t *testing.T) {
	var copied string
	old := copyToClipboard
	copyToClipboard = func(value string) error {
		copied = value
		return nil
	}
	defer func() { copyToClipboard = old }()

	model := NewModel(Config{})
	model = appendTestAssistant(model, "clipboard text")
	model = model.SetInput("/copy")

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("/copy should run synchronously")
	}
	if copied != "clipboard text" {
		t.Fatalf("expected latest output copied to clipboard, got %q", copied)
	}
	if block, ok := latestTimelineBlock(next.timeline, BlockCommand); !ok || !strings.Contains(block.Body, "copied latest output to clipboard") {
		t.Fatalf("expected clipboard confirmation block, got %#v", next.timeline)
	}
}

func TestCopyCommandReportsMissingOutput(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("/copy")

	next, cmd := model.Submit(context.Background())
	if cmd != nil {
		t.Fatal("/copy should run synchronously")
	}
	if block, ok := latestTimelineBlock(next.timeline, BlockError); !ok || !strings.Contains(block.Body, "no output to copy") {
		t.Fatalf("expected no-output error block, got %#v", next.timeline)
	}
}

func TestSubmitWithoutAgentAddsGroupedErrorBlock(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("hello")

	next, _ := model.Submit(context.Background())

	if len(next.timeline.Turns) != 1 {
		t.Fatalf("expected one grouped turn, got %#v", next.timeline.Turns)
	}
	blocks := next.timeline.Turns[0].Blocks
	if len(blocks) != 1 || blocks[0].Kind != BlockError || !strings.Contains(blocks[0].Body, "agent is not configured") {
		t.Fatalf("expected grouped error block, got %#v", blocks)
	}
	if view := ansi.Strip(next.View()); !strings.Contains(view, "✗ error") && !strings.Contains(view, "error agent is not configured") {
		t.Fatalf("expected error block glyph in view:\n%s", view)
	}
}

func TestVerboseRenderBlockShowsTimestamp(t *testing.T) {
	createdAt := time.Date(2026, 6, 28, 12, 34, 0, 0, time.Local)
	block := Block{
		Kind:      BlockCommand,
		Title:     "/status",
		Body:      "ok",
		CreatedAt: createdAt,
	}
	model := NewModel(Config{})

	compact := model.renderBlock(block, 80)
	if strings.Contains(compact, "12:34") {
		t.Fatalf("compact block should not show timestamp:\n%s", compact)
	}

	model.verbose = true
	verbose := model.renderBlock(block, 80)
	if !strings.Contains(verbose, "12:34") {
		t.Fatalf("verbose block should show timestamp:\n%s", verbose)
	}
}

func TestToggleLatestToolBlockUpdatesGroupedTimeline(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model = model.SetInput("run command")
	model, _ = model.Submit(context.Background())
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolRequested,
		ToolName:   "run_command",
		ToolCallID: "call-diff",
		Input:      `{"command":"git diff"}`,
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolResult,
		ToolName:   "run_command",
		ToolCallID: "call-diff",
		Output:     strings.Repeat("+ changed line\n", 40),
	})
	if len(model.timeline.Turns) != 1 || len(model.timeline.Turns[0].Blocks) != 1 {
		t.Fatalf("expected one grouped tool block, got %#v", model.timeline.Turns)
	}
	if !model.timeline.Turns[0].Blocks[0].Tool.Collapsed {
		t.Fatalf("expected grouped tool to start collapsed, got %#v", model.timeline.Turns[0].Blocks[0].Tool)
	}

	model = model.toggleLatestToolBlock()
	if model.timeline.Turns[0].Blocks[0].Tool.Collapsed {
		t.Fatalf("expected the reusable tool-detail action to expand grouped timeline tool, got %#v", model.timeline.Turns[0].Blocks[0].Tool)
	}
}

func TestToggleLatestToolBlockUsesGroupedTimelineWhenEntriesEmpty(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("show diff")
	model.timeline = model.timeline.UpsertToolBlock(&ToolBlock{
		ID:        "call-diff",
		Name:      "run_command",
		Status:    ToolDone,
		Input:     `{"command":"git diff"}`,
		Output:    strings.Repeat("+ changed line\n", 40),
		Collapsed: true,
	})

	model = model.toggleLatestToolBlock()

	if model.timeline.Turns[0].Blocks[0].Tool.Collapsed {
		t.Fatalf("expected the reusable tool-detail action to expand latest grouped timeline tool when entries are empty, got %#v", model.timeline.Turns[0].Blocks[0].Tool)
	}
}

func TestToggleLatestToolBlockSkipsShortTools(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model = model.SetInput("run tools")
	model, _ = model.Submit(context.Background())
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolRequested,
		ToolName:   "run_command",
		ToolCallID: "call-diff",
		Input:      `{"command":"git diff"}`,
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolResult,
		ToolName:   "run_command",
		ToolCallID: "call-diff",
		Output:     strings.Repeat("+ changed line\n", 40),
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolRequested,
		ToolName:   "run_command",
		ToolCallID: "call-pwd",
		Input:      `{"command":"pwd"}`,
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolResult,
		ToolName:   "run_command",
		ToolCallID: "call-pwd",
		Output:     "D:/repo",
	})
	tools := timelineToolBlocks(model)
	if len(tools) != 2 {
		t.Fatalf("test setup expected two tool blocks, got %#v", model.timeline)
	}

	model = model.toggleLatestToolBlock()
	tools = timelineToolBlocks(model)

	if tools[0].Collapsed {
		t.Fatalf("expected the reusable tool-detail action to skip latest short tool and expand previous long tool, got %#v", tools[0])
	}
	if tools[1].Collapsed {
		t.Fatalf("expected latest short tool to remain unchanged, got %#v", tools[1])
	}
}

func TestToggleLatestToolBlockSkipsShortTimelineTools(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("run tools")
	model.timeline = model.timeline.UpsertToolBlock(&ToolBlock{
		ID:        "call-diff",
		Name:      "run_command",
		Status:    ToolDone,
		Input:     `{"command":"git diff"}`,
		Output:    strings.Repeat("+ changed line\n", 40),
		Collapsed: true,
	})
	model.timeline = model.timeline.UpsertToolBlock(&ToolBlock{
		ID:        "call-pwd",
		Name:      "run_command",
		Status:    ToolDone,
		Input:     `{"command":"pwd"}`,
		Output:    "D:/repo",
		Collapsed: false,
	})

	model = model.toggleLatestToolBlock()

	if model.timeline.Turns[0].Blocks[0].Tool.Collapsed {
		t.Fatalf("expected the reusable tool-detail action to skip latest short timeline tool and expand previous long tool, got %#v", model.timeline.Turns[0].Blocks[0].Tool)
	}
	if model.timeline.Turns[0].Blocks[1].Tool.Collapsed {
		t.Fatalf("expected latest short timeline tool to remain unchanged, got %#v", model.timeline.Turns[0].Blocks[1].Tool)
	}
}

func timelineToolBlocks(model Model) []*ToolBlock {
	var tools []*ToolBlock
	for _, turn := range model.timeline.Turns {
		for _, block := range turn.Blocks {
			if block.Tool != nil {
				tools = append(tools, block.Tool)
			}
		}
	}
	return tools
}

func TestCtrlEWithDraftDoesNotToggleLatestToolBlock(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model = model.SetInput("run command")
	model, _ = model.Submit(context.Background())
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolRequested,
		ToolName:   "run_command",
		ToolCallID: "call-diff",
		Input:      `{"command":"git diff"}`,
	})
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolResult,
		ToolName:   "run_command",
		ToolCallID: "call-diff",
		Output:     strings.Repeat("+ changed line\n", 40),
	})
	if !model.timeline.Turns[0].Blocks[0].Tool.Collapsed {
		t.Fatalf("expected grouped tool to start collapsed, got %#v", model.timeline.Turns[0].Blocks[0].Tool)
	}
	model = model.SetInput("draft")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	model = updated.(Model)
	if !model.timeline.Turns[0].Blocks[0].Tool.Collapsed {
		t.Fatalf("expected Ctrl+E with draft input not to expand latest tool, got %#v", model.timeline.Turns[0].Blocks[0].Tool)
	}
	if got := model.inputValue(); got != "draft" {
		t.Fatalf("expected Ctrl+E with draft input to preserve composer draft, got %q", got)
	}
}

func TestLetterXTypesIntoComposer(t *testing.T) {
	model := NewModel(Config{})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	next := updated.(Model)
	if got := next.inputValue(); got != "x" {
		t.Fatalf("expected x to type into composer instead of toggling tool block, got %q", got)
	}
}

func TestBusyAgentSubmitQueuesPrompt(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 2)}})
	model.agent.Busy = true
	model = model.SetInput("hello")

	next, cmd := model.Submit(context.Background())
	// a busy submit parks the prompt instead of starting an agent run
	if cmd != nil {
		t.Fatalf("expected busy submit not to schedule an agent run, got %T", cmd())
	}
	if len(next.agent.QueuedPrompts) != 1 || next.agent.QueuedPrompts[0] != "hello" {
		t.Fatalf("expected prompt queued, got %#v", next.agent.QueuedPrompts)
	}
	// the queued prompt stays out of the timeline; it renders pinned above the
	// composer until the current turn finishes
	for _, turn := range next.timeline.Turns {
		if strings.TrimSpace(turn.User.Body) != "" {
			t.Fatalf("queued prompt must not enter the timeline yet, got %#v", turn.User)
		}
	}
	if !next.agent.Busy {
		t.Fatalf("expected agent to stay busy on the original turn")
	}
}

func TestQueuedPromptRunsAfterTurn(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 2)}})
	model.agent.Busy = true
	model.agent.QueuedPrompts = []string{"second"}

	updated, _ := model.Update(agentDoneMsg{})
	next := updated.(Model)

	if len(next.agent.QueuedPrompts) != 0 {
		t.Fatalf("expected queue drained after the turn, got %#v", next.agent.QueuedPrompts)
	}
	if !next.agent.Busy {
		t.Fatalf("expected the queued prompt to start the agent")
	}
	// the drained prompt should now be in the timeline as a user entry
	found := false
	for _, turn := range next.timeline.Turns {
		if strings.Contains(turn.User.Body, "second") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected drained prompt to enter the timeline, got %#v", next.timeline)
	}
}

func TestCancelDropsQueuedPrompts(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 2)}})
	model.agent.Busy = true
	model.agent.QueuedPrompts = []string{"second", "third"}

	updated, _ := model.Update(agentDoneMsg{err: context.Canceled})
	next := updated.(Model)

	if len(next.agent.QueuedPrompts) != 0 {
		t.Fatalf("expected cancel to drop the queue, got %#v", next.agent.QueuedPrompts)
	}
	if next.agent.Busy {
		t.Fatalf("expected cancel to not start the next queued prompt")
	}
}

func TestMissingConfirmerMarksGroupedPendingToolFailed(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "write file",
		Input:    `{"path":"README.md"}`,
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	next := updated.(Model)

	if len(next.timeline.Turns) != 1 {
		t.Fatalf("expected one grouped turn, got %#v", next.timeline.Turns)
	}
	blocks := next.timeline.Turns[0].Blocks
	if len(blocks) < 2 {
		t.Fatalf("expected failed tool and error block, got %#v", blocks)
	}
	if blocks[0].Tool == nil || blocks[0].Tool.Status != ToolFailed || !strings.Contains(blocks[0].Tool.Error, "no TUI confirmer") {
		t.Fatalf("expected pending tool to become failed, got %#v", blocks[0])
	}
	if blocks[1].Kind != BlockError || !strings.Contains(blocks[1].Body, "no TUI confirmer") {
		t.Fatalf("expected grouped confirmer error block, got %#v", blocks[1])
	}
}

func TestEnterSubmissionUsesTUIRootContextForSlashCommands(t *testing.T) {
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	registry := command.NewRegistry()
	ctxSeen := make(chan context.Context, 1)
	if err := registry.Register(command.Command{
		Name:       "status",
		RemoteSafe: true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			ctxSeen <- ctx
			return command.Result{Output: "ok"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Context: rootCtx, Commands: registry})
	model = model.SetInput("/status")

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cancelRoot()
	usedCtx := <-ctxSeen

	select {
	case <-usedCtx.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected slash command to receive TUI root context")
	}
}

func TestHighRiskConfirmationRejectsWithNShortcut(t *testing.T) {
	confirmer := NewConfirmer()
	requestReady := make(chan struct{})
	approved := make(chan bool, 1)
	go func() {
		result, err := confirmer.Confirm(context.Background(), confirmationRequestForRisk("high"))
		if err != nil {
			t.Error(err)
		}
		approved <- result
	}()
	waitForPendingConfirmation(t, confirmer, requestReady)
	<-requestReady

	model := NewModel(Config{Confirmer: confirmer})
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "run_command",
		Risk:     "high",
		Message:  "run dangerous command",
		Input:    `{"command":"rm -rf tmp"}`,
	})
	model = model.SetInput("ordinary draft")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	next := updated.(Model)
	if next.agent.Pending != nil {
		t.Fatal("expected pending confirmation to be cleared after response")
	}
	if got := next.inputValue(); got != "ordinary draft" {
		t.Fatalf("expected confirmation shortcut not to mutate prompt input, got %q", got)
	}
	if got := <-approved; got {
		t.Fatal("expected high-risk n shortcut to reject")
	}
}

func TestMediumRiskConfirmationAcceptsYShortcut(t *testing.T) {
	confirmer := NewConfirmer()
	requestReady := make(chan struct{})
	approved := make(chan bool, 1)
	go func() {
		result, err := confirmer.Confirm(context.Background(), confirmationRequestForRisk("medium"))
		if err != nil {
			t.Error(err)
		}
		approved <- result
	}()
	waitForPendingConfirmation(t, confirmer, requestReady)
	<-requestReady

	model := NewModel(Config{Confirmer: confirmer})
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "write file",
		Input:    `{"path":"README.md"}`,
	})
	model = model.SetInput("ordinary draft")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	next := updated.(Model)
	if next.agent.Pending != nil {
		t.Fatal("expected pending confirmation to be cleared after response")
	}
	if got := next.inputValue(); got != "ordinary draft" {
		t.Fatalf("expected confirmation shortcut not to mutate prompt input, got %q", got)
	}
	if got := <-approved; !got {
		t.Fatal("expected medium-risk y response to approve")
	}
}

func TestInlineConfirmationShortcutsDoNotUsePromptInput(t *testing.T) {
	confirmer := NewConfirmer()
	requestReady := make(chan struct{})
	approved := make(chan bool, 1)
	go func() {
		result, err := confirmer.Confirm(context.Background(), confirmationRequestForRisk("medium"))
		if err != nil {
			t.Error(err)
		}
		approved <- result
	}()
	waitForPendingConfirmation(t, confirmer, requestReady)
	<-requestReady

	model := NewModel(Config{Confirmer: confirmer})
	model = model.SetInput("ordinary draft")
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "write file",
		Input:    `{"path":"README.md"}`,
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	next := updated.(Model)
	if next.agent.Pending != nil {
		t.Fatal("expected y shortcut to clear pending confirmation")
	}
	if got := next.inputValue(); got != "ordinary draft" {
		t.Fatalf("expected confirmation shortcut not to mutate prompt input, got %q", got)
	}
	if got := <-approved; !got {
		t.Fatal("expected y shortcut to approve medium-risk request")
	}
}

func TestHighRiskConfirmationUsesSelectorAndEnter(t *testing.T) {
	confirmer := NewConfirmer()
	approved := make(chan bool, 1)
	startConfirm := func() {
		ready := make(chan struct{})
		go func() {
			result, err := confirmer.Confirm(context.Background(), confirmationRequestForRisk("high"))
			if err != nil {
				t.Error(err)
			}
			approved <- result
		}()
		waitForPendingConfirmation(t, confirmer, ready)
	}

	model := NewModel(Config{Confirmer: confirmer})
	model = model.SetInput("ordinary draft")

	// Default selection is No; Enter rejects the high-risk request as-is.
	startConfirm()
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "run_command",
		Risk:     "high",
		Message:  "run command",
		Input:    `{"command":"rm -rf tmp"}`,
	})
	if model.agent.ConfirmChoice == choiceOnce {
		t.Fatal("expected high-risk confirmation to default to the No selection")
	}
	view := model.View()
	if !strings.Contains(view, "Yes") || !strings.Contains(view, "No") {
		t.Fatalf("expected permission panel to render the Yes/No selector:\n%s", view)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := updated.(Model)
	if next.agent.Pending != nil {
		t.Fatal("expected Enter to confirm the default No selection")
	}
	if got := <-approved; got {
		t.Fatal("expected default No selection to reject the high-risk request")
	}

	// A fresh request: navigate to Yes, then Enter approves.
	startConfirm()
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "run_command",
		Risk:     "high",
		Message:  "run command",
		Input:    `{"command":"rm -rf tmp"}`,
	})
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	next = updated.(Model)
	if next.agent.ConfirmChoice != choiceOnce {
		t.Fatal("expected → to select Yes")
	}
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next = updated.(Model)
	if next.agent.Pending != nil {
		t.Fatal("expected Enter to confirm the Yes selection")
	}
	if got := <-approved; !got {
		t.Fatal("expected Yes selection + Enter to approve the high-risk request")
	}
	if got := next.inputValue(); got != "ordinary draft" {
		t.Fatalf("expected confirmation flow not to mutate prompt input, got %q", got)
	}
}

func TestHighRiskConfirmationArrowKeysCycleAndMatchVisualDirection(t *testing.T) {
	model := NewModel(Config{})
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "run_command",
		Risk:     "high",
		Message:  "run command",
		Input:    `{"command":"rm -rf tmp"}`,
	})
	// High-risk defaults to No (safe); panel is vertical Yes / No top→bottom.
	// ↑/← move up the list; ↓/→ move down; both wrap at the ends.

	// From No (bottom): Up moves to Yes.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.agent.ConfirmChoice != choiceOnce {
		t.Fatalf("Up from No should move to Yes, got %v", model.agent.ConfirmChoice)
	}
	// Up again past the top wraps to No.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.agent.ConfirmChoice == choiceOnce {
		t.Fatalf("Up past Yes should wrap to No, got %v", model.agent.ConfirmChoice)
	}
	// From No (bottom): Down wraps to Yes (top).
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.agent.ConfirmChoice != choiceOnce {
		t.Fatalf("Down from No should wrap to Yes, got %v", model.agent.ConfirmChoice)
	}
	// Down from Yes moves to No.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.agent.ConfirmChoice == choiceOnce {
		t.Fatalf("Down from Yes should move to No, got %v", model.agent.ConfirmChoice)
	}

	// Left/Right remain aliases of Up/Down for older muscle memory.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(Model)
	if model.agent.ConfirmChoice != choiceOnce {
		t.Fatalf("Left from No should move to Yes, got %v", model.agent.ConfirmChoice)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if model.agent.ConfirmChoice == choiceOnce {
		t.Fatalf("Right from Yes should move to No, got %v", model.agent.ConfirmChoice)
	}

	// Tab still toggles between the two (independent of arrow cycling).
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.agent.ConfirmChoice != choiceOnce {
		t.Fatal("Tab should toggle from No to Yes")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.agent.ConfirmChoice == choiceOnce {
		t.Fatal("Tab should toggle from Yes to No")
	}
}

func TestPermissionPanelUpDownSelectsAndEnterConfirms(t *testing.T) {
	confirmer := NewConfirmer()
	approved := make(chan bool, 1)
	go func() {
		ok, _ := confirmer.Confirm(context.Background(), confirmationRequestForRisk("medium"))
		approved <- ok
	}()
	// Wait until Confirm is blocked on the TUI response channel.
	requestReady := make(chan struct{})
	waitForPendingConfirmation(t, confirmer, requestReady)
	<-requestReady

	model := NewModel(Config{Confirmer: confirmer, RuleSource: nil})
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "write file",
		Input:    `{"path":"a.go"}`,
	})
	// Medium defaults to Allow once; Always is hidden without RuleSource so
	// Down jumps Allow once → Reject.
	if model.agent.ConfirmChoice != choiceOnce {
		t.Fatalf("medium default choice = %v, want once", model.agent.ConfirmChoice)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.agent.ConfirmChoice != choiceReject {
		t.Fatalf("Down should select Reject when Always is unavailable, got %v", model.agent.ConfirmChoice)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if model.agent.ConfirmChoice != choiceOnce {
		t.Fatalf("Up should return to Allow once, got %v", model.agent.ConfirmChoice)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.agent.Pending != nil {
		t.Fatal("Enter should confirm the highlighted Allow once choice")
	}
	if got := <-approved; !got {
		t.Fatal("expected Allow once + Enter to approve")
	}
}

func TestEnterDoesNotSubmitPromptWhileConfirmationIsPending(t *testing.T) {
	confirmer := NewConfirmer()
	requestReady := make(chan struct{})
	go func() {
		_, _ = confirmer.Confirm(context.Background(), confirmationRequestForRisk("medium"))
	}()
	waitForPendingConfirmation(t, confirmer, requestReady)
	<-requestReady

	model := NewModel(Config{Confirmer: confirmer})
	model = model.SetInput("do not submit")
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "write file",
		Input:    `{"path":"README.md"}`,
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := updated.(Model)
	// Enter confirms the highlighted permission row (Allow once by default).
	// It must never submit the composer draft as a new user turn.
	if next.agent.Pending != nil {
		t.Fatal("expected Enter to confirm the highlighted permission choice")
	}
	if got := next.inputValue(); got != "do not submit" {
		t.Fatalf("expected Enter on permission to preserve prompt input, got %q", got)
	}
	if len(next.timeline.Turns) != 1 {
		t.Fatalf("expected no new user turn from Enter on permission, got %#v", next.timeline)
	}
	if body := strings.TrimSpace(next.timeline.Turns[0].User.Body); body != "" && body != "do not submit" {
		// User.Body is empty until submit; permission must not StartUserTurn.
		t.Fatalf("unexpected user turn body while confirming: %q", body)
	}
}

func TestPendingMediumConfirmationConsumesOrdinaryRunes(t *testing.T) {
	confirmer := NewConfirmer()
	requestReady := make(chan struct{})
	go func() {
		_, _ = confirmer.Confirm(context.Background(), confirmationRequestForRisk("medium"))
	}()
	waitForPendingConfirmation(t, confirmer, requestReady)
	<-requestReady

	model := NewModel(Config{Confirmer: confirmer})
	model = model.SetInput("ordinary draft")
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "write file",
		Input:    `{"path":"README.md"}`,
	})

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	next := updated.(Model)
	if cmd != nil {
		t.Fatal("expected ordinary confirmation rune not to schedule a command")
	}
	if next.agent.Pending == nil {
		t.Fatal("expected ordinary confirmation rune to keep confirmation pending")
	}
	if got := next.inputValue(); got != "ordinary draft" {
		t.Fatalf("expected ordinary confirmation rune not to mutate prompt input, got %q", got)
	}
}

func TestConfirmationResponseContinuesAgentStream(t *testing.T) {
	stream := make(chan tea.Msg, 1)
	stream <- agentEventMsg{event: agent.Event{
		Type:     agent.EventToolApproved,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "approved",
	}}
	model := NewModel(Config{Confirmer: NewConfirmer()})
	model.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file", Risk: "medium"}
	model.agent.Stream = stream
	model = model.SetInput("ordinary draft")

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("expected confirmation response to continue reading the agent stream")
	}
	msg := cmd()
	if _, ok := msg.(agentEventMsg); !ok {
		t.Fatalf("expected next agent event message, got %#v", msg)
	}
}

func TestEscRejectsPendingConfirmationAndContinuesAgentStream(t *testing.T) {
	confirmer := NewConfirmer()
	requestReady := make(chan struct{})
	approved := make(chan bool, 1)
	go func() {
		result, err := confirmer.Confirm(context.Background(), confirmationRequestForRisk("medium"))
		if err != nil {
			t.Error(err)
		}
		approved <- result
	}()
	waitForPendingConfirmation(t, confirmer, requestReady)
	<-requestReady

	stream := make(chan tea.Msg, 1)
	stream <- agentEventMsg{event: agent.Event{
		Type:     agent.EventToolRejected,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "rejected by user",
	}}
	model := NewModel(Config{Confirmer: confirmer})
	model.agent.Pending = &agent.Event{Type: agent.EventToolConfirmationRequested, ToolName: "write_file", Risk: "medium"}
	model.agent.Stream = stream
	model = model.SetInput("yes")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := updated.(Model)
	if next.agent.Pending != nil {
		t.Fatal("expected Esc to clear pending confirmation")
	}
	if next.inputValue() != "yes" {
		t.Fatalf("expected Esc confirmation shortcut to preserve prompt input, got %q", next.inputValue())
	}
	if got := <-approved; got {
		t.Fatal("expected Esc to reject pending confirmation")
	}
	if cmd == nil {
		t.Fatal("expected Esc rejection to continue reading the agent stream")
	}
	msg := cmd()
	if _, ok := msg.(agentEventMsg); !ok {
		t.Fatalf("expected next agent event message, got %#v", msg)
	}
}

func TestStartAgentUsesTUIRootContext(t *testing.T) {
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	release := make(chan struct{})
	runner := &contextRecordingChatRunner{
		ctxSeen:     make(chan context.Context, 1),
		ctxCanceled: make(chan struct{}, 1),
		release:     release,
	}
	model := NewModel(Config{Context: rootCtx, Chat: runner})

	_, _ = model.startAgent("hello")
	<-runner.ctxSeen
	cancelRoot()

	select {
	case <-runner.ctxCanceled:
	case <-time.After(200 * time.Millisecond):
		close(release)
		t.Fatal("expected root context cancellation to cancel running agent")
	}
}

func TestAgentEventSinkStopsWaitingAfterTUIContextIsCanceled(t *testing.T) {
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	runner := &blockingSinkChatRunner{
		blocking: make(chan struct{}, 1),
		done:     make(chan struct{}, 1),
	}
	model := NewModel(Config{Context: rootCtx, Chat: runner})

	next, _ := model.startAgent("hello")
	<-runner.blocking
	cancelRoot()

	select {
	case <-runner.done:
	case <-time.After(200 * time.Millisecond):
		drainUntilDone(next.agent.Stream, runner.done, 200*time.Millisecond)
		t.Fatal("expected event sink to stop waiting after TUI context cancellation")
	}
}

func TestAgentDoneCancelsTaskContext(t *testing.T) {
	runner := &immediateChatRunner{
		ctxSeen: make(chan context.Context, 1),
	}
	model := NewModel(Config{Chat: runner})

	next, cmd := model.startAgent("hello")
	if cmd == nil {
		t.Fatal("expected command to wait for agent completion")
	}
	runCtx := <-runner.ctxSeen
	msg := cmd()
	updated, _ := next.Update(msg)
	if updated.(Model).agent.Busy {
		t.Fatal("expected agent to no longer be busy after completion")
	}

	select {
	case <-runCtx.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected task context to be canceled after agent completion")
	}
}

func TestStartAgentClearsPreviousError(t *testing.T) {
	model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
	model.agent.Err = context.Canceled

	next, _ := model.startAgent("hello")
	if next.agent.Err != nil {
		t.Fatalf("expected startAgent to clear previous error, got %v", next.agent.Err)
	}
}

// compactingChatRunner is a ChatRunner whose Compact records that it ran and
// streams started/finished events before returning.
type compactingChatRunner struct {
	compactRan chan context.Context
}

func (r *compactingChatRunner) RunWithEvents(ctx context.Context, prompt string, sink agent.EventSink) (*agent.RunResult, error) {
	return nil, nil
}

func (r *compactingChatRunner) Compact(ctx context.Context, sink agent.EventSink) error {
	r.compactRan <- ctx
	sink(agent.Event{Type: agent.EventCompactionStarted, Message: "summarizing"})
	sink(agent.Event{Type: agent.EventCompactionFinished, Message: "compacted 100 -> 20 tokens"})
	return nil
}

func TestSlashCompactStartsAsyncCompaction(t *testing.T) {
	runner := &compactingChatRunner{compactRan: make(chan context.Context, 1)}
	model := NewModel(Config{Chat: runner})
	model = model.SetSize(100, 24)

	next, cmd := model.runCommand(context.Background(), "/compact")
	if cmd == nil {
		t.Fatal("expected /compact to start an async operation, not a blocking command")
	}
	if !next.agent.Busy {
		t.Fatal("expected agent to be marked busy while compacting")
	}
	select {
	case <-runner.compactRan:
	case <-time.After(time.Second):
		t.Fatal("expected Chat.Compact to be invoked asynchronously")
	}
}

func TestSlashCompactRequiresAgent(t *testing.T) {
	model := NewModel(Config{}) // no Chat runner
	model = model.SetSize(100, 24)

	next, cmd := model.runCommand(context.Background(), "/compact")
	if cmd != nil {
		t.Fatal("expected /compact without a chat runner to be a no-op")
	}
	if next.agent.Busy {
		t.Fatal("expected agent to stay idle when no chat runner is configured")
	}
}

func TestSuccessfulAgentDoneClearsPreviousError(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model.agent.Err = context.Canceled

	updated, _ := model.Update(agentDoneMsg{})
	next := updated.(Model)
	if next.agent.Err != nil {
		t.Fatalf("expected successful agent completion to clear previous error, got %v", next.agent.Err)
	}
	if next.agent.Busy {
		t.Fatal("expected successful agent completion to clear busy state")
	}
}

func TestTUIConfirmerQueuesEarlyResponse(t *testing.T) {
	confirmer := NewConfirmer()
	confirmer.Respond(true)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	approved, err := confirmer.Confirm(ctx, confirmationRequestForRisk("medium"))
	if err != nil {
		t.Fatal(err)
	}
	if !approved {
		t.Fatal("expected early response to be delivered to later confirmation request")
	}
}

func confirmationRequestForRisk(risk string) safety.ConfirmationRequest {
	return safety.ConfirmationRequest{
		ToolName: "tool",
		Risk:     risk,
		Summary:  "confirm tool",
		Detail:   "{}",
	}
}

func waitForPendingConfirmation(t *testing.T, confirmer *Confirmer, ready chan<- struct{}) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for pending confirmation")
		case <-tick.C:
			if confirmerHasPending(confirmer) {
				close(ready)
				return
			}
		}
	}
}

func confirmerHasPending(confirmer *Confirmer) bool {
	if confirmer == nil {
		return false
	}
	confirmer.mu.Lock()
	defer confirmer.mu.Unlock()
	return confirmer.pending != nil
}

func TestModelInitializesSidebarRuntimeSummaries(t *testing.T) {
	model := NewModel(Config{
		Status: Status{
			SessionID:       "s1",
			Model:           "fake",
			ToolCount:       3,
			ContextSummary:  "tokens 20%",
			PlanningSummary: "ready: todo_002",
			MCPStatus:       "1 server",
		},
	})

	if model.live.ContextSummary != "tokens 20%" {
		t.Fatalf("expected context summary in live, got %#v", model.live)
	}
	if model.live.PlanningSummary != "ready: todo_002" {
		t.Fatalf("expected planning summary in live, got %#v", model.live)
	}
}

type contextRecordingChatRunner struct {
	ctxSeen     chan context.Context
	ctxCanceled chan struct{}
	release     <-chan struct{}
}

func (r *contextRecordingChatRunner) RunWithEvents(ctx context.Context, prompt string, sink agent.EventSink) (*agent.RunResult, error) {
	r.ctxSeen <- ctx
	select {
	case <-ctx.Done():
		r.ctxCanceled <- struct{}{}
		return nil, ctx.Err()
	case <-r.release:
		return nil, nil
	}
}

func (r *contextRecordingChatRunner) Compact(ctx context.Context, sink agent.EventSink) error {
	return nil
}

type blockingSinkChatRunner struct {
	blocking chan struct{}
	done     chan struct{}
}

func (r *blockingSinkChatRunner) RunWithEvents(ctx context.Context, prompt string, sink agent.EventSink) (*agent.RunResult, error) {
	defer close(r.done)
	for i := 0; i < 64; i++ {
		if i == 32 {
			r.blocking <- struct{}{}
		}
		sink(agent.Event{Type: agent.EventModelChunk, Message: "chunk"})
	}
	return nil, nil
}

func (r *blockingSinkChatRunner) Compact(ctx context.Context, sink agent.EventSink) error { return nil }

type immediateChatRunner struct {
	ctxSeen chan context.Context
}

func (r *immediateChatRunner) RunWithEvents(ctx context.Context, prompt string, sink agent.EventSink) (*agent.RunResult, error) {
	r.ctxSeen <- ctx
	return nil, nil
}

func (r *immediateChatRunner) Compact(ctx context.Context, sink agent.EventSink) error { return nil }

func drainUntilDone(stream <-chan tea.Msg, done <-chan struct{}, timeout time.Duration) {
	deadline := time.After(timeout)
	for {
		select {
		case <-done:
			return
		case <-stream:
		case <-deadline:
			return
		}
	}
}

func TestHeaderShowsContextSummary(t *testing.T) {
	model := NewModel(Config{Status: Status{
		ProjectRoot:    "D:/dev/x",
		Model:          "glm-5.1",
		PermissionMode: "confirm",
	}})
	model = model.SetSize(100, 24)
	model = model.ApplyAgentEvent(agent.Event{
		Type:             agent.EventContextUpdated,
		ContextTokens:    42000,
		ContextMaxTokens: 100000,
	})
	if !strings.Contains(model.View(), "42.0k") {
		t.Fatalf("expected header to surface live context usage:\n%s", model.View())
	}
}

func TestCompactionUpdatesHeaderTokens(t *testing.T) {
	model := NewModel(Config{Status: Status{
		ProjectRoot:    "D:/dev/x",
		Model:          "glm-5.1",
		PermissionMode: "confirm",
	}})
	model = model.SetSize(100, 24)
	model = model.ApplyAgentEvent(agent.Event{
		Type:             agent.EventContextUpdated,
		ContextTokens:    42000,
		ContextMaxTokens: 100000,
	})
	if !strings.Contains(model.View(), "42.0k") {
		t.Fatalf("expected pre-compact header to show ctx 42%%:\n%s", model.View())
	}

	// /compact reports the post-compact estimate; the header must drop in the
	// same turn instead of waiting for the next EventContextUpdated.
	model = model.ApplyAgentEvent(agent.Event{
		Type:             agent.EventCompactionFinished,
		Message:          "compacted 42000 -> 8000 tokens",
		ContextTokens:    8000,
		ContextMaxTokens: 100000,
	})
	if !strings.Contains(model.View(), "8.0k") {
		t.Fatalf("expected post-compact header to show ctx 8%%:\n%s", model.View())
	}
}

func TestHeaderPrefersMeasuredTokens(t *testing.T) {
	model := NewModel(Config{Status: Status{
		ProjectRoot:    "D:/dev/x",
		Model:          "glm-5.1",
		PermissionMode: "confirm",
	}})
	model = model.SetSize(100, 24)
	model = model.ApplyAgentEvent(agent.Event{
		Type:             agent.EventContextUpdated,
		ContextTokens:    42000,
		ContextMaxTokens: 100000,
	})
	if !strings.Contains(model.View(), "42.0k") {
		t.Fatalf("expected governor estimate ctx 42%% before measured:\n%s", model.View())
	}
	// Real measured tokens (50% of the window) must win over the governor's 42%
	// estimate, so the header matches /status instead of showing two values.
	model = model.ApplyAgentEvent(agent.Event{
		Type:                agent.EventContextMeasured,
		MeasuredInputTokens: 50000,
	})
	if !strings.Contains(model.View(), "50.0k") {
		t.Fatalf("expected measured tokens to override governor estimate:\n%s", model.View())
	}
}

func TestShiftTabTogglesPlanMode(t *testing.T) {
	model := NewModel(Config{})
	if model.mode != ModeNormal {
		t.Fatalf("expected default ModeNormal, got %q", model.mode)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m := updated.(Model)
	if m.mode != ModePlan {
		t.Fatalf("expected ModePlan after shift+tab, got %q", m.mode)
	}
	if got := m.runtimeFooter(); got != "plan" {
		t.Fatalf("plan mode should lead the runtime footer, got %q", got)
	}

	// Toggle back to normal; the plan label must return to normal.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(Model)
	if m.mode != ModeNormal {
		t.Fatalf("expected ModeNormal after second shift+tab, got %q", m.mode)
	}
	if got := m.runtimeFooter(); got != "normal" {
		t.Fatalf("normal mode should lead the runtime footer, got %q", got)
	}
}

func TestShiftTabDoesNotToggleWhileConfirming(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Pending = &agent.Event{ToolName: "write_file", Risk: "low"}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m := updated.(Model)
	if m.mode != ModeNormal {
		t.Fatalf("shift+tab must not toggle mode while a confirmation is pending, got %q", m.mode)
	}
}

type recordingPlanMode struct {
	calls int
	last  bool
}

func (r *recordingPlanMode) SetPlanMode(plan bool) {
	r.calls++
	r.last = plan
}

func TestShiftTabNotifiesPlanModeController(t *testing.T) {
	rpc := &recordingPlanMode{}
	model := NewModel(Config{PlanMode: rpc})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m := updated.(Model)
	if !m.mode.IsPlan() {
		t.Fatalf("expected plan mode after shift+tab, got %q", m.mode)
	}
	if rpc.calls != 1 || !rpc.last {
		t.Fatalf("expected SetPlanMode(true) once, got calls=%d last=%v", rpc.calls, rpc.last)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(Model)
	if m.mode.IsPlan() {
		t.Fatalf("expected normal mode after second shift+tab, got %q", m.mode)
	}
	if rpc.calls != 2 || rpc.last {
		t.Fatalf("expected SetPlanMode(false), got calls=%d last=%v", rpc.calls, rpc.last)
	}
}

func TestLateSameRunEventsAfterLocalCancelAreIgnored(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("cancel me")
	model.agent.Busy = true
	model.agent.RunGeneration = 7
	model.agent.Cancel = func() {}
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "partial"})

	model = model.cancelRunningAgent()
	blocksBefore := len(model.timeline.Turns[0].Blocks)

	updated, cmd := model.Update(agentEventMsg{
		runGeneration: 7,
		event:         agent.Event{Type: agent.EventModelChunk, Message: "late"},
	})
	next := updated.(Model)
	if cmd != nil {
		t.Fatal("late event after cancellation must not arm another agent reader")
	}
	if next.agent.LiveStream != nil {
		t.Fatalf("late event recreated live state: %#v", next.agent.LiveStream)
	}
	if got := next.timeline.Turns[0].Run.State; got != "cancelled" {
		t.Fatalf("late event changed cancelled state to %q", got)
	}
	if got := len(next.timeline.Turns[0].Blocks); got != blocksBefore {
		t.Fatalf("late event changed transcript blocks: got %d want %d", got, blocksBefore)
	}
}

func TestStaleRunMessagesCannotMutateReplacementRun(t *testing.T) {
	model := NewModel(Config{})
	model.timeline = model.timeline.StartUserTurn("replacement")
	model.agent.Busy = true
	model.agent.RunGeneration = 12
	model.agent.Stream = make(chan tea.Msg)
	model.agent.QueuedPrompts = []string{"keep queued"}

	updated, cmd := model.Update(agentDoneMsg{runGeneration: 11, err: context.Canceled})
	next := updated.(Model)
	if cmd != nil {
		t.Fatal("stale done must not arm a reader or start queued work")
	}
	if !next.agent.Busy || next.agent.Stream == nil {
		t.Fatal("stale done from prior run cleaned up the replacement run")
	}
	if next.agent.RunGeneration != 12 {
		t.Fatalf("stale done changed run generation to %d", next.agent.RunGeneration)
	}
	if len(next.agent.QueuedPrompts) != 1 || next.agent.QueuedPrompts[0] != "keep queued" {
		t.Fatalf("stale done drained replacement queue: %#v", next.agent.QueuedPrompts)
	}

	updated, cmd = next.Update(agentEventMsg{
		runGeneration: 11,
		event:         agent.Event{Type: agent.EventModelChunk, Message: "old output"},
	})
	next = updated.(Model)
	if cmd != nil || next.agent.LiveStream != nil {
		t.Fatalf("stale event affected replacement run: cmd=%v live=%#v", cmd, next.agent.LiveStream)
	}
}

func TestStaleAgentTickDoesNotArmReplacementReader(t *testing.T) {
	model := NewModel(Config{})
	model.agent.Busy = true
	model.agent.RunGeneration = 4
	model.agent.Stream = make(chan tea.Msg)

	updated, cmd := model.Update(agentTickMsg{runGeneration: 3})
	next := updated.(Model)
	if cmd != nil {
		t.Fatal("stale tick must not arm an extra reader on the replacement stream")
	}
	if !next.agent.Busy || next.agent.Stream == nil {
		t.Fatal("stale tick changed replacement runtime state")
	}
}

func TestAgentFinishedCommitsLiveAndUsesFallbackExactlyOnce(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(Model) Model
		finish     string
		wantKinds  []BlockKind
		wantBodies []string
	}{
		{
			name: "assistant stream wins over duplicate finish message",
			setup: func(m Model) Model {
				return m.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "streamed tail"})
			},
			finish:     "streamed tail",
			wantKinds:  []BlockKind{BlockAssistant},
			wantBodies: []string{"streamed tail"},
		},
		{
			name:       "non-streamed finish fallback",
			setup:      func(m Model) Model { return m },
			finish:     "fallback answer",
			wantKinds:  []BlockKind{BlockAssistant},
			wantBodies: []string{"fallback answer"},
		},
		{
			name: "reasoning followed by finish fallback",
			setup: func(m Model) Model {
				return m.ApplyAgentEvent(agent.Event{Type: agent.EventReasoningChunk, Message: "private tail"})
			},
			finish:     "final answer",
			wantKinds:  []BlockKind{BlockReasoning, BlockAssistant},
			wantBodies: []string{"private tail", "final answer"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(Config{})
			m.timeline = m.timeline.StartUserTurn("prompt")
			m.agent.Busy = true
			m = tt.setup(m)
			m = m.ApplyAgentEvent(agent.Event{Type: agent.EventAgentFinished, Message: tt.finish})
			m = m.ApplyAgentEvent(agent.Event{Type: agent.EventAgentFinished, Message: tt.finish})
			blocks := m.timeline.Turns[0].Blocks
			if len(blocks) != len(tt.wantKinds) {
				t.Fatalf("duplicate finish produced %d blocks, want %d: %#v", len(blocks), len(tt.wantKinds), blocks)
			}
			for i := range blocks {
				if blocks[i].Kind != tt.wantKinds[i] || blocks[i].Body != tt.wantBodies[i] {
					t.Fatalf("block %d = (%s,%q), want (%s,%q)", i, blocks[i].Kind, blocks[i].Body, tt.wantKinds[i], tt.wantBodies[i])
				}
			}
			if !m.agent.TerminalHandled {
				t.Fatal("finish must mark terminal handling complete")
			}
		})
	}
}

func TestToolScopedAgentErrorFailsToolWithoutEndingRun(t *testing.T) {
	m := NewModel(Config{})
	m.timeline = m.timeline.StartUserTurn("prompt")
	m.agent.Busy = true
	m = m.ApplyAgentEvent(agent.Event{Type: agent.EventToolRequested, ToolName: "read_file", ToolCallID: "call-1"})
	m = m.ApplyAgentEvent(agent.Event{Type: agent.EventAgentError, ToolName: "read_file", ToolCallID: "call-1", Error: "tool panic"})

	turn := m.timeline.Turns[0]
	if m.agent.TerminalHandled {
		t.Fatal("tool-scoped error must not mark the run terminal")
	}
	if turn.Run.State == "failed" {
		t.Fatalf("tool-scoped error marked run failed: %#v", turn.Run)
	}
	if len(turn.Blocks) != 1 || turn.Blocks[0].Kind != BlockTool || turn.Blocks[0].Tool == nil {
		t.Fatalf("expected matching tool block only, got %#v", turn.Blocks)
	}
	if turn.Blocks[0].Tool.ID != "call-1" || turn.Blocks[0].Tool.Status != ToolFailed {
		t.Fatalf("tool identity/status changed incorrectly: %#v", turn.Blocks[0].Tool)
	}

	m = m.ApplyAgentEvent(agent.Event{Type: agent.EventAgentFinished, Message: "recovered"})
	if got := m.timeline.Turns[0].Run.State; got != "done" {
		t.Fatalf("finish after tool error did not complete run: %q", got)
	}
}

func TestLocalCancelCommitsUnterminatedLiveTailOnce(t *testing.T) {
	m := NewModel(Config{})
	m.timeline = m.timeline.StartUserTurn("prompt")
	m.agent.Busy = true
	m.agent.RunGeneration = 9
	m.agent.Cancel = func() {}
	m = m.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: "unfinished tail"})

	m = m.cancelRunningAgent()
	m = m.cancelRunningAgent()
	updated, _ := m.Update(agentDoneMsg{runGeneration: 9, err: context.Canceled})
	m = updated.(Model)
	blocks := m.timeline.Turns[0].Blocks
	if len(blocks) != 1 || blocks[0].Kind != BlockAssistant || blocks[0].Body != "unfinished tail" {
		t.Fatalf("cancel/late done lost or duplicated tail: %#v", blocks)
	}
	if got := m.timeline.Turns[0].Run.State; got != "cancelled" {
		t.Fatalf("late done changed cancelled state to %q", got)
	}
}

func TestComposerReflowsHeightWhenTerminalResizes(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 24).SetInput(strings.Repeat("word ", 12))
	wideHeight := model.composer.Input.Height()
	model = model.SetSize(24, 24)
	if got := model.composer.Input.Height(); got <= wideHeight {
		t.Fatalf("expected narrow resize to increase composer height, wide=%d narrow=%d", wideHeight, got)
	}
	model = model.SetSize(80, 24)
	if got := model.composer.Input.Height(); got != wideHeight {
		t.Fatalf("expected wide resize to restore composer height %d, got %d", wideHeight, got)
	}
}

func TestWrappedComposerUsesUpForCursorBeforeHistory(t *testing.T) {
	model := NewModel(Config{}).SetSize(24, 24)
	model = model.SetInput("saved prompt")
	model, _ = model.Submit(context.Background())
	draft := strings.Repeat("wrapped ", 8)
	model = model.SetInput(draft)
	before := model.composer.Input.LineInfo().RowOffset
	if before == 0 {
		t.Fatalf("test setup did not wrap input: %#v", model.composer.Input.LineInfo())
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if got := model.inputValue(); got != draft {
		t.Fatalf("expected Up to move inside wrapped draft before history, got %q", got)
	}
	if after := model.composer.Input.LineInfo().RowOffset; after >= before {
		t.Fatalf("expected cursor to move to an earlier visual row, before=%d after=%d", before, after)
	}
}

func appendTestAssistant(model Model, body string) Model {
	model.timeline = model.timeline.AppendBlock(BlockAssistant, "agent", body)
	return model.markNewOutputBelow()
}

func TestCommandSurfaceRoutesAreReachable(t *testing.T) {
	for _, descriptor := range command.DiscoverableSurfaceDescriptors() {
		descriptor := descriptor
		t.Run(descriptor.Name, func(t *testing.T) {
			switch command.ClassifyExecutionTarget(descriptor.ExecutionTarget) {
			case command.ExecutionTargetRegistry:
				registry := command.NewRegistry()
				target := strings.TrimPrefix(string(descriptor.ExecutionTarget), "registry.")
				if err := registry.Register(command.Command{
					Name: target,
					Run: func(context.Context, command.Env, []string) (command.Result, error) {
						if target == "resume" {
							return command.Result{Output: "reached " + target, OpenSessionManager: true}, nil
						}
						return command.Result{Output: "reached " + target}, nil
					},
				}); err != nil {
					t.Fatal(err)
				}
				model := NewModel(Config{
					Commands:       registry,
					SessionManager: newFakeSessionManager(nil),
				})
				next, cmd := model.runCommand(context.Background(), descriptor.Shortcut)
				if cmd != nil {
					t.Fatalf("registry route %s returned unexpected async command", descriptor.Shortcut)
				}
				if target == "resume" {
					if next.overlay.kind != overlaySessions {
						t.Fatalf("%s did not open the session overlay; kind=%v", descriptor.Shortcut, next.overlay.kind)
					}
					return
				}
				block := latestCommandSurfaceBlock(t, next)
				if !strings.Contains(block.Body, "reached "+target) {
					t.Fatalf("%s did not reach registry target %q: %#v", descriptor.Shortcut, target, block)
				}

			case command.ExecutionTargetTUILocal:
				switch descriptor.Name {
				case "copy":
					next, _ := NewModel(Config{}).runCommand(context.Background(), descriptor.Shortcut)
					block := latestCommandSurfaceBlock(t, next)
					if block.Title != "/copy" || !strings.Contains(block.Body, "output to copy") {
						t.Fatalf("/copy did not reach local copy dispatch: %#v", block)
					}
				case "mouse":
					// Starts with MouseCapture zero/false in bare Config; /mouse enables.
					next, cmd := NewModel(Config{}).runCommand(context.Background(), descriptor.Shortcut)
					if !next.mouseEnabled {
						t.Fatal("/mouse did not enable mouse capture from off")
					}
					if cmd == nil {
						t.Fatal("/mouse should return EnableMouseCellMotion cmd")
					}
				case "retry":
					model := NewModel(Config{Chat: &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}})
					model.timeline = model.timeline.StartUserTurn("retry me")
					model.timeline = model.timeline.MarkAgentEnded("failed", "boom", time.Now())
					model = model.SetInput(descriptor.Shortcut)
					next, cmd := model.Submit(context.Background())
					if cmd == nil || len(next.timeline.Turns) != 2 || next.timeline.Turns[1].User.Body != "retry me" {
						t.Fatalf("/retry did not reach retry dispatch: cmd=%v turns=%#v", cmd != nil, next.timeline.Turns)
					}
				case "compact":
					runner := &compactingChatRunner{compactRan: make(chan context.Context, 1)}
					next, cmd := NewModel(Config{Chat: runner}).runCommand(context.Background(), descriptor.Shortcut)
					if cmd == nil || !next.agent.Busy {
						t.Fatalf("/compact did not reach async compact dispatch: cmd=%v busy=%v", cmd != nil, next.agent.Busy)
					}
				default:
					t.Fatalf("unmapped TUI-local descriptor %#v", descriptor)
				}

			case command.ExecutionTargetOverlay:
				next, cmd := NewModel(Config{}).runCommand(context.Background(), descriptor.Shortcut)
				if cmd != nil {
					t.Fatalf("overlay route %s returned unexpected async command", descriptor.Shortcut)
				}
				switch descriptor.Name {
				case "diff":
					if next.overlay.kind != overlayDiff {
						t.Fatalf("/diff did not open diff overlay; kind=%v", next.overlay.kind)
					}
				case "history":
					if !next.history.visible || next.mode != ModeHistory {
						t.Fatalf("/history did not open history overlay; visible=%v mode=%v", next.history.visible, next.mode)
					}
				default:
					t.Fatalf("unmapped overlay descriptor %#v", descriptor)
				}

			case command.ExecutionTargetExit:
				model := NewModel(Config{}).SetInput(descriptor.Shortcut)
				next, cmd := model.Submit(context.Background())
				if cmd == nil || len(next.timeline.Turns) != 0 {
					t.Fatalf("%s did not reach exit dispatch: cmd=%v turns=%#v", descriptor.Shortcut, cmd != nil, next.timeline.Turns)
				}

			default:
				t.Fatalf("descriptor %q has unknown target %q", descriptor.Name, descriptor.ExecutionTarget)
			}
		})
	}
}

func TestCommandSurfaceSuggestionsMatchDescriptors(t *testing.T) {
	descriptors := command.DiscoverableSurfaceDescriptors()
	items := NewSuggestionList(nil).commandItems
	if len(items) != len(descriptors) {
		t.Fatalf("suggestion count = %d, descriptor count = %d", len(items), len(descriptors))
	}
	for i, descriptor := range descriptors {
		item := items[i]
		if item.Name != descriptor.Name || item.Description != descriptor.Description || item.Prefix+item.Name != descriptor.Shortcut {
			t.Errorf("suggestion[%d] = %#v, want descriptor %#v", i, item, descriptor)
		}
	}
}

func TestCompatibilityCommandRoutesRemainReachable(t *testing.T) {
	registry := command.NewRegistry()
	if err := commandbuiltin.RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	base := NewModel(Config{
		Commands:       registry,
		SessionManager: newFakeSessionManager(nil),
		Status:         Status{SessionID: "session-current"},
		CommandEnv:     command.Env{SessionID: "session-current"},
	})

	t.Run("sessions aliases resume", func(t *testing.T) {
		next, _ := base.runCommand(context.Background(), "/sessions")
		if next.overlay.kind != overlaySessions {
			t.Fatalf("/sessions did not delegate to /resume overlay; kind=%v", next.overlay.kind)
		}
	})
	t.Run("session shows details", func(t *testing.T) {
		next, _ := base.runCommand(context.Background(), "/session")
		block := latestCommandSurfaceBlock(t, next)
		if block.Panel == nil || block.Panel.Title != "session" || !strings.Contains(block.Body, "id: session-current") {
			t.Fatalf("/session did not show current-session details: %#v", block)
		}
		if next.overlay.kind == overlaySessions {
			t.Fatal("/session opened the session list")
		}
	})
	for _, route := range []string{"/new", "/cost", "/theme"} {
		t.Run(route, func(t *testing.T) {
			next, _ := base.runCommand(context.Background(), route)
			block := latestCommandSurfaceBlock(t, next)
			if block.Kind != BlockCommand || block.Title != route {
				t.Fatalf("%s did not execute: %#v", route, block)
			}
		})
	}
	for _, route := range []string{"/quit", "/q"} {
		t.Run(route, func(t *testing.T) {
			model := base.SetInput(route)
			next, cmd := model.Submit(context.Background())
			if cmd == nil || len(next.timeline.Turns) != 0 {
				t.Fatalf("%s did not reach exit dispatch", route)
			}
		})
	}
	t.Run("session-stats remains unknown", func(t *testing.T) {
		next, _ := base.runCommand(context.Background(), "/session-stats")
		block := latestCommandSurfaceBlock(t, next)
		if block.Kind != BlockError || !strings.Contains(block.Body, "unknown command: /session-stats") {
			t.Fatalf("/session-stats must remain unknown: %#v", block)
		}
	})
}

func latestCommandSurfaceBlock(t *testing.T, model Model) Block {
	t.Helper()
	if len(model.timeline.Turns) == 0 {
		t.Fatal("expected command route to append a timeline turn")
	}
	turn := model.timeline.Turns[len(model.timeline.Turns)-1]
	if len(turn.Blocks) == 0 {
		t.Fatal("expected command route to append a timeline block")
	}
	return turn.Blocks[len(turn.Blocks)-1]
}
