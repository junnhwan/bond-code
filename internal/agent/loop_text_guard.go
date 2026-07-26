package agent

import "strings"

// textGuard watches the streaming text chunks of a single model response for
// degenerate repetition and trips a circuit breaker early so the loop can
// cancel the stream and recover. It is the streaming-output analog of
// loopGuard: loopGuard only covers repeated *tool calls* (checked after the
// response is collected), so a response that degenerates into pure repeated
// text — no tool calls — would otherwise stream until the provider stops or
// the connection times out. The real-world failure mode this catches is a
// token-level stuck loop (e.g. hundreds of identical "ognition" chunks) that
// poured out for ~5 minutes with no guard firing.
//
// Two independent gates; either trips the breaker:
//   - Gate A (exact consecutive chunk repeat): the same chunk text back to
//     back, MaxRepeatedTextChunks times. Catches token-level stuck loops.
//   - Gate B (sliding substring probe): the trailing ~48-char window of the
//     accumulated answer already occurred earlier, MaxRepeatedTextSubstrings
//     times. Catches phrase/sentence loops whose chunks differ slightly.
//
// Like loopGuard it is NOT thread-safe: the chunk loop runs on a single
// goroutine, so it reuses the same non-concurrent, cfg-driven shape.
type textGuard struct {
	cfg textGuardConfig

	// Gate A state.
	lastChunk string
	chunkRun  int

	// Gate B state. accumulated is a bounded recent window of the answer used
	// only for substring searches; healthyLen is reported in answer (caller)
	// space, not accumulated space, so window trimming never shifts it.
	accumulated   string
	substringHits int

	// healthyLen is the answer length captured just before the repeating run
	// began; the caller truncates its streamed answer to this. Only meaningful
	// once tripped.
	healthyLen int
	tripped    bool
}

type textGuardConfig struct {
	MaxRepeatedTextChunks     int
	MaxRepeatedTextSubstrings int
}

func newTextGuard(cfg textGuardConfig) *textGuard {
	return &textGuard{cfg: cfg}
}

// reasoningTextGuardConfig loosens the breaker for extended-thinking streams.
//
// Visible-answer defaults (16 identical chunks / 3 substring hits) are tuned for
// stuck token loops in final text. Reasoning is *supposed* to restate goals,
// re-list options, and echo the user ask — Gate B false-trips long healthy
// thinking and cancels the stream mid-way ("recovering"). Keep only Gate A with
// a higher consecutive-identical threshold so true token stuck-loops still die
// without chopping normal multi-paragraph thinking.
func reasoningTextGuardConfig(cfg textGuardConfig) textGuardConfig {
	chunks := cfg.MaxRepeatedTextChunks
	if chunks <= 0 {
		chunks = 16
	}
	// Reasoning deltas are often tiny (1–4 tokens). 16 identical chunks is too
	// easy to hit on a repeated connector; require a longer stuck run.
	const minReasoningChunkRun = 64
	if chunks < minReasoningChunkRun {
		chunks = minReasoningChunkRun
	}
	return textGuardConfig{
		MaxRepeatedTextChunks:     chunks,
		MaxRepeatedTextSubstrings: 0, // disable phrase gate on reasoning
	}
}

const (
	// textDegenerationProbeLen is the trailing slice of the answer checked for
	// an earlier occurrence. 48 chars is long enough that legitimate prose or
	// code almost never reproduces it verbatim twice, but short enough to catch
	// phrase-level loops.
	textDegenerationProbeLen = 48
	// textDegenerationAccumulatedCap bounds the substring-search window so cost
	// stays linear-ish per chunk even on very long degenerate streams.
	textDegenerationAccumulatedCap = 8192
)

// Saw consumes one content chunk. cumLen is the caller's streamed-answer
// length AFTER writing this chunk (e.g. answer.Builder.Len()). It returns
// degenerate=true when either gate trips, plus the healthyLen to which the
// caller should truncate its streamed answer (the boundary captured just
// before the repeating run started — the first copy of the repeated token may
// be retained, which is harmless). Once tripped the guard stays tripped;
// subsequent calls are no-ops returning the same verdict.
func (g *textGuard) Saw(content string, cumLen int) (degenerate bool, healthyLen int) {
	if g.tripped {
		return true, g.healthyLen
	}
	if content == "" {
		return false, 0
	}

	// Gate A: exact consecutive chunk repeat (token-level stuck loop).
	if g.cfg.MaxRepeatedTextChunks > 0 {
		if content == g.lastChunk {
			if g.chunkRun == 0 {
				// Boundary at the start of this run: keep the first copy.
				g.healthyLen = cumLen - len(content)
			}
			g.chunkRun++
			if g.chunkRun >= g.cfg.MaxRepeatedTextChunks {
				g.tripped = true
			}
		} else {
			g.lastChunk = content
			g.chunkRun = 0
		}
	}

	// Gate B: sliding substring probe (phrase/sentence-level loops).
	if !g.tripped && g.cfg.MaxRepeatedTextSubstrings > 0 {
		g.accumulated += content
		if len(g.accumulated) > textDegenerationAccumulatedCap {
			g.accumulated = g.accumulated[len(g.accumulated)-textDegenerationAccumulatedCap:]
		}
		if len(g.accumulated) >= textDegenerationProbeLen {
			n := len(g.accumulated)
			probe := g.accumulated[n-textDegenerationProbeLen:]
			prior := g.accumulated[:n-textDegenerationProbeLen]
			if strings.Contains(prior, probe) {
				if g.substringHits == 0 {
					g.healthyLen = cumLen - len(probe)
				}
				g.substringHits++
				if g.substringHits >= g.cfg.MaxRepeatedTextSubstrings {
					g.tripped = true
				}
			} else {
				g.substringHits = 0
			}
		}
	}

	return g.tripped, g.healthyLen
}
