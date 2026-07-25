package tool

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeTool struct{}

func (fakeTool) Name() string                   { return "fake" }
func (fakeTool) Description() string            { return "fake tool" }
func (fakeTool) Schema() any                    { return map[string]any{"type": "object"} }
func (fakeTool) Risk(json.RawMessage) RiskLevel { return RiskLow }
func (fakeTool) Execute(context.Context, json.RawMessage) (*Result, error) {
	return &Result{ToolName: "fake", Output: "ok", OK: true}, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(fakeTool{}); err != nil {
		t.Fatal(err)
	}

	got, ok := reg.Get("fake")
	if !ok {
		t.Fatal("expected fake tool")
	}
	if got.Name() != "fake" {
		t.Fatalf("unexpected tool %s", got.Name())
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(fakeTool{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeTool{}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestResultConstructorsBuildStandardEnvelope(t *testing.T) {
	success := Success("read_file", "read ok", "content")
	if success.Status != "success" || !success.OK || success.Summary != "read ok" || success.Output != "content" {
		t.Fatalf("unexpected success result: %#v", success)
	}

	failure := ErrorResult("read_file", "read failed", "missing path")
	if failure.Status != "error" || failure.OK || failure.Summary != "read failed" || failure.Error != "missing path" {
		t.Fatalf("unexpected error result: %#v", failure)
	}

	guarded := Guarded("read_file", "loop guard", "blocked repeated call")
	if guarded.Status != "guarded" || guarded.OK || guarded.Summary != "loop guard" || guarded.Output != "blocked repeated call" {
		t.Fatalf("unexpected guarded result: %#v", guarded)
	}
}
