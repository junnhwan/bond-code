package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/junnhwan/bond-code/internal/fsx"
)

type tuiPreferences struct {
	Verbose        bool   `json:"verbose"`
	Accent         string `json:"accent,omitempty"`
	ShowThinking   bool   `json:"show_thinking,omitempty"`
	ShowTimestamps bool   `json:"show_timestamps,omitempty"`
	// ShowToolDetails defaults to true at runtime (see coalesceDisplayPrefs) so
	// existing users keep seeing completed tool calls; the JSON tag stays omitempty
	// so a false persists explicitly rather than being read back as zero-value-true.
	HideToolDetails bool `json:"hide_tool_details,omitempty"`
	ShowScrollbar   bool `json:"show_scrollbar,omitempty"`
	// Legacy rail_mode is ignored on load (sidebar/rail removed from Grok chrome).
	LegacyRailMode string `json:"rail_mode,omitempty"`
}

func loadTUIPreferences(path string) tuiPreferences {
	if path == "" {
		return tuiPreferences{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tuiPreferences{}
	}
	var prefs tuiPreferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return tuiPreferences{}
	}
	return prefs
}

func saveTUIPreferences(path string, prefs tuiPreferences) error {
	if path == "" {
		return nil
	}
	// Drop legacy rail field on save so preferences stay clean.
	prefs.LegacyRailMode = ""
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fsx.WriteFileAtomic(path, append(data, '\n'), 0o600)
}
