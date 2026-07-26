package agent

import (
	"strings"
	"testing"
)

func TestTextGuardGateAExactRepeatedChunks(t *testing.T) {
	g := newTextGuard(textGuardConfig{MaxRepeatedTextChunks: 2, MaxRepeatedTextSubstrings: 0})
	// First chunk seeds the run.
	if d, _ := g.Saw("x", 1); d {
		t.Fatal("first chunk must not trip")
	}
	// Second identical chunk: first repeat, below threshold 2.
	if d, _ := g.Saw("x", 2); d {
		t.Fatal("second identical chunk must not trip at threshold 2")
	}
	// Third identical chunk: second consecutive repeat trips.
	d, healthy := g.Saw("x", 3)
	if !d {
		t.Fatal("expected gate A to trip on the third identical chunk (threshold 2)")
	}
	if healthy != 1 {
		t.Fatalf("healthyLen = %d, want 1 (keep the first copy of the run)", healthy)
	}
	// Tripped guard stays tripped regardless of later input.
	if d2, _ := g.Saw("different", 100); !d2 {
		t.Fatal("tripped guard must stay tripped")
	}
}

func TestTextGuardGateAResetsOnDifferentChunk(t *testing.T) {
	g := newTextGuard(textGuardConfig{MaxRepeatedTextChunks: 2, MaxRepeatedTextSubstrings: 0})
	g.Saw("foo", 3) // seed
	if d, _ := g.Saw("foo", 6); d {
		t.Fatal("second identical must not trip at threshold 2")
	}
	g.Saw("bar", 9) // different chunk resets the run
	// After reset, repeats must start the count over.
	if d, _ := g.Saw("foo", 12); d {
		t.Fatal("first foo after reset must not trip")
	}
	if d, _ := g.Saw("foo", 15); d {
		t.Fatal("first repeat after reset must not trip; run should have reset")
	}
}

func TestTextGuardGateBSubstringProbe(t *testing.T) {
	g := newTextGuard(textGuardConfig{MaxRepeatedTextChunks: 0, MaxRepeatedTextSubstrings: 2})
	phrase := strings.Repeat("a", textDegenerationProbeLen) // exactly one probe window
	// First occurrence seeds the window; no earlier text to match.
	if d, _ := g.Saw(phrase, len(phrase)); d {
		t.Fatal("first occurrence must not trip")
	}
	// Second occurrence: the probe re-appears in the prior window — first hit.
	if d, _ := g.Saw(phrase, 2*len(phrase)); d {
		t.Fatal("first substring hit must not trip at threshold 2")
	}
	// Third occurrence: second consecutive hit trips.
	if d, _ := g.Saw(phrase, 3*len(phrase)); !d {
		t.Fatal("expected gate B to trip on the second substring hit")
	}
}

func TestTextGuardDoesNotTripOnVariedOutput(t *testing.T) {
	g := newTextGuard(textGuardConfig{MaxRepeatedTextChunks: 16, MaxRepeatedTextSubstrings: 3})
	chunks := []string{
		"Sure, ", "I'll ", "read ", "the ", "file ", "and ", "then ", "edit ",
		"the ", "parser ", "to ", "handle ", "the ", "new ", "syntax ", "correctly.",
	}
	cum := 0
	for _, c := range chunks {
		cum += len(c)
		if d, _ := g.Saw(c, cum); d {
			t.Fatalf("varied output falsely tripped the breaker at %q", c)
		}
	}
}

func TestTextGuardIgnoresEmptyContent(t *testing.T) {
	g := newTextGuard(textGuardConfig{MaxRepeatedTextChunks: 2, MaxRepeatedTextSubstrings: 2})
	if d, _ := g.Saw("", 0); d {
		t.Fatal("empty chunk must not trip")
	}
}

func TestReasoningTextGuardConfigDisablesPhraseGate(t *testing.T) {
	cfg := reasoningTextGuardConfig(textGuardConfig{
		MaxRepeatedTextChunks:     16,
		MaxRepeatedTextSubstrings: 3,
	})
	if cfg.MaxRepeatedTextSubstrings != 0 {
		t.Fatalf("reasoning must disable Gate B, got %d", cfg.MaxRepeatedTextSubstrings)
	}
	if cfg.MaxRepeatedTextChunks < 64 {
		t.Fatalf("reasoning Gate A threshold too low: %d", cfg.MaxRepeatedTextChunks)
	}
}

func TestReasoningTextGuardDoesNotTripOnRestatedThoughts(t *testing.T) {
	// Healthy thinking often restates the user ask and options — Gate B used
	// to cancel these mid-stream. With reasoning config it must stay quiet.
	g := newTextGuard(reasoningTextGuardConfig(textGuardConfig{
		MaxRepeatedTextChunks:     16,
		MaxRepeatedTextSubstrings: 3,
	}))
	// A phrase that would trip Gate B at threshold 3 on answer-text defaults.
	phrase := strings.Repeat("The user wants a directory listing. ", 4)
	cum := 0
	for i := 0; i < 6; i++ {
		cum += len(phrase)
		if d, _ := g.Saw(phrase, cum); d {
			t.Fatalf("restated thinking falsely tripped at iteration %d", i)
		}
	}
}
