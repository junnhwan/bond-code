package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/command"
)

func containsSuggestion(items []Suggestion, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func TestSuggestionList(t *testing.T) {
	registry := command.NewRegistry()
	_ = registry.Register(command.Command{
		Name:        "help",
		Description: "Show help information",
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{}, nil
		},
	})
	_ = registry.Register(command.Command{
		Name:        "status",
		Description: "Show status",
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{}, nil
		},
	})
	_ = registry.Register(command.Command{
		Name:        "context",
		Description: "Show context",
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{}, nil
		},
	})

	sl := NewSuggestionList(registry)

	t.Run("initially hidden", func(t *testing.T) {
		if sl.IsVisible() {
			t.Error("expected suggestions to be hidden initially")
		}
	})

	t.Run("show all", func(t *testing.T) {
		sl.Show("")
		if !sl.IsVisible() {
			t.Error("expected suggestions to be visible after Show")
		}
		visible := sl.GetVisible("")
		if len(visible) != 18 {
			t.Errorf("expected 18 canonical builtin suggestions, got %d", len(visible))
		}
		if !containsSuggestion(visible, "retry") {
			t.Errorf("expected local retry command in suggestions, got %#v", visible)
		}
		if !containsSuggestion(visible, "copy") {
			t.Errorf("expected local copy command in suggestions, got %#v", visible)
		}
		if !containsSuggestion(visible, "mouse") {
			t.Errorf("expected local mouse command in suggestions, got %#v", visible)
		}
	})

	t.Run("filter", func(t *testing.T) {
		sl.Show("stat")
		visible := sl.GetVisible("stat")
		if len(visible) != 1 {
			t.Errorf("expected 1 suggestion matching 'stat', got %d", len(visible))
		}
		if visible[0].Name != "status" {
			t.Errorf("expected 'status', got %q", visible[0].Name)
		}
	})

	t.Run("navigation", func(t *testing.T) {
		sl.Show("")
		visible := sl.GetVisible("")

		// Initially selects first item
		if sl.GetSelectedIndex() != 0 {
			t.Errorf("expected index 0, got %d", sl.GetSelectedIndex())
		}

		// Next
		sl.SelectNext("")
		if sl.GetSelectedIndex() != 1 {
			t.Errorf("expected index 1 after Next, got %d", sl.GetSelectedIndex())
		}

		// Prev
		sl.SelectPrev("")
		if sl.GetSelectedIndex() != 0 {
			t.Errorf("expected index 0 after Prev, got %d", sl.GetSelectedIndex())
		}

		// Wrap around at start
		sl.SelectPrev("")
		if sl.GetSelectedIndex() != len(visible)-1 {
			t.Errorf("expected wrap to last index %d, got %d", len(visible)-1, sl.GetSelectedIndex())
		}

		// Wrap around at end
		sl.SelectNext("")
		if sl.GetSelectedIndex() != 0 {
			t.Errorf("expected wrap to index 0, got %d", sl.GetSelectedIndex())
		}
	})

	t.Run("get selected", func(t *testing.T) {
		sl.Show("")
		sl.selected = 1
		selected := sl.GetSelected("")
		if selected != "clear" {
			t.Errorf("expected 'clear' (second canonical item), got %q", selected)
		}
	})

	t.Run("hide", func(t *testing.T) {
		sl.Show("")
		sl.Hide()
		if sl.IsVisible() {
			t.Error("expected suggestions to be hidden after Hide")
		}
		if sl.GetSelectedIndex() != -1 {
			t.Error("expected selected index to be -1 after Hide")
		}
	})
}

func TestSuggestionsIncludeCanonicalExitCommand(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("/exit")
	model = model.updateSuggestions()

	view := model.View()
	if !strings.Contains(view, "/exit") || !strings.Contains(view, "Quit BondCode") {
		t.Fatalf("expected canonical /exit suggestion, got:\n%s", view)
	}
}

func TestSlashSuggestionsRenderCompactCommandPalette(t *testing.T) {
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
	model = model.SetInput("/")
	model = model.updateSuggestions()

	view := model.View()
	if !strings.Contains(view, "/status") || !strings.Contains(view, "Show current runtime status") {
		t.Fatalf("expected command palette suggestion, got:\n%s", view)
	}
	if strings.Contains(view, " - Show current runtime status") {
		t.Fatalf("expected compact aligned palette without dash separator, got:\n%s", view)
	}
}

