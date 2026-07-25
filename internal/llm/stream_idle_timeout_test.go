package llm

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewAnthropicCompatibleClientNormalizesStreamIdleTimeout(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{name: "zero uses default", seconds: 0, want: 120 * time.Second},
		{name: "negative uses default", seconds: -1, want: 120 * time.Second},
		{name: "positive is retained", seconds: 37, want: 37 * time.Second},
		{name: "maximum is retained", seconds: 86400, want: 24 * time.Hour},
		{name: "above maximum is clamped", seconds: 86401, want: 24 * time.Hour},
		{name: "largest int clamps before duration conversion", seconds: maxInt, want: 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewAnthropicCompatibleClient(AnthropicCompatibleConfig{
				StreamIdleTimeoutSeconds: tt.seconds,
			})

			if client.streamIdleTimeout != tt.want {
				t.Fatalf("stream idle timeout = %s, want %s", client.streamIdleTimeout, tt.want)
			}
		})
	}
}

func TestNewAnthropicCompatibleClientUsesStreamingHTTPTransport(t *testing.T) {
	defaults, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport type = %T, want *http.Transport", http.DefaultTransport)
	}

	client := NewAnthropicCompatibleClient(AnthropicCompatibleConfig{})
	if client.httpClient.Timeout != 0 {
		t.Fatalf("http client timeout = %s, want no total request timeout", client.httpClient.Timeout)
	}

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("http client transport type = %T, want *http.Transport", client.httpClient.Transport)
	}
	if transport == defaults {
		t.Fatal("http client transport aliases http.DefaultTransport; want an independent clone")
	}
	if transport.ResponseHeaderTimeout != 60*time.Second {
		t.Fatalf("response header timeout = %s, want %s", transport.ResponseHeaderTimeout, 60*time.Second)
	}

	assertSameFunction(t, "proxy", transport.Proxy, defaults.Proxy)
	assertSameFunction(t, "dial context", transport.DialContext, defaults.DialContext)
	preserved := []struct {
		name      string
		got, want any
	}{
		{name: "force HTTP/2", got: transport.ForceAttemptHTTP2, want: defaults.ForceAttemptHTTP2},
		{name: "maximum idle connections", got: transport.MaxIdleConns, want: defaults.MaxIdleConns},
		{name: "maximum idle connections per host", got: transport.MaxIdleConnsPerHost, want: defaults.MaxIdleConnsPerHost},
		{name: "maximum connections per host", got: transport.MaxConnsPerHost, want: defaults.MaxConnsPerHost},
		{name: "idle connection timeout", got: transport.IdleConnTimeout, want: defaults.IdleConnTimeout},
		{name: "TLS handshake timeout", got: transport.TLSHandshakeTimeout, want: defaults.TLSHandshakeTimeout},
		{name: "expect continue timeout", got: transport.ExpectContinueTimeout, want: defaults.ExpectContinueTimeout},
		{name: "disable keep alives", got: transport.DisableKeepAlives, want: defaults.DisableKeepAlives},
		{name: "disable compression", got: transport.DisableCompression, want: defaults.DisableCompression},
		{name: "maximum response header bytes", got: transport.MaxResponseHeaderBytes, want: defaults.MaxResponseHeaderBytes},
	}
	for _, field := range preserved {
		if !reflect.DeepEqual(field.got, field.want) {
			t.Errorf("%s = %v, want default %v", field.name, field.got, field.want)
		}
	}
}

