package llm

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countingClient struct {
	inflight atomic.Int32
	maxSeen  atomic.Int32
	delay    time.Duration
	err      error
}

func (c *countingClient) Stream(ctx context.Context, _ []Message, _ []ToolSpec) (<-chan Chunk, <-chan error) {
	out := make(chan Chunk)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		n := c.inflight.Add(1)
		defer c.inflight.Add(-1)
		for {
			cur := c.maxSeen.Load()
			if n <= cur || c.maxSeen.CompareAndSwap(cur, n) {
				break
			}
		}
		if c.delay > 0 {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case <-time.After(c.delay):
			}
		}
		if c.err != nil {
			errs <- c.err
			return
		}
		out <- Chunk{Content: "ok", Done: true, StopReason: "end_turn"}
		errs <- nil
	}()
	return out, errs
}

func drainStream(t *testing.T, client Client) error {
	t.Helper()
	chunks, errs := client.Stream(context.Background(), []Message{{Role: RoleUser, Content: "x"}}, nil)
	for range chunks {
	}
	return <-errs
}

func TestGateSerializesConcurrentStreams(t *testing.T) {
	inner := &countingClient{delay: 40 * time.Millisecond}
	gate := NewGate(RateLimitConfig{Enabled: true, MaxConcurrent: 1})
	client := NewRateLimitedClient(inner, gate)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := drainStream(t, client); err != nil {
				t.Errorf("stream: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := inner.maxSeen.Load(); got != 1 {
		t.Fatalf("max concurrent streams = %d, want 1", got)
	}
}

func TestGateCooldownAfterRateLimit(t *testing.T) {
	inner := &countingClient{err: &APIError{StatusCode: 429, Body: "rate limit", RetryAfter: 80 * time.Millisecond}}
	gate := NewGate(RateLimitConfig{
		Enabled:             true,
		MaxConcurrent:       1,
		CooldownOnRateLimit: 80 * time.Millisecond,
	})
	client := NewRateLimitedClient(inner, gate)

	if err := drainStream(t, client); err == nil {
		t.Fatal("expected 429")
	}
	// Next acquire should wait for cooldown.
	started := time.Now()
	// Swap to success for the second call by clearing error after a short delay
	// is hard with static err — instead measure that Acquire blocks.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := gate.Acquire(ctx)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("acquire after cooldown: %v", err)
	}
	gate.Release()
	if elapsed < 50*time.Millisecond {
		t.Fatalf("expected cooldown wait, elapsed=%s", elapsed)
	}
}

func TestGateRPMLimitsStarts(t *testing.T) {
	gate := NewGate(RateLimitConfig{Enabled: true, MaxConcurrent: 4, MaxRequestsPerMinute: 2})
	ctx := context.Background()
	if err := gate.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	gate.Release()
	if err := gate.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	gate.Release()

	// Third start should block until window moves; use short timeout.
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	err := gate.Acquire(ctx2)
	if err == nil {
		gate.Release()
		t.Fatal("expected RPM to block third acquire")
	}
}

func TestParseRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "42")
	if d := ParseRetryAfter(h); d != 42*time.Second {
		t.Fatalf("got %v", d)
	}
}

func TestRetryAfterOfBodyHint(t *testing.T) {
	err := &APIError{StatusCode: 429, Body: "1分钟内最多请求15次"}
	if d := RetryAfterOf(err); d != time.Minute {
		t.Fatalf("got %v want 1m", d)
	}
}

func TestNewGateDisabled(t *testing.T) {
	if g := NewGate(RateLimitConfig{}); g != nil {
		t.Fatal("disabled gate should be nil")
	}
	inner := &countingClient{}
	client := NewRateLimitedClient(inner, nil)
	if client != inner {
		t.Fatal("nil gate should return inner unchanged")
	}
}
