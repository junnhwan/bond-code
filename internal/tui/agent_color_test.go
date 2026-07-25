package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAgentColorStableAndNonEmpty(t *testing.T) {
	for _, name := range []string{"", "research", "coder", "reviewer", "orchestrator"} {
		c := agentColor(name)
		if string(c) == "" {
			t.Fatalf("agentColor(%q) returned an empty color", name)
		}
	}
	// Same name must always hash to the same color across calls.
	want := agentColor("research")
	for i := 0; i < 5; i++ {
		if agentColor("research") != want {
			t.Fatalf("agentColor is not deterministic for research")
		}
	}
}

func TestAgentColorStaysInPalette(t *testing.T) {
	palette := map[lipgloss.Color]bool{}
	for _, c := range agentPalette {
		palette[c] = true
	}
	for _, name := range []string{"research", "coder", "reviewer", "orchestrator", "agent"} {
		if !palette[agentColor(name)] {
			t.Fatalf("agentColor(%q) returned a color outside the palette", name)
		}
	}
}
