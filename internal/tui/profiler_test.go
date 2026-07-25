package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProfilerDisabledWhenEnvUnset: without BONDCODE_PROFILE the profiler is
// nil and every helper is a safe no-op (View calls them unconditionally).
func TestProfilerDisabledWhenEnvUnset(t *testing.T) {
	t.Setenv("BONDCODE_PROFILE", "")
	if p := newProfiler(); p != nil {
		t.Fatalf("newProfiler should be nil without BONDCODE_PROFILE, got %T", p)
	}
	// Nil-receiver calls must not panic — View uses them without guards.
	var p *profiler
	pt := p.phase("timeline")
	pt.done()
	p.count("event")
	p.frameSince(time.Now())
}

// TestProfilerEmitsSummaryWhenEnabled: with BONDCODE_PROFILE pointing at a
// path, accumulating phases/counts and forcing an emit writes a summary line
// carrying the frame count and per-phase averages.
func TestProfilerEmitsSummaryWhenEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prof.log")
	t.Setenv("BONDCODE_PROFILE", path)

	p := newProfiler()
	if p == nil {
		t.Fatal("newProfiler should be non-nil with BONDCODE_PROFILE set")
	}
	defer p.Close()

	// Simulate three frames, each with a timeline phase and one event/flush.
	for i := 0; i < 3; i++ {
		start := time.Now()
		pt := p.phase("timeline")
		pt.done()
		p.count("event")
		p.count("flush")
		p.frameSince(start)
	}

	// The 2s emit interval won't have elapsed; force one to check output shape.
	p.mu.Lock()
	p.emitLocked()
	p.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("profile file not written: %v", err)
	}
	line := strings.TrimSpace(string(data))
	for _, want := range []string{"frames=", "view_avg=", "timeline=", "events=", "flushes="} {
		if !strings.Contains(line, want) {
			t.Fatalf("profile summary missing %q, got: %s", want, line)
		}
	}
}
