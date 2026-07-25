package tool

import (
	"context"
	"encoding/json"
)

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

type Tool interface {
	Name() string
	Description() string
	Schema() any
	Risk(input json.RawMessage) RiskLevel
	Execute(ctx context.Context, input json.RawMessage) (*Result, error)
}