func TestStreamIdleTimeoutErrorReportsTimeoutDetails(t *testing.T) {
	const idleFor = 37 * time.Second
	var err error = &StreamIdleTimeoutError{Duration: idleFor}

	var idleErr *StreamIdleTimeoutError
	if !errors.As(err, &idleErr) {
		t.Fatalf("errors.As(%T) = false, want true", err)
	}
	if idleErr.Duration != idleFor {
		t.Fatalf("idle timeout duration = %s, want %s", idleErr.Duration, idleFor)
	}
	if !strings.Contains(err.Error(), idleFor.String()) {
		t.Fatalf("error text %q does not include duration %q", err, idleFor)
	}

	var netErr net.Error
	if !errors.As(err, &netErr) {
		t.Fatalf("errors.As(net.Error) = false for %T", err)
	}
	if !netErr.Timeout() {
		t.Error("Timeout() = false, want true")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Error("stream idle timeout matches context.DeadlineExceeded; want distinct error identity")
	}
}

func assertSameFunction(t *testing.T, name string, got, want any) {
	t.Helper()
	gotValue := reflect.ValueOf(got)
	wantValue := reflect.ValueOf(want)
	if gotValue.IsValid() != wantValue.IsValid() {
		t.Fatalf("%s presence differs from http.DefaultTransport", name)
	}
	if gotValue.IsValid() && gotValue.Pointer() != wantValue.Pointer() {
		t.Fatalf("%s does not preserve http.DefaultTransport default", name)
	}
}

type idleTestReader struct {
	ctx          context.Context
	mu           sync.Mutex
	calls        int
	firstDelay   time.Duration
	zeroReads    bool
	firstStarted chan struct{}
	firstRelease chan struct{}
	secondStart  chan struct{}
}

func (r *idleTestReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()

	if call == 1 && r.firstStarted != nil {
		close(r.firstStarted)
		<-r.firstRelease
		p[0] = 'x'
		return 1, nil
	}
	if call == 1 && r.firstDelay > 0 {
		time.Sleep(r.firstDelay)
		p[0] = 'x'
		return 1, nil
	}
	if r.zeroReads {
		time.Sleep(time.Millisecond)
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		default:
			return 0, nil
		}
	}
	if call == 2 && r.secondStart != nil {
		close(r.secondStart)
	}
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func TestStreamIdleControllerTimesOutSetupAndTranslatesCause(t *testing.T) {
	controller := newStreamIdleController(context.Background(), 20*time.Millisecond)
	generation := controller.arm()
	defer controller.finish()

	select {
	case <-controller.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("setup watchdog did not cancel request context")
	}

	err := controller.translate(controller.Context().Err())
	var idleErr *StreamIdleTimeoutError
	if !errors.As(err, &idleErr) {
		t.Fatalf("setup error = %T %v, want StreamIdleTimeoutError", err, err)
	}
	if idleErr.Duration != 20*time.Millisecond {
		t.Fatalf("idle duration = %s, want 20ms", idleErr.Duration)
	}
	controller.disarm(generation)
}

func TestStreamIdleReaderTimesOutFirstAndInterByteSilence(t *testing.T) {
	const idle = 25 * time.Millisecond
	controller := newStreamIdleController(context.Background(), idle)
	reader := &idleTestReader{ctx: controller.Context()}
	guarded := newStreamIdleReader(reader, controller)
	defer controller.finish()

	started := time.Now()
	_, err := guarded.Read(make([]byte, 1))
	elapsed := time.Since(started)
	var idleErr *StreamIdleTimeoutError
	if !errors.As(controller.translate(err), &idleErr) {
		t.Fatalf("first-byte error = %T %v, want StreamIdleTimeoutError", err, err)
	}
	if elapsed < idle || elapsed > 500*time.Millisecond {
		t.Fatalf("first-byte timeout took %s, want between %s and 500ms", elapsed, idle)
	}
}

