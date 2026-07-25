package tui

import (
	"path/filepath"
	"testing"
	"time"
)

// TestToggleThinkingFlipsStateAndPersists checks the toggle mutates the field,
// fires a toast, and writes through to preferences (verified by reloading).
func TestToggleThinkingFlipsStateAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prefs.json")
	model := NewModel(Config{PreferencesPath: path})
	if model.showThinking {
		t.Fatal("expected showThinking to default off")
	}
	model = model.toggleThinking()
	if !model.showThinking {
		t.Fatal("expected showThinking on after toggle")
	}
	if len(model.toasts) != 1 {
		t.Fatalf("expected a toast from the toggle, got %d", len(model.toasts))
	}
	// Reload from the same path: the choice must survive.
	loaded := loadTUIPreferences(path)
	if !loaded.ShowThinking {
		t.Fatal("expected show_thinking to persist")
	}
}

// TestToggleToolDetailsDefaultsOn guards the "existing users keep seeing tool
// calls" invariant: an empty prefs file yields showToolDetails=true, and toggling
// writes hide_tool_details=true rather than relying on a zero bool.
func TestToggleToolDetailsDefaultsOnAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prefs.json")
	model := NewModel(Config{PreferencesPath: path})
	if !model.showToolDetails {
		t.Fatal("expected showToolDetails to default ON")
	}
	model = model.toggleToolDetails()
	if model.showToolDetails {
		t.Fatal("expected showToolDetails off after toggle")
	}
	loaded := loadTUIPreferences(path)
	if !loaded.HideToolDetails {
		t.Fatal("expected hide_tool_details to persist as true")
	}
	// And reloading reproduces the off state.
	if NewModel(Config{PreferencesPath: path}).showToolDetails {
		t.Fatal("expected reloaded model to have showToolDetails off")
	}
}

// TestRenderReasoningRespectsToggle checks the folded preview vs the expanded
// full render differ and the toggle selects between them.
func TestExpandedReasoningFromPersistedPreferenceOmitsRemovedLeaderHint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	if err := saveTUIPreferences(path, tuiPreferences{ShowThinking: true}); err != nil {
		t.Fatal(err)
	}
	model := NewModel(Config{PreferencesPath: path})
	if !model.showThinking {
		t.Fatal("test setup expected persisted show_thinking to load")
	}

	rendered := model.renderReasoning("first thought\nsecond thought", 80)
	if !contains(rendered, "expanded") {
		t.Fatalf("expanded reasoning should retain a truthful state label, got %q", rendered)
	}
	for _, notWant := range []string{"<leader>", "leader", "to fold"} {
		if contains(rendered, notWant) {
			t.Fatalf("expanded reasoning leaked removed fold route %q: %q", notWant, rendered)
		}
	}
}

func TestRenderReasoningRespectsToggle(t *testing.T) {
	body := "line one\nline two\nline three\nline four"
	off := NewModel(Config{})
	folded := off.renderReasoning(body, 60)
	on := NewModel(Config{})
	on.showThinking = true
	expanded := on.renderReasoning(body, 60)
	if folded == expanded {
		t.Fatal("expected folded and expanded reasoning to differ")
	}
	if !contains(folded, "· 4 lines") {
		t.Fatalf("folded preview should report line count, got: %q", folded)
	}
	if !contains(expanded, "line four") {
		t.Fatalf("expanded render should include the full body, got: %q", expanded)
	}
	if contains(folded, "line four") {
		t.Fatal("folded preview should NOT include the 4th line")
	}
}

// TestRenderBlockHidesCompletedToolWhenDensityOff checks that turning tool
// details off drops completed tool blocks entirely (the renderBlock path the
// timeline loop skips on empty output).
func TestRenderBlockHidesCompletedToolWhenDensityOff(t *testing.T) {
	block := Block{Kind: BlockTool, ID: "t1", Tool: &ToolBlock{Name: "read_file", Status: ToolDone, Output: "ok"}}
	on := NewModel(Config{})
	if on.renderBlock(block, 60) == "" {
		t.Fatal("expected completed tool to render when details are ON")
	}
	off := NewModel(Config{})
	off.showToolDetails = false
	if got := off.renderBlock(block, 60); got != "" {
		t.Fatalf("expected completed tool to be hidden when details are OFF, got %q", got)
	}
	// Running tools stay visible even when details are off.
	running := block
	running.Tool = &ToolBlock{Name: "read_file", Status: ToolRunning}
	if got := off.renderBlock(running, 60); got == "" {
		t.Fatal("expected running tool to stay visible when details are OFF")
	}
}

// TestRenderTurnTimestamp checks the optional annotation: empty without a start
// time, present with start, and carrying a duration once the turn ended.
func TestRenderTurnTimestamp(t *testing.T) {
	if got := renderTurnTimestamp(Turn{}); got != "" {
		t.Fatalf("expected empty timestamp for zero-start turn, got %q", got)
	}
	start := time.Date(2026, 7, 2, 14, 32, 0, 0, time.UTC)
	only := renderTurnTimestamp(Turn{StartedAt: start})
	if !contains(only, "14:32") {
		t.Fatalf("expected start time in annotation, got %q", only)
	}
	ended := renderTurnTimestamp(Turn{StartedAt: start, EndedAt: start.Add(90 * time.Second)})
	if !contains(ended, "14:32") || !contains(ended, "1m") {
		t.Fatalf("expected start + duration in annotation, got %q", ended)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