func TestSlashSuggestionsFitShortTerminal(t *testing.T) {
	registry := command.NewRegistry()
	for _, name := range []string{"status", "session", "sessions", "memory", "context", "compact"} {
		if err := registry.Register(command.Command{
			Name:        name,
			Description: "Show " + name,
			Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
				return command.Result{}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	model := NewModel(Config{Commands: registry})
	model = model.SetSize(80, 8)
	model = model.SetInput("/")
	model = model.updateSuggestions()

	assertViewFits(t, model.View(), model.width, model.height)
}

func TestSlashKeyOpensCommandSuggestions(t *testing.T) {
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

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model = updated.(Model)

	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatal("typing / should open command suggestions")
	}
	view := model.View()
	if !strings.Contains(view, "/status") || !strings.Contains(view, "Show current runtime status") {
		t.Fatalf("expected registered command in slash suggestions:\n%s", view)
	}
}

func TestEscClosesEmptySlashCommandSuggestions(t *testing.T) {
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
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model = updated.(Model)
	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatal("test setup expected slash command suggestions")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)

	if model.composer.Suggestions.IsVisible() {
		t.Fatalf("expected Esc to close slash command suggestions:\n%s", model.View())
	}
	if got := model.inputValue(); got != "" {
		t.Fatalf("expected Esc to dismiss the empty slash draft, got %q", got)
	}
}

func TestEscKeepsFilteredCommandDraftWhenClosingSuggestions(t *testing.T) {
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
	model = model.SetInput("/sta")
	model = model.updateSuggestions()
	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatal("test setup expected visible suggestions")
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)

	if got := model.inputValue(); got != "/sta" {
		t.Fatalf("expected Esc to preserve filtered command draft, got %q", got)
	}
	if model.composer.Suggestions != nil && model.composer.Suggestions.IsVisible() {
		t.Fatalf("expected Esc to hide command suggestions:\n%s", model.View())
	}
}

func TestRemovedLeaderKeyLeavesSuggestionsOpen(t *testing.T) {
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
	model = model.SetInput("/")
	model = model.updateSuggestions()
	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatal("test setup expected visible suggestions")
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	model = updated.(Model)

	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatalf("removed Ctrl+X route should leave suggestions open:\n%s", model.View())
	}
	if model.leaderPending || model.whichKeyVisible {
		t.Fatalf("removed Ctrl+X route must not arm leader UI, pending=%v visible=%v", model.leaderPending, model.whichKeyVisible)
	}
}

