package mcp

import "github.com/junnhwan/bond-code/internal/tool"

func riskFromAnnotations(info toolInfo) tool.RiskLevel {
	if boolAnnotation(info.Annotations, "destructiveHint") {
		return tool.RiskHigh
	}
	if boolAnnotation(info.Annotations, "readOnlyHint") {
		return tool.RiskLow
	}
	if boolAnnotation(info.Annotations, "idempotentHint") {
		return tool.RiskMedium
	}
	return tool.RiskMedium
}

func boolAnnotation(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	value, ok := values[key]
	if !ok {
		return false
	}
	b, ok := value.(bool)
	return ok && b
}
