package app

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

func newSwitchModelApp(t *testing.T, model string) *App {
	t.Helper()
	fake := llm.NewFakeClient([]llm.Chunk{{Content: "ok", Done: true}})
	registry := tool.NewRegistry()
	loop := agent.NewLoop(agent.LoopConfig{}, fake, registry, safety.Policy{}, nil)
	return &App{
		Config: &config.Config{
			Model: config.ModelConfig{Model: model, APIKeyEnv: "BONDCODE_API_KEY"},
		},
		LLM:   fake,
		Agent: loop,
	}
}

func TestSwitchModelUpdatesConfigAndClient(t *testing.T) {
	app := newSwitchModelApp(t, "glm-5.1")
	orig := app.LLM

	if err := app.SwitchModel("glm-4.6"); err != nil {
		t.Fatalf("SwitchModel: %v", err)
	}
	if got := app.Config.Model.Model; got != "glm-4.6" {
		t.Fatalf("expected config model glm-4.6, got %q", got)
	}
	// buildModelClient rebuilds a fresh client, so the App's LLM reference must
	// change (the loop gets the same new client via SetClient).
	if app.LLM == orig {
		t.Fatal("expected app.LLM to be rebuilt as a new client after the switch")
	}
}

func TestSwitchModelBusyReturnsBusySentinel(t *testing.T) {
	app := newSwitchModelApp(t, "glm-5.1")
	// Simulate a mid-turn hold on the app mutex (RunWithEvents holds it for the
	// whole turn); SwitchModel must fail fast instead of racing the stream.
	app.mu.Lock()
	defer app.mu.Unlock()

	err := app.SwitchModel("glm-4.6")
	if !IsModelBusy(err) {
		t.Fatalf("expected errModelBusy, got %v", err)
	}
	if app.Config.Model.Model != "glm-5.1" {
		t.Fatalf("busy switch must not mutate config, got %q", app.Config.Model.Model)
	}
}

func TestSwitchModelEmptyNameErrors(t *testing.T) {
	app := newSwitchModelApp(t, "glm-5.1")
	if err := app.SwitchModel("   "); err == nil {
		t.Fatal("expected an error for an empty model name")
	}
}

func TestSwitchModelRetainsStreamIdleTimeout(t *testing.T) {
	t.Setenv("BONDCODE_TEST_API_KEY", "test")
	server := stallingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})

	app := newSwitchModelApp(t, "old-model")
	app.Config.Model.BaseURL = server.URL
	app.Config.Model.APIKeyEnv = "BONDCODE_TEST_API_KEY"
	app.Config.Model.StreamIdleTimeoutSeconds = 1
	if err := app.SwitchModel("new-model"); err != nil {
		t.Fatalf("SwitchModel: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, err := app.RunWithEvents(ctx, "hello", nil)
	if err == nil {
		t.Fatal("RunWithEvents unexpectedly succeeded")
	}
	var idleErr *llm.StreamIdleTimeoutError
	if !errors.As(err, &idleErr) {
		t.Fatalf("RunWithEvents error = %v, want StreamIdleTimeoutError", err)
	}
	if idleErr.Duration != time.Second {
		t.Fatalf("idle timeout duration = %s, want 1s", idleErr.Duration)
	}
}
