package builtin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

func TestModelCommandNoArgShowsCurrentAndSuggestions(t *testing.T) {
	var switched string
	cmd := ModelCommand()
	res, err := cmd.Run(context.Background(), command.Env{
		Model:           "glm-5.1",
		ModelSuggestions: []string{"glm-4.6", "glm-4.5"},
		SwitchModel: func(m string) error {
			switched = m
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if switched != "" {
		t.Fatalf("no-arg must not switch, got %q", switched)
	}
	if !strings.Contains(res.Output, "glm-5.1") || !strings.Contains(res.Output, "glm-4.6") {
		t.Fatalf("expected output to list current + suggestions, got %q", res.Output)
	}
	if res.ModelSwitched != nil {
		t.Fatal("expected no ModelSwitched signal on no-arg")
	}
}

func TestModelCommandWithArgSwitchesAndSignals(t *testing.T) {
	var switched string
	cmd := ModelCommand()
	res, err := cmd.Run(context.Background(), command.Env{
		Model: "glm-5.1",
		SwitchModel: func(m string) error {
			switched = m
			return nil
		},
	}, []string{"glm-4.6"})
	if err != nil {
		t.Fatal(err)
	}
	if switched != "glm-4.6" {
		t.Fatalf("expected SwitchModel called with glm-4.6, got %q", switched)
	}
	if res.ModelSwitched == nil || *res.ModelSwitched != "glm-4.6" {
		t.Fatalf("expected ModelSwitched=glm-4.6, got %#v", res.ModelSwitched)
	}
}

func TestModelCommandNilCallbackReportsUnavailable(t *testing.T) {
	cmd := ModelCommand()
	res, err := cmd.Run(context.Background(), command.Env{}, []string{"glm-4.6"})
	if err != nil {
		t.Fatalf("nil callback should be a soft message, not an error: %v", err)
	}
	if !strings.Contains(res.Output, "not available") {
		t.Fatalf("expected an unavailable message, got %q", res.Output)
	}
}

func TestModelCommandPropagatesSwitchError(t *testing.T) {
	cmd := ModelCommand()
	_, err := cmd.Run(context.Background(), command.Env{
		SwitchModel: func(string) error { return errors.New("boom") },
	}, []string{"glm-4.6"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the switch error to propagate, got %v", err)
	}
}