func TestStreamIdleReaderGivesNextReadFreshIntervalAfterProgress(t *testing.T) {
	const idle = 40 * time.Millisecond
	controller := newStreamIdleController(context.Background(), idle)
	reader := &idleTestReader{ctx: controller.Context(), firstDelay: 30 * time.Millisecond}
	guarded := newStreamIdleReader(reader, controller)
	defer controller.finish()

	started := time.Now()
	if n, err := guarded.Read(make([]byte, 1)); n != 1 || err != nil {
		t.Fatalf("first read = (%d, %v), want (1, nil)", n, err)
	}
	_, err := guarded.Read(make([]byte, 1))
	var idleErr *StreamIdleTimeoutError
	if !errors.As(controller.translate(err), &idleErr) {
		t.Fatalf("second read error = %T %v, want StreamIdleTimeoutError", err, err)
	}
	if elapsed := time.Since(started); elapsed < 60*time.Millisecond {
		t.Fatalf("progress did not grant the next read a fresh interval; total = %s", elapsed)
	}
}

func TestStreamIdleReaderZeroNilReadsKeepAbsoluteDeadline(t *testing.T) {
	const idle = 30 * time.Millisecond
	controller := newStreamIdleController(context.Background(), idle)
	reader := &idleTestReader{ctx: controller.Context(), zeroReads: true}
	guarded := newStreamIdleReader(reader, controller)
	defer controller.finish()

	started := time.Now()
	var err error
	for err == nil && time.Since(started) < time.Second {
		_, err = guarded.Read(make([]byte, 1))
	}
	var idleErr *StreamIdleTimeoutError
	if !errors.As(controller.translate(err), &idleErr) {
		t.Fatalf("zero-read error = %T %v, want StreamIdleTimeoutError", err, err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("zero-byte reads extended the deadline to %s", elapsed)
	}
}

func TestStreamIdleControllerDoesNotRunBetweenReads(t *testing.T) {
	controller := newStreamIdleController(context.Background(), 20*time.Millisecond)
	defer controller.finish()

	time.Sleep(60 * time.Millisecond)
	select {
	case <-controller.Context().Done():
		t.Fatalf("idle controller canceled while no setup/read was armed: %v", context.Cause(controller.Context()))
	default:
	}
}

func TestStreamIdleControllerStaleCallbackCannotCancelLaterRead(t *testing.T) {
	const idle = 100 * time.Millisecond
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	controller := newStreamIdleController(parent, idle)
	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	controller.beforeExpire = func(generation uint64) {
		close(callbackStarted)
		<-releaseCallback
	}
	reader := &idleTestReader{
		ctx:          controller.Context(),
		firstStarted: make(chan struct{}),
		firstRelease: make(chan struct{}),
		secondStart:  make(chan struct{}),
	}
	guarded := newStreamIdleReader(reader, controller)

	firstDone := make(chan error, 1)
	go func() {
		_, err := guarded.Read(make([]byte, 1))
		firstDone <- err
	}()
	<-reader.firstStarted
	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("first watchdog callback did not start")
	}
	controller.mu.Lock()
	staleWatch := controller.current
	controller.mu.Unlock()
	if staleWatch == nil {
		t.Fatal("stale watchdog missing after callback started")
	}
	callbackCompleted := staleWatch.done

	close(reader.firstRelease)
	if err := <-firstDone; err != nil {
		t.Fatalf("successful first read returned %v", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := guarded.Read(make([]byte, 1))
		secondDone <- err
	}()
	<-reader.secondStart
	close(releaseCallback)
	select {
	case <-callbackCompleted:
	case <-time.After(time.Second):
		t.Fatal("stale watchdog callback did not complete")
	}
	select {
	case <-controller.Context().Done():
		t.Fatalf("stale callback canceled later generation: %v", context.Cause(controller.Context()))
	default:
	}

	cancelParent()
	if err := <-secondDone; !errors.Is(controller.translate(err), context.Canceled) {
		t.Fatalf("second read error = %v, want caller context.Canceled", err)
	}
	finished := make(chan struct{})
	go func() {
		controller.finish()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("finish did not wait for watchdog callback cleanup")
	}
}

func TestStreamIdleReaderPreservesCallerDeadlineError(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	controller := newStreamIdleController(parent, time.Second)
	reader := &idleTestReader{ctx: controller.Context()}
	guarded := newStreamIdleReader(reader, controller)
	defer controller.finish()

	_, err := guarded.Read(make([]byte, 1))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline read error = %T %v, want context.DeadlineExceeded", err, err)
	}
	var idleErr *StreamIdleTimeoutError
	if errors.As(err, &idleErr) {
		t.Fatalf("caller deadline translated to idle timeout: %v", err)
	}
}

