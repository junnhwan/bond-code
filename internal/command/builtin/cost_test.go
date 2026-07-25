package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/command"
)

type fakeCostStatusProvider struct{}

func (fakeCostStatusProvider) StatusSnapshot() app.RuntimeStatus {
	return app.RuntimeStatus{
		Model: "fake-model",
		Usage: app.UsageStatus{
			ModelCalls:        2,
			LastInputTokens:   140,
			LastOutputTokens:  12,
			TotalInputTokens:  240,
			TotalOutputTokens: 20,
		},
	}
}

func TestCostCommandRendersMeasuredTokenUsage(t *testing.T) {
	result, err := CostCommand().Run(context.Background(), command.Env{
		StatusProvider: fakeCostStatusProvider{},
	}, nil)
	if err != nil {
		t.Fatalf("cost command: %v", err)
	}

	for _, want := range []string{"model calls: 2", "input: 240 tokens", "output: 20 tokens", "total: 260 tokens", "last call: 140 in / 12 out"} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("expected %q in cost output:\n%s", want, result.Output)
		}
	}
	if result.Panel == nil || result.Panel.Title != "cost" {
		t.Fatalf("expected structured cost panel, got %#v", result.Panel)
	}
}

func TestCostCommandReportsNoMeasuredUsage(t *testing.T) {
	result, err := CostCommand().Run(context.Background(), command.Env{}, nil)
	if err != nil {
		t.Fatalf("cost command: %v", err)
	}
	if !strings.Contains(result.Output, "not measured yet") {
		t.Fatalf("expected no-usage message, got:\n%s", result.Output)
	}
}

func TestRegisterAllIncludesCostCommand(t *testing.T) {
	registry := command.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("cost"); !ok {
		t.Fatal("expected cost command to be registered")
	}
}
