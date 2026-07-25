package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/llm"
)

func streamError(t *testing.T, client llm.Client) (time.Duration, error) {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	chunks, errs := client.Stream(ctx, []llm.Message{{Role: llm.RoleUser, Content: "hello"}}, nil)
	for range chunks {
	}
	return time.Since(started), <-errs
}

func assertOneSecondIdleTimeout(t *testing.T, err error, elapsed time.Duration) {
	t.Helper()
	var idleErr *llm.StreamIdleTimeoutError
	if !errors.As(err, &idleErr) {
		t.Fatalf("error = %v, want StreamIdleTimeoutError", err)
	}
	if idleErr.Duration != time.Second {
		t.Fatalf("idle timeout duration = %s, want 1s", idleErr.Duration)
	}
	if elapsed < 700*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("elapsed = %s, want configured one-second bound", elapsed)
	}
}

func stallingServer(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(func() {
		server.CloseClientConnections()
		done := make(chan struct{})
		go func() { server.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("httptest server did not close within deadline")
		}
	})
	return server
}

func TestBuildModelClientPropagatesStreamIdleTimeoutToPrimary(t *testing.T) {
	t.Setenv("BONDCODE_TEST_API_KEY", "test")
	server := stallingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})

	client := buildModelClient(config.ModelConfig{
		BaseURL: server.URL, APIKeyEnv: "BONDCODE_TEST_API_KEY", Model: "primary",
		StreamIdleTimeoutSeconds: 1,
	})
	elapsed, err := streamError(t, client)
	assertOneSecondIdleTimeout(t, err, elapsed)
}

func TestBuildModelClientPropagatesStreamIdleTimeoutToFallback(t *testing.T) {
	t.Setenv("BONDCODE_TEST_API_KEY", "test")
	server := stallingServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if body.Model == "primary" {
			http.Error(w, "overloaded", 529)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})

	client := buildModelClient(config.ModelConfig{
		BaseURL: server.URL, APIKeyEnv: "BONDCODE_TEST_API_KEY", Model: "primary",
		StreamIdleTimeoutSeconds: 1,
		Retry:                    config.RetryConfig{Enabled: true, MaxAttempts: 2, BaseBackoffMs: 1, MaxBackoffMs: 1, OverloadFallbackThreshold: 1, FallbackModels: []string{"fallback"}},
	})
	elapsed, err := streamError(t, client)
	assertOneSecondIdleTimeout(t, err, elapsed)
}