func TestStreamIdleControllerConcurrentFinishWaitsForCallbackCleanup(t *testing.T) {
	controller := newStreamIdleController(context.Background(), 10*time.Millisecond)
	callbackReachedCleanup := make(chan struct{})
	releaseCallbackCleanup := make(chan struct{})
	controller.beforeCleanup = func(uint64) {
		close(callbackReachedCleanup)
		<-releaseCallbackCleanup
	}
	controller.arm()
	controller.mu.Lock()
	watch := controller.current
	controller.mu.Unlock()
	if watch == nil {
		t.Fatal("watchdog missing after arm")
	}
	callbackCompleted := watch.done
	select {
	case <-callbackReachedCleanup:
	case <-time.After(time.Second):
		t.Fatal("watchdog callback did not reach pending cleanup")
	}

	firstFinished := make(chan struct{})
	secondFinished := make(chan struct{})
	go func() {
		controller.finish()
		close(firstFinished)
	}()
	go func() {
		controller.finish()
		close(secondFinished)
	}()
	select {
	case <-firstFinished:
		t.Fatal("first finish returned before callback cleanup")
	case <-secondFinished:
		t.Fatal("concurrent finish returned before callback cleanup")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseCallbackCleanup)
	select {
	case <-callbackCompleted:
	case <-time.After(time.Second):
		t.Fatal("watchdog callback did not signal completed cleanup")
	}
	for name, done := range map[string]<-chan struct{}{
		"first":  firstFinished,
		"second": secondFinished,
	} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("%s finish did not complete after callback cleanup", name)
		}
	}
}

func TestStreamIdleControllerFirstCancellationCauseWins(t *testing.T) {
	t.Run("caller first", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		controller := newStreamIdleController(parent, time.Hour)
		cancel()
		<-controller.Context().Done()
		controller.cancel(&StreamIdleTimeoutError{Duration: time.Hour})
		if err := controller.translate(controller.Context().Err()); !errors.Is(err, context.Canceled) {
			t.Fatalf("translated error = %v, want context.Canceled", err)
		}
		controller.finish()
	})

	t.Run("watchdog first", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		controller := newStreamIdleController(parent, time.Hour)
		controller.cancel(&StreamIdleTimeoutError{Duration: time.Hour})
		cancel()
		var idleErr *StreamIdleTimeoutError
		if err := controller.translate(controller.Context().Err()); !errors.As(err, &idleErr) {
			t.Fatalf("translated error = %T %v, want StreamIdleTimeoutError", err, err)
		}
		controller.finish()
	})
}

func newIdleTestClient(t *testing.T, serverURL string, idle time.Duration) *AnthropicCompatibleClient {
	t.Helper()
	t.Setenv("BONDCODE_IDLE_TEST_KEY", "test-key")
	client := NewAnthropicCompatibleClient(AnthropicCompatibleConfig{
		BaseURL:   serverURL,
		APIKeyEnv: "BONDCODE_IDLE_TEST_KEY",
		Model:     "test-model",
		MaxTokens: 128,
	})
	client.streamIdleTimeout = idle
	return client
}

func runIdleWire(client *AnthropicCompatibleClient, chunks chan<- Chunk) error {
	return client.streamAnthropicWire(context.Background(), nil, nil, chunks)
}

func closeIdleTestServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		server.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("httptest.Server.Close did not return; handler/request leaked")
	}
}

