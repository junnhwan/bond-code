package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThemeNoArgListsPresetsAndMarksCurrent(t *testing.T) {
	defer ApplyAccent(AccentPresets[0].Color)
	m := NewModel(Config{})
	m, _ = m.runThemeCommand(nil)

	last := m.timeline.Turns[len(m.timeline.Turns)-1].Blocks[len(m.timeline.Turns[len(m.timeline.Turns)-1].Blocks)-1]
	body := last.Body
	// Structured panel: active marker + multi-row accents (not "magenta *").
	if !strings.Contains(body, "active") && !strings.Contains(body, "▸") {
		t.Fatalf("expected the active preset marked, got %q", body)
	}
	if !strings.Contains(body, "magenta") {
		t.Fatalf("expected magenta listed, got %q", body)
	}
	if !strings.Contains(body, "blue") || !strings.Contains(body, "green") {
		t.Fatalf("expected presets listed, got %q", body)
	}
	if strings.HasPrefix(strings.TrimSpace(body), "accents:") {
		t.Fatalf("must not use flat CSV dump, got %q", body)
	}
}

func TestThemeWithArgAppliesAndPersists(t *testing.T) {
	defer ApplyAccent(AccentPresets[0].Color)
	dir := t.TempDir()
	prefsPath := filepath.Join(dir, "prefs.json")

	m := NewModel(Config{PreferencesPath: prefsPath})
	m, _ = m.runThemeCommand([]string{"green"})

	if m.accent != "green" {
		t.Fatalf("expected accent green, got %q", m.accent)
	}
	data, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatalf("read prefs: %v", err)
	}
	if !strings.Contains(string(data), `"accent": "green"`) {
		t.Fatalf("expected accent persisted to prefs, got %s", data)
	}
}

func TestThemeUnknownArgErrors(t *testing.T) {
	defer ApplyAccent(AccentPresets[0].Color)
	m := NewModel(Config{})
	m, _ = m.runThemeCommand([]string{"nope"})

	last := m.timeline.Turns[len(m.timeline.Turns)-1].Blocks[len(m.timeline.Turns[len(m.timeline.Turns)-1].Blocks)-1]
	if last.Kind != BlockError {
		t.Fatalf("expected an error block for unknown accent, got %v", last.Kind)
	}
}
