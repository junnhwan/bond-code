package tui

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestDefaultThemeIsGrokNightInspired(t *testing.T) {
	// Real GrokNight values from xai-grok-pager-render/theme/groknight.rs.
	if DefaultTheme.BackgroundPanel != lipgloss.Color("#0a0a0a") {
		t.Fatalf("BackgroundPanel: want #0a0a0a, got %v", DefaultTheme.BackgroundPanel)
	}
	if DefaultTheme.BackgroundBase != lipgloss.Color("#141414") {
		t.Fatalf("BackgroundBase: want #141414, got %v", DefaultTheme.BackgroundBase)
	}
	if DefaultTheme.Accent != lipgloss.Color("#bb9af7") {
		t.Fatalf("Accent: want magenta #bb9af7, got %v", DefaultTheme.Accent)
	}
	if DefaultTheme.Accent == lipgloss.Color("#FAB283") {
		t.Fatal("default accent must not be peach")
	}
	if DefaultAccentName() != "magenta" {
		t.Fatalf("DefaultAccentName: want magenta, got %q", DefaultAccentName())
	}
	// Path is TokyoNight orange.
	if DefaultTheme.Path != lipgloss.Color("#ff9e64") {
		t.Fatalf("Path: want #ff9e64, got %v", DefaultTheme.Path)
	}
}

func TestStylesUseDefaultThemeTokens(t *testing.T) {
	assertColor := func(name string, got any, want any) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: expected %v, got %v", name, want, got)
		}
	}

	assertColor("assistant foreground", assistantStyle.GetForeground(), DefaultTheme.Text)
	assertColor("tool", toolStyle.GetForeground(), DefaultTheme.Tool)
	assertColor("path", pathStyle.GetForeground(), DefaultTheme.Path)
	assertColor("error", errorStyle.GetForeground(), DefaultTheme.Error)
	assertColor("selection", suggestionSelectedStyle.GetBackground(), DefaultTheme.Selection)
}
