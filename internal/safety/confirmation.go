package safety

import "context"

type ConfirmationRequest struct {
	ToolName string
	Risk     string
	Summary  string
	Detail   string
	// Input is the raw tool-input JSON. Phase 5A: the TUI uses it to derive the
	// Allow-always pattern via PatternKey. It mirrors Detail when Detail is the
	// raw arguments, but stays a separate field so Detail can later evolve into
	// richer human-readable text without breaking pattern derivation.
	Input string
}

type Confirmer interface {
	Confirm(ctx context.Context, req ConfirmationRequest) (bool, error)
}

// Response carries a confirmation outcome plus the Phase 5A extras. It is the
// return type of DetailedConfirmer.ConfirmDetailed.
type Response struct {
	Approved bool
	// RejectReason, when non-empty on a rejected response, is fed back to the
	// model in the tool-result envelope so it can adjust. Empty rejection is
	// valid and reads to the model as a plain "user declined".
	RejectReason string
}

// DetailedConfirmer is the opt-in Phase 5A extension of Confirmer. Callers
// prefer ConfirmDetailed over Confirm when present, so a confirmer can capture
// reject reasons (and, on its own side, persist Allow-always grants). Legacy
// confirmers keep working via Confirm alone; the Loop falls back transparently.
type DetailedConfirmer interface {
	ConfirmDetailed(ctx context.Context, req ConfirmationRequest) (Response, error)
}

type StaticConfirmer bool

func (s StaticConfirmer) Confirm(ctx context.Context, req ConfirmationRequest) (bool, error) {
	return bool(s), nil
}

type AutoApproveConfirmer struct {
	MaxRisk  string
	Fallback Confirmer
}

func (c AutoApproveConfirmer) Confirm(ctx context.Context, req ConfirmationRequest) (bool, error) {
	if riskRank(req.Risk) <= riskRank(c.MaxRisk) {
		return true, nil
	}
	if c.Fallback == nil {
		return false, nil
	}
	return c.Fallback.Confirm(ctx, req)
}

func riskRank(risk string) int {
	switch risk {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	default:
		return 3
	}
}
