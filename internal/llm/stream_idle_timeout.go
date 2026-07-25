package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	defaultStreamIdleTimeoutSeconds = 120
	maxStreamIdleTimeoutSeconds     = 24 * 60 * 60
)

type StreamIdleTimeoutError struct {
	Duration time.Duration
}

func (e *StreamIdleTimeoutError) Error() string {
	return fmt.Sprintf("stream idle timeout after %s", e.Duration)
}

func (e *StreamIdleTimeoutError) Timeout() bool {
	return true
}

// Temporary keeps StreamIdleTimeoutError compatible with net.Error. Callers
// should use Timeout; net.Error.Temporary itself is deprecated.
func (e *StreamIdleTimeoutError) Temporary() bool {
	return true
}

func normalizeStreamIdleTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = defaultStreamIdleTimeoutSeconds
	}
	if seconds > maxStreamIdleTimeoutSeconds {
		seconds = maxStreamIdleTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

type streamIdleTimer struct {
	timer *time.Timer
	done  chan struct{}
	once  sync.Once
}

func (t *streamIdleTimer) complete() {
	t.once.Do(func() { close(t.done) })
}

type streamIdleController struct {
	parent  context.Context
	ctx     context.Context
	cancel  context.CancelCauseFunc
	timeout time.Duration

	mu         sync.Mutex
	generation uint64
	armed      bool
	completed  bool
	current    *streamIdleTimer
	pending    map[*streamIdleTimer]struct{}
	finished   chan struct{}

	// beforeExpire and beforeCleanup are package-private deterministic test
	// seams. Production controllers leave them nil; race tests can pause a
	// callback before it claims cancellation or before it cleans up its timer.
	beforeExpire  func(uint64)
	beforeCleanup func(uint64)
}

func newStreamIdleController(parent context.Context, timeout time.Duration) *streamIdleController {
	if timeout <= 0 {
		timeout = time.Duration(defaultStreamIdleTimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &streamIdleController{
		parent:   parent,
		ctx:      ctx,
		cancel:   cancel,
		timeout:  timeout,
		pending:  make(map[*streamIdleTimer]struct{}),
		finished: make(chan struct{}),
	}
}

func (c *streamIdleController) Context() context.Context {
	return c.ctx
}

func (c *streamIdleController) arm() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.completed || c.ctx.Err() != nil {
		return 0
	}
	if c.armed {
		return c.generation
	}

	c.generation++
	generation := c.generation
	watch := &streamIdleTimer{done: make(chan struct{})}
	c.armed = true
	c.current = watch
	c.pending[watch] = struct{}{}
	watch.timer = time.AfterFunc(c.timeout, func() {
		if c.beforeExpire != nil {
			c.beforeExpire(generation)
		}
		c.expire(generation)
		if c.beforeCleanup != nil {
			c.beforeCleanup(generation)
		}
		c.mu.Lock()
		delete(c.pending, watch)
		watch.complete()
		c.mu.Unlock()
	})
	return generation
}

func (c *streamIdleController) disarm(generation uint64) {
	if generation == 0 {
		return
	}
	c.mu.Lock()
	if c.completed || !c.armed || c.generation != generation {
		c.mu.Unlock()
		return
	}
	c.armed = false
	watch := c.current
	c.current = nil
	if watch != nil && watch.timer.Stop() {
		delete(c.pending, watch)
		watch.complete()
	}
	c.mu.Unlock()
}

func (c *streamIdleController) expire(generation uint64) {
	c.mu.Lock()
	if c.completed || !c.armed || c.generation != generation {
		c.mu.Unlock()
		return
	}
	c.armed = false
	c.current = nil
	// Claim the request cause before releasing the state lock. This keeps a
	// winning watchdog callback atomic with respect to finish(); parent
	// cancellation may still race and context.WithCancelCause preserves the
	// true first cause between those two independent owners.
	c.cancel(&StreamIdleTimeoutError{Duration: c.timeout})
	c.mu.Unlock()
}

func (c *streamIdleController) translate(err error) error {
	if err == nil {
		return nil
	}
	cause := context.Cause(c.ctx)
	var idleErr *StreamIdleTimeoutError
	if errors.As(cause, &idleErr) {
		return idleErr
	}
	if callerErr := c.parent.Err(); callerErr != nil {
		return callerErr
	}
	return err
}

func (c *streamIdleController) finish() {
	c.mu.Lock()
	if c.completed {
		finished := c.finished
		c.mu.Unlock()
		<-finished
		return
	}
	c.completed = true
	c.armed = false
	if watch := c.current; watch != nil {
		c.current = nil
		if watch.timer.Stop() {
			delete(c.pending, watch)
			watch.complete()
		}
	}
	pending := make([]<-chan struct{}, 0, len(c.pending))
	for watch := range c.pending {
		pending = append(pending, watch.done)
	}
	c.mu.Unlock()

	// Release the parent link and any net/http operation still retaining the
	// derived context. A prior caller or watchdog cause remains the first cause.
	c.cancel(nil)
	for _, done := range pending {
		<-done
	}
	close(c.finished)
}

type streamIdleReader struct {
	reader     io.Reader
	controller *streamIdleController
}

func newStreamIdleReader(reader io.Reader, controller *streamIdleController) io.Reader {
	return &streamIdleReader{reader: reader, controller: controller}
}

func (r *streamIdleReader) Read(p []byte) (int, error) {
	generation := r.controller.arm()
	n, err := r.reader.Read(p)
	// A positive byte count is network progress, so the following read gets a
	// fresh interval. A terminal error also ends this read. Repeated (0, nil)
	// reads deliberately retain the original timer and absolute deadline.
	if n > 0 || err != nil {
		r.controller.disarm(generation)
	}
	return n, r.controller.translate(err)
}

type streamIdleBody struct {
	body       io.ReadCloser
	reader     io.Reader
	controller *streamIdleController
}

func newStreamIdleBody(body io.ReadCloser, controller *streamIdleController) io.ReadCloser {
	return &streamIdleBody{
		body:       body,
		reader:     newStreamIdleReader(body, controller),
		controller: controller,
	}
}

func (b *streamIdleBody) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *streamIdleBody) Close() error {
	err := b.body.Close()
	b.controller.finish()
	return b.controller.translate(err)
}