func TestAnthropicCompatibleStreamStaysAliveWithNetworkProgress(t *testing.T) {
	const idle = 45 * time.Millisecond
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer close(handlerDone)
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for range 10 {
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
			time.Sleep(15 * time.Millisecond)
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
		flusher.Flush()
	}))
	client := newIdleTestClient(t, server.URL, idle)
	chunks := make(chan Chunk, 8)
	started := time.Now()
	err := runIdleWire(client, chunks)
	if err != nil {
		t.Fatalf("active stream failed: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 3*idle {
		t.Fatalf("test stream lasted %s, want at least %s", elapsed, 3*idle)
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("active stream handler did not exit")
	}
	closeIdleTestServer(t, server)
}

func TestAnthropicCompatibleStreamTimesOutSilentSetupFirstByteAndInterByte(t *testing.T) {
	const idle = 35 * time.Millisecond
	tests := []struct {
		name  string
		serve func(http.ResponseWriter, *http.Request)
	}{
		{
			name: "setup before headers",
			serve: func(_ http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
			},
		},
		{
			name: "first body byte",
			serve: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			},
		},
		{
			name: "between body bytes",
			serve: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "data: {\"type\":")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerDone := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(handlerDone)
				tt.serve(w, r)
			}))
			client := newIdleTestClient(t, server.URL, idle)
			started := time.Now()
			err := runIdleWire(client, make(chan Chunk, 8))
			var idleErr *StreamIdleTimeoutError
			if !errors.As(err, &idleErr) {
				t.Fatalf("stream error = %T %v, want StreamIdleTimeoutError", err, err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("idle timeout took too long: %s", elapsed)
			}
			select {
			case <-handlerDone:
			case <-time.After(time.Second):
				t.Fatal("server handler did not observe request cancellation")
			}
			closeIdleTestServer(t, server)
		})
	}
}

func TestAnthropicCompatibleStreamPropagatesIdleTimeoutReadingNon2xxBody(t *testing.T) {
	const idle = 35 * time.Millisecond
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "partial error body")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	client := newIdleTestClient(t, server.URL, idle)
	err := runIdleWire(client, make(chan Chunk, 1))
	var idleErr *StreamIdleTimeoutError
	if !errors.As(err, &idleErr) {
		t.Fatalf("non-2xx body error = %T %v, want StreamIdleTimeoutError", err, err)
	}
	if !IsRetryable(context.Background(), err) {
		t.Fatalf("non-2xx idle error is not retryable: %v", err)
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("non-2xx handler did not exit")
	}
	closeIdleTestServer(t, server)
}

