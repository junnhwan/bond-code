package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// profileReportInterval bounds how often the profiler emits a summary line.
const profileReportInterval = 2 * time.Second

// profiler is an opt-in frame profiler, enabled by BONDCODE_PROFILE. It
// accumulates per-phase durations and event counts across frames and writes a
// one-line summary every profileReportInterval so a laggy run surfaces exactly
// which phase is slow (View total / timeline / live / header) plus the
// agent-event and stream-flush rates.
//
// Nil-safe: every method is a no-op on a nil *profiler, and phase() returns a
// nil *phaseTimer whose done() is also a no-op. View therefore pays only a
// nil-receiver check per measured site when profiling is off.
//
// Usage:
//
//	BONDCODE_PROFILE=1          → ./bondcode.profile.log
//	BONDCODE_PROFILE=<path>     → that file
//
// Reproduce the laggy scenario, then read the file: each line is one
// profileReportInterval window of per-frame averages and event rates.
type profiler struct {
	mu     sync.Mutex
	file   *os.File
	frames int
	phases map[string]time.Duration
	counts map[string]int
	since  time.Time
}

func newProfiler() *profiler {
	val := os.Getenv("BONDCODE_PROFILE")
	if val == "" {
		return nil
	}
	path := val
	if path == "1" {
		path = "bondcode.profile.log"
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bondcode profile: cannot open %s: %v\n", path, err)
		return nil
	}
	fmt.Fprintf(os.Stderr, "bondcode profile: writing to %s\n", path)
	return &profiler{
		file:   f,
		phases: map[string]time.Duration{},
		counts: map[string]int{},
		since:  time.Now(),
	}
}

// phaseTimer records one phase's elapsed time on done(). Nil-safe.
type phaseTimer struct {
	p     *profiler
	name  string
	start time.Time
}

// phase starts a named phase timer. Nil-safe on a nil *profiler (returns nil).
func (p *profiler) phase(name string) *phaseTimer {
	if p == nil {
		return nil
	}
	return &phaseTimer{p: p, name: name, start: time.Now()}
}

// done records the phase's elapsed time. Nil-safe.
func (pt *phaseTimer) done() {
	if pt == nil {
		return
	}
	d := time.Since(pt.start)
	pt.p.mu.Lock()
	pt.p.phases[pt.name] += d
	pt.p.counts[pt.name]++
	pt.p.mu.Unlock()
}

// count bumps a frequency counter (no duration). Used for event/flush rates.
func (p *profiler) count(name string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.counts[name]++
	p.mu.Unlock()
}

// countMsg bumps a per-msg-type counter so one run attributes Update/View
// frequency to a specific msg kind. The gap between frames/s and the named
// counters (event/flush) is exactly what this is meant to explain — e.g. an
// unexpected spinner.TickMsg or stream-chunk storm.
func (p *profiler) countMsg(msg any) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.counts["msg:"+fmt.Sprintf("%T", msg)]++
	p.mu.Unlock()
}

// Close releases the profile file. Nil-safe. The program owns the profiler
// for its whole lifetime (no per-model teardown), so this mainly exists for
// tests; production relies on process exit.
func (p *profiler) Close() error {
	if p == nil {
		return nil
	}
	return p.file.Close()
}

// frameSince records one View() invocation and emits a summary periodically.
// Called once per frame via defer from View.
func (p *profiler) frameSince(start time.Time) {
	if p == nil {
		return
	}
	d := time.Since(start)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.frames++
	p.phases["view_total"] += d
	p.counts["view_total"]++
	if time.Since(p.since) >= profileReportInterval {
		p.emitLocked()
		p.since = time.Now()
	}
}

func (p *profiler) emitLocked() {
	window := time.Since(p.since)
	if window <= 0 {
		window = profileReportInterval
	}
	sec := window.Seconds()
	fps := float64(p.frames) / sec
	avg := func(name string) time.Duration {
		c := p.counts[name]
		if c == 0 {
			return 0
		}
		return p.phases[name] / time.Duration(c)
	}
	rate := func(name string) float64 {
		return float64(p.counts[name]) / sec
	}
	fmt.Fprintf(p.file, "%s | frames=%d (%.1f/s) | view_avg=%s timeline=%s live=%s header=%s | events=%.1f/s flushes=%.1f/s\n",
		time.Now().Format("15:04:05.000"),
		p.frames, fps,
		avg("view_total"), avg("timeline"), avg("live"), avg("header"),
		rate("event"), rate("flush"),
	)
	// Per-msg-type breakdown so the frames/s total can be attributed to a
	// specific msg kind (spinner tick, stream chunk, flush, key, …). Sorted
	// descending by rate so the loud source is first.
	type kv struct {
		name string
		rate float64
	}
	var msgs []kv
	for k, c := range p.counts {
		if !strings.HasPrefix(k, "msg:") {
			continue
		}
		msgs = append(msgs, kv{strings.TrimPrefix(k, "msg:"), float64(c) / sec})
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].rate > msgs[j].rate })
	if len(msgs) > 0 {
		parts := make([]string, 0, len(msgs))
		for _, m := range msgs {
			parts = append(parts, fmt.Sprintf("%s=%.1f/s", m.name, m.rate))
		}
		fmt.Fprintf(p.file, "  msgs: %s\n", strings.Join(parts, " "))
	}
	p.frames = 0
	for k := range p.phases {
		delete(p.phases, k)
	}
	for k := range p.counts {
		delete(p.counts, k)
	}
}
