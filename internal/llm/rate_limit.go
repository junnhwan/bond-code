package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig shapes the shared request gate used by parent + all child
// agents on one API key. Zero Enabled keeps the old unbounded behavior.
type RateLimitConfig struct {
	Enabled bool
	// MaxConcurrent caps in-flight Stream calls (1 = fully serial).
	MaxConcurrent int
	// MaxRequestsPerMinute caps starts per rolling 60s window; 0 disables.
	MaxRequestsPerMinute int
	// CooldownOnRateLimit is applied after a 429 (or overridden by Retry-After).
	CooldownOnRateLimit time.Duration
}

// Gate serializes / paces LLM traffic across every client that shares it.
// Safe for concurrent use from parent loop + parallel subagents.
type Gate struct {
	maxConcurrent int
	maxRPM        int
	cooldownDef   time.Duration

	sem chan struct{}

	mu            sync.Mutex
	starts        []time.Time // rolling window for RPM
	cooldownUntil time.Time
}

// NewGate builds a gate from config. Returns nil when rate limiting is off so
// callers can skip wrapping.
func NewGate(cfg RateLimitConfig) *Gate {
	if !cfg.Enabled {
		return nil
	}
	maxC := cfg.MaxConcurrent
	if maxC <= 0 {
		maxC = 1
	}
	cooldown := cfg.CooldownOnRateLimit
	if cooldown <= 0 {
		cooldown = 60 * time.Second
	}
	return &Gate{
		maxConcurrent: maxC,
		maxRPM:        cfg.MaxRequestsPerMinute,
		cooldownDef:   cooldown,
		sem:           make(chan struct{}, maxC),
	}
}

// Acquire blocks until a concurrent slot (and optional RPM token) is available
// and any rate-limit cooldown has expired. ctx cancel aborts the wait.
func (g *Gate) Acquire(ctx context.Context) error {
	if g == nil {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// 1) Cooldown after a prior 429.
		if wait, ok := g.cooldownWait(); ok {
			if err := sleepCtx(ctx, wait); err != nil {
				return err
			}
			continue
		}
		// 2) Rolling RPM window.
		if wait, ok := g.rpmWait(); ok {
			if err := sleepCtx(ctx, wait); err != nil {
				return err
			}
			continue
		}
		// 3) Concurrent in-flight Stream slots.
		select {
		case g.sem <- struct{}{}:
			g.recordStart()
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Release frees one concurrent slot.
func (g *Gate) Release() {
	if g == nil {
		return
	}
	select {
	case <-g.sem:
	default:
		// Unbalanced release — ignore rather than block forever.
	}
}

// NoteRateLimited extends the global cooldown after a provider 429.
// retryAfter from Retry-After (or body heuristics) wins when longer than the
// configured default cooldown.
func (g *Gate) NoteRateLimited(retryAfter time.Duration) {
	if g == nil {
		return
	}
	d := g.cooldownDef
	if retryAfter > d {
		d = retryAfter
	}
	if d <= 0 {
		d = 60 * time.Second
	}
	until := time.Now().Add(d)
	g.mu.Lock()
	if until.After(g.cooldownUntil) {
		g.cooldownUntil = until
	}
	g.mu.Unlock()
}

func (g *Gate) cooldownWait() (time.Duration, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cooldownUntil.IsZero() {
		return 0, false
	}
	d := time.Until(g.cooldownUntil)
	if d <= 0 {
		g.cooldownUntil = time.Time{}
		return 0, false
	}
	return d, true
}

func (g *Gate) rpmWait() (time.Duration, bool) {
	if g.maxRPM <= 0 {
		return 0, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pruneStartsLocked(time.Now())
	if len(g.starts) < g.maxRPM {
		return 0, false
	}
	// Wait until the oldest start falls out of the 60s window.
	oldest := g.starts[0]
	wait := time.Until(oldest.Add(time.Minute))
	if wait <= 0 {
		return 0, false
	}
	return wait, true
}

func (g *Gate) recordStart() {
	if g.maxRPM <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	g.pruneStartsLocked(now)
	g.starts = append(g.starts, now)
}

func (g *Gate) pruneStartsLocked(now time.Time) {
	cut := now.Add(-time.Minute)
	i := 0
	for i < len(g.starts) && g.starts[i].Before(cut) {
		i++
	}
	if i > 0 {
		g.starts = append([]time.Time(nil), g.starts[i:]...)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	// Slice long waits so cancellation stays responsive.
	if d > 2*time.Second {
		d = 2 * time.Second
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// RateLimitedClient wraps a Client so every Stream pays the shared Gate.
type RateLimitedClient struct {
	inner Client
	gate  *Gate
}

// NewRateLimitedClient returns inner unchanged when gate is nil.
func NewRateLimitedClient(inner Client, gate *Gate) Client {
	if gate == nil || inner == nil {
		return inner
	}
	return &RateLimitedClient{inner: inner, gate: gate}
}

func (c *RateLimitedClient) Stream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan Chunk, <-chan error) {
	out := make(chan Chunk)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		if err := c.gate.Acquire(ctx); err != nil {
			errs <- fmt.Errorf("llm rate limit gate: %w", err)
			return
		}
		defer c.gate.Release()

		inChunks, inErrs := c.inner.Stream(ctx, messages, tools)
		for inChunks != nil || inErrs != nil {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case chunk, ok := <-inChunks:
				if !ok {
					inChunks = nil
					continue
				}
				select {
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				case out <- chunk:
				}
			case err, ok := <-inErrs:
				if !ok {
					inErrs = nil
					continue
				}
				if err != nil && IsRateLimited(err) {
					c.gate.NoteRateLimited(RetryAfterOf(err))
				}
				errs <- err
				return
			}
		}
		errs <- nil
	}()
	return out, errs
}

// RetryAfterOf extracts a suggested wait from APIError.RetryAfter or common
// Chinese/English rate-limit body wording.
func RetryAfterOf(err error) time.Duration {
	if err == nil {
		return 0
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		if apiErr.RetryAfter > 0 {
			return apiErr.RetryAfter
		}
		body := strings.ToLower(apiErr.Body + " " + apiErr.Error())
		if strings.Contains(apiErr.Body, "1分钟") || strings.Contains(body, "per minute") ||
			strings.Contains(body, "per 1 minute") || strings.Contains(body, "1 minute") {
			return time.Minute
		}
	}
	// Unwrapped string errors from retry wrappers.
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "1分钟") || strings.Contains(err.Error(), "1分钟内最多请求") {
		return time.Minute
	}
	return 0
}