func TestSuggestionNavigationUsesArrowKeys(t *testing.T) {
	registry := command.NewRegistry()
	for _, name := range []string{"context", "help", "status"} {
		if err := registry.Register(command.Command{
			Name:        name,
			Description: "Show " + name,
			Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
				return command.Result{}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	model := NewModel(Config{Commands: registry})
	model = model.SetInput("/")
	model = model.updateSuggestions()

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got := model.composer.Suggestions.GetSelectedIndex(); got != 1 {
		t.Fatalf("expected Down to move suggestion selection to 1, got %d", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if got := model.composer.Suggestions.GetSelectedIndex(); got != 0 {
		t.Fatalf("expected Up to move suggestion selection to 0, got %d", got)
	}
}

func TestShiftTabCyclesModeWithSuggestionsOpen(t *testing.T) {
	registry := command.NewRegistry()
	for _, name := range []string{"context", "help", "status"} {
		if err := registry.Register(command.Command{
			Name:        name,
			Description: "Show " + name,
			Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
				return command.Result{}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	model := NewModel(Config{Commands: registry})
	model = model.SetInput("/")
	model = model.updateSuggestions()
	if model.mode != ModeNormal {
		t.Fatalf("test setup expected normal mode, got %q", model.mode)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(Model)

	if model.mode != ModePlan {
		t.Fatalf("Shift+Tab should cycle to plan mode even with suggestions open, got %q", model.mode)
	}
	if got := model.composer.Suggestions.GetSelectedIndex(); got != 0 {
		t.Fatalf("Shift+Tab must not perform legacy suggestion navigation, got index %d", got)
	}
	if !model.composer.Suggestions.IsVisible() {
		t.Fatal("mode cycling should leave command suggestions available")
	}
}

func TestSuggestionModeSwitchResetsSelection(t *testing.T) {
	registry := command.NewRegistry()
	for _, name := range []string{"context", "help", "status"} {
		if err := registry.Register(command.Command{
			Name:        name,
			Description: "Show " + name,
			Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
				return command.Result{}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	for _, name := range []string{"README.md", "internal/agent/loop.go", "notes.txt"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	model := NewModel(Config{Commands: registry, Status: Status{ProjectRoot: root}})
	model = model.SetInput("/")
	model = model.updateSuggestions()
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got := model.composer.Suggestions.GetSelectedIndex(); got != 1 {
		t.Fatalf("test setup expected command selection at 1, got %d", got)
	}

	model = model.SetInput("@")
	model = model.updateSuggestions()

	if got := model.composer.Suggestions.GetSelectedIndex(); got != 0 {
		t.Fatalf("expected file suggestion mode to reset selection to 0, got %d", got)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if got := model.inputValue(); got != "@README.md " {
		t.Fatalf("expected first file suggestion to complete after mode switch, got %q", got)
	}
}

func TestSuggestionFilterChangeResetsSelection(t *testing.T) {
	registry := command.NewRegistry()
	for _, name := range []string{"skills", "status"} {
		if err := registry.Register(command.Command{
			Name:        name,
			Description: "Show " + name,
			Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
				return command.Result{}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	model := NewModel(Config{Commands: registry})
	model = model.SetInput("/")
	model = model.updateSuggestions()
	before := model.composer.Suggestions.GetSelectedIndex()
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	want := (before + 1) % len(model.composer.Suggestions.GetVisible(""))
	if got := model.composer.Suggestions.GetSelectedIndex(); got != want {
		t.Fatalf("test setup expected command selection at %d, got %d", want, got)
	}

	model = model.SetInput("/s")
	model = model.updateSuggestions()

	if got := model.composer.Suggestions.GetSelectedIndex(); got != 0 {
		t.Fatalf("expected filter change to reset selection to 0, got %d", got)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if got := model.inputValue(); got != "/skills " {
		t.Fatalf("expected first canonical filtered command to complete after filter change, got %q", got)
	}
}

func TestSubmittingUnmatchedSlashCommandHidesSuggestions(t *testing.T) {
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
	model = model.SetInput("/unknown")
	model = model.updateSuggestions()
	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatal("expected unmatched slash input to keep the suggestion controller visible before submit")
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.composer.Suggestions != nil && model.composer.Suggestions.IsVisible() {
		t.Fatalf("expected suggestions hidden after submitting unmatched slash command, view:\n%s", model.View())
	}
	if !strings.Contains(model.View(), "unknown command") {
		t.Fatalf("expected command error after submitting unmatched slash command, got:\n%s", model.View())
	}
}

func TestAtMentionSuggestionsShowLocalFileCandidates(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "agent", "loop.go"), []byte("package agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	model := NewModel(Config{Status: Status{ProjectRoot: root}})
	model = model.SetInput("@")
	model = model.updateSuggestions()

	view := model.View()
	for _, want := range []string{"@README.md", "@internal/agent/loop.go"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected file mention suggestion %q, got:\n%s", want, view)
		}
	}
}

func TestInlineAtMentionSuggestionsShowLocalFileCandidates(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}

	model := NewModel(Config{Status: Status{ProjectRoot: root}})
	model = model.SetInput("read @REA")
	model = model.updateSuggestions()

	view := model.View()
	if !strings.Contains(view, "@README.md") {
		t.Fatalf("expected inline file mention suggestion, got:\n%s", view)
	}
	if strings.Contains(view, "@notes.txt") {
		t.Fatalf("expected inline file mention filter to exclude notes.txt, got:\n%s", view)
	}
}

func TestQuotedAtMentionSuggestionsShowLocalFileCandidates(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0o600); err != nil {
		t.Fatal(err)
	}

	model := NewModel(Config{Status: Status{ProjectRoot: root}})
	model = model.SetInput(`read "@REA`)
	model = model.updateSuggestions()

	view := model.View()
	if !strings.Contains(view, "@README.md") {
		t.Fatalf("expected quoted file mention suggestion, got:\n%s", view)
	}
}

func TestCompletedInlineAtMentionDoesNotSuggestAfterFollowingText(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0o600); err != nil {
		t.Fatal(err)
	}

	model := NewModel(Config{Status: Status{ProjectRoot: root}})
	model = model.SetInput("read @README.md now")
	model = model.updateSuggestions()

	if model.composer.Suggestions != nil && model.composer.Suggestions.IsVisible() {
		t.Fatalf("expected completed inline file mention not to keep suggestions visible:\n%s", model.View())
	}
}

func TestInlineAtMentionCompletionPreservesPromptPrefix(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Status: Status{ProjectRoot: root}})
	model = model.SetInput("read @REA")
	model = model.updateSuggestions()
	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatal("test setup expected visible inline file suggestions")
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)

	if got := model.inputValue(); got != "read @README.md " {
		t.Fatalf("expected inline file completion to preserve prompt prefix, got %q", got)
	}
}

func TestAtMentionCompletionQuotesPathsWithSpaces(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "My Notes.md"), []byte("space path context"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}
	model := NewModel(Config{Status: Status{ProjectRoot: root}, Chat: runner})
	model = model.SetInput("read @My")
	model = model.updateSuggestions()
	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatal("test setup expected visible file suggestions")
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)

	if cmd != nil {
		t.Fatalf("expected Enter on file suggestion not to submit prompt, got cmd %T", cmd)
	}
	if got := model.inputValue(); got != "read @<My Notes.md> " {
		t.Fatalf("expected spaced file mention to be angle-quoted, got %q", got)
	}

	model, cmd = model.Submit(context.Background())
	if cmd == nil {
		t.Fatal("expected completed prompt to start agent")
	}
	msg := cmd()
	run, ok := msg.(runAgentMsg)
	if !ok {
		t.Fatalf("expected runAgentMsg, got %#v", msg)
	}
	if !strings.Contains(run.prompt, `<file path="My Notes.md">`) || !strings.Contains(run.prompt, "space path context") {
		t.Fatalf("expected angle-quoted spaced path to expand before agent run, got:\n%s", run.prompt)
	}
}

func TestAngledAtMentionSuggestionsAllowSpacesWhileTyping(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "My Notes.md"), []byte("space path context"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Status: Status{ProjectRoot: root}})
	model = model.SetInput("read @<My Notes")
	model = model.updateSuggestions()

	view := model.View()
	if !strings.Contains(view, "@My Notes.md") {
		t.Fatalf("expected file suggestions inside unfinished angle mention, got:\n%s", view)
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)

	if cmd != nil {
		t.Fatalf("expected Enter on angled file suggestion not to submit prompt, got cmd %T", cmd)
	}
	if got := model.inputValue(); got != "read @<My Notes.md> " {
		t.Fatalf("expected angled mention completion to replace unfinished token, got %q", got)
	}
}

func TestCompletedAngledAtMentionDoesNotKeepSuggestionsVisible(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "My Notes.md"), []byte("space path context"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Status: Status{ProjectRoot: root}})
	model = model.SetInput("read @<My Notes.md> now")
	model = model.updateSuggestions()

	if model.composer.Suggestions != nil && model.composer.Suggestions.IsVisible() {
		t.Fatalf("expected completed angled file mention not to keep suggestions visible:\n%s", model.View())
	}
}

func TestPunctuatedAtMentionCompletionPreservesPromptPrefix(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{Status: Status{ProjectRoot: root}})
	model = model.SetInput(`fix ("@REA`)
	model = model.updateSuggestions()
	if model.composer.Suggestions == nil || !model.composer.Suggestions.IsVisible() {
		t.Fatal("test setup expected visible punctuated file suggestions")
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)

	if got := model.inputValue(); got != `fix ("@README.md ` {
		t.Fatalf("expected punctuated file completion to preserve prompt prefix, got %q", got)
	}
}

func TestEnterOnInlineAtMentionCompletesWithoutSubmitting(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &immediateChatRunner{ctxSeen: make(chan context.Context, 1)}
	model := NewModel(Config{Status: Status{ProjectRoot: root}, Chat: runner})
	model = model.SetInput("read @REA")
	model = model.updateSuggestions()

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)

	if got := model.inputValue(); got != "read @README.md " {
		t.Fatalf("expected Enter on inline file suggestion to complete mention, got %q", got)
	}
	if cmd != nil {
		t.Fatalf("expected Enter on file suggestion not to submit prompt, got cmd %T", cmd)
	}
	if model.agent.Busy {
		t.Fatal("expected file mention completion not to start agent")
	}
}

func TestAtMentionSuggestionsVisibleWindowFollowsSelection(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 8; i++ {
		name := filepath.Join(root, "file0"+string(rune('0'+i))+".txt")
		if err := os.WriteFile(name, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	model := NewModel(Config{Status: Status{ProjectRoot: root}})
	model = model.SetInput("@")
	model = model.updateSuggestions()
	for i := 0; i < 6; i++ {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(Model)
	}

	view := model.View()
	if !strings.Contains(view, "@file06.txt") {
		t.Fatalf("expected file suggestion window to follow selected item, got:\n%s", view)
	}
}

func TestFileMentionSuggestionsFuzzyFiltersAndRanks(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"main.go", "main_test.go", "other.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sugs := FileMentionSuggestions(root, "main")
	names := make([]string, 0, len(sugs))
	for _, s := range sugs {
		names = append(names, s.Name)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 matches for 'main', got %v", names)
	}
	for _, n := range names {
		if n != "main.go" && n != "main_test.go" {
			t.Errorf("unexpected match %q", n)
		}
	}
}

func TestFuzzyScorePrefersPrefixOverMidWord(t *testing.T) {
	if fuzzyScore("main.go", "main") <= fuzzyScore("domain.go", "main") {
		t.Error("prefix match should score higher than mid-word match")
	}
	if fuzzyScore("abc", "xyz") != -1 {
		t.Error("non-match should score -1")
	}
}
