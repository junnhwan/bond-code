package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestLookupAccentPreset(t *testing.T) {
	if p := LookupAccentPreset("Blue"); p == nil || p.Name != "blue" {
		t.Fatalf("expected case-insensitive match for blue, got %#v", p)
	}
	if p := LookupAccentPreset("nope"); p != nil {
		t.Fatalf("expected nil for an unknown preset, got %#v", p)
	}
}

func TestResolveAccentColorFallsBackToDefault(t *testing.T) {
	if c := ResolveAccentColor(""); c != AccentPresets[0].Color {
		t.Fatalf("expected default magenta for empty name, got %v", c)
	}
	if c := ResolveAccentColor("green"); c != lipgloss.Color("#9ece6a") {
		t.Fatalf("expected green hex, got %v", c)
	}
}

func TestApplyAccentRebuildsDerivedStyles(t *testing.T) {
	defer ApplyAccent(AccentPresets[0].Color) // restore default magenta

	green := lipgloss.Color("#9ece6a")
	ApplyAccent(green)

	if DefaultTheme.Accent != green {
		t.Fatalf("DefaultTheme.Accent = %v, want green", DefaultTheme.Accent)
	}
	// lipgloss captures the color at Foreground() call time, so the derived
	// styles must have been rebuilt — otherwise they'd still hold the old accent.
	if accentStyle.GetForeground() != green {
		t.Fatalf("accentStyle not rebuilt: %v", accentStyle.GetForeground())
	}
	if breakdownSystemStyle.GetForeground() != green {
		t.Fatalf("breakdownSystemStyle not rebuilt: %v", breakdownSystemStyle.GetForeground())
	}
}
