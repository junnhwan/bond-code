package mcp

import (
	"encoding/json"
	"testing"

	"github.com/junnhwan/bond-code/internal/tool"
)

func TestRiskFromAnnotationsReadOnly(t *testing.T) {
	info := toolInfo{
		Name: "read_repo",
		Annotations: map[string]any{
			"readOnlyHint": true,
		},
	}
	if got := riskFromAnnotations(info); got != tool.RiskLow {
		t.Fatalf("expected low risk for readOnlyHint, got %s", got)
	}
}

func TestRiskFromAnnotationsDestructive(t *testing.T) {
	info := toolInfo{
		Name: "delete_file",
		Annotations: map[string]any{
			"destructiveHint": true,
		},
	}
	if got := riskFromAnnotations(info); got != tool.RiskHigh {
		t.Fatalf("expected high risk for destructiveHint, got %s", got)
	}
}

func TestAdapterToolUsesAnnotationRisk(t *testing.T) {
	raw := json.RawMessage(`{}`)
	tl := adapterTool{info: toolInfo{Name: "read", Annotations: map[string]any{"readOnlyHint": true}}}
	if got := tl.Risk(raw); got != tool.RiskLow {
		t.Fatalf("expected low risk, got %s", got)
	}
}

func TestNamespacedToolPreservesInnerRisk(t *testing.T) {
	inner := adapterTool{info: toolInfo{Name: "delete", Annotations: map[string]any{"destructiveHint": true}}}
	wrapped := NamespaceTool("server", inner)
	if got := wrapped.Risk(json.RawMessage(`{}`)); got != tool.RiskHigh {
		t.Fatalf("expected namespaced tool to preserve high risk, got %s", got)
	}
}