func TestAnthropicCompatibleStreamDoesNotTimeoutDuringChunkBackpressure(t *testing.T) {
	const idle = 30 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"blocked\"}}\n\n")
		w.(http.Flusher).Flush()
	}))
	client := newIdleTestClient(t, server.URL, idle)
	parserBlocked := make(chan struct{})
	parser := newAnthropicSSEParser()
	parser.beforeChunkSend = func(chunk Chunk) {
		if chunk.Content == "blocked" {
			close(parserBlocked)
		}
	}
	chunks := make(chan Chunk)
	errDone := make(chan error, 1)
	go func() {
		errDone <- client.streamAnthropicWireWithParser(context.Background(), nil, nil, chunks, parser)
	}()

	select {
	case <-parserBlocked:
	case <-time.After(time.Second):
		t.Fatal("parser did not reach blocked chunk delivery")
	}
	waited := time.NewTimer(4 * idle)
	defer waited.Stop()
	select {
	case err := <-errDone:
		t.Fatalf("backpressured stream returned before consumer resumed: %v", err)
	case <-waited.C:
	}
	select {
	case chunk := <-chunks:
		if chunk.Content != "blocked" {
			t.Fatalf("chunk content = %q, want blocked", chunk.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("parser did not remain blocked on chunk delivery")
	}
	select {
	case chunk := <-chunks:
		if !chunk.Done {
			t.Fatalf("final chunk = %+v, want Done", chunk)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not emit final Done chunk after consumer resumed")
	}
	select {
	case err := <-errDone:
		if err != nil {
			t.Fatalf("backpressured stream failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("backpressured stream did not finish after consumer resumed")
	}
	closeIdleTestServer(t, server)
}

func TestAnthropicCompatibleStreamCallerCancellationIsExact(t *testing.T) {
	const idle = time.Second
	tests := []struct {
		name        string
		flushHeader bool
	}{
		{name: "before headers"},
		{name: "during body read", flushHeader: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerStarted := make(chan struct{})
			handlerDone := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(handlerDone)
				_, _ = io.Copy(io.Discard, r.Body)
				if tt.flushHeader {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				close(handlerStarted)
				<-r.Context().Done()
			}))
			client := newIdleTestClient(t, server.URL, idle)
			ctx, cancel := context.WithCancel(context.Background())
			errDone := make(chan error, 1)
			go func() { errDone <- client.streamAnthropicWire(ctx, nil, nil, make(chan Chunk, 1)) }()
			<-handlerStarted
			cancel()
			select {
			case err := <-errDone:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("stream error = %T %v, want context.Canceled", err, err)
				}
				var idleErr *StreamIdleTimeoutError
				if errors.As(err, &idleErr) {
					t.Fatalf("caller cancellation translated to idle error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("caller cancellation did not terminate stream promptly")
			}
			select {
			case <-handlerDone:
			case <-time.After(time.Second):
				t.Fatal("canceled handler did not exit")
			}
			closeIdleTestServer(t, server)
		})
	}
}

func TestAnthropicCompatibleStreamConcurrentCallerCancellationCleanup(t *testing.T) {
	const requests = 12
	handlerStarted := make(chan struct{}, requests)
	handlerDone := make(chan struct{}, requests)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { handlerDone <- struct{}{} }()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		handlerStarted <- struct{}{}
		<-r.Context().Done()
	}))
	client := newIdleTestClient(t, server.URL, time.Second)

	cancels := make([]context.CancelFunc, 0, requests)
	errs := make(chan error, requests)
	for range requests {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		go func() {
			errs <- client.streamAnthropicWire(ctx, nil, nil, make(chan Chunk, 1))
		}()
	}
	for range requests {
		select {
		case <-handlerStarted:
		case <-time.After(time.Second):
			t.Fatal("concurrent cancellation handler did not start")
		}
	}
	for _, cancel := range cancels {
		cancel()
	}
	for range requests {
		select {
		case err := <-errs:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("concurrent cancellation error = %T %v, want context.Canceled", err, err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrently canceled request did not exit")
		}
	}
	for range requests {
		select {
		case <-handlerDone:
		case <-time.After(time.Second):
			t.Fatal("concurrently canceled server handler did not exit")
		}
	}
	closeIdleTestServer(t, server)
}

func TestAnthropicCompatibleStreamConcurrentTimeoutCleanup(t *testing.T) {
	const (
		idle     = 30 * time.Millisecond
		requests = 12
	)
	handlerDone := make(chan struct{}, requests)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { handlerDone <- struct{}{} }()
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	client := newIdleTestClient(t, server.URL, idle)
	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- runIdleWire(client, make(chan Chunk, 1))
		}()
	}
	requestDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(requestDone)
	}()
	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent timed-out request goroutines did not exit")
	}
	close(errs)
	for err := range errs {
		var idleErr *StreamIdleTimeoutError
		if !errors.As(err, &idleErr) {
			t.Errorf("concurrent stream error = %T %v, want StreamIdleTimeoutError", err, err)
		}
	}
	for range requests {
		select {
		case <-handlerDone:
		case <-time.After(time.Second):
			t.Fatal("concurrent server handler did not exit")
		}
	}
	closeIdleTestServer(t, server)
}
