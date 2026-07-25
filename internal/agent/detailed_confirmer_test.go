package agent

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

// detailedRejectConfirmer is a Phase 5A DetailedConfirmer that always rejects
// with a fixed reason, so the loop test can assert the reason flows into the
// tool-result envelope. Its base Confirm is never reached because the loop
// prefers ConfirmDetailed.
type detailedRejectConfirmer struct {
	reason string
}

func (d *detailedRejectConfirmer) Confirm(ctx context.Context, req safety.ConfirmationRequest) (bool, error) {
	return false, nil
}

func (d *detailedRejectConfirmer) ConfirmDetailed(ctx context.Context, req safety.ConfirmationRequest) (safety.Response, error) {
	return safety.Response{Approved: false, RejectReason: d.reason}, nil
}

// detailedApproveConfirmer is a Phase 5A DetailedConfirmer that always approves,
// to exercise the type-assertion approval path end to end.
type detailedApproveConfirmer struct{}

func (d *detailedApproveConfirmer) Confirm(ctx context.Context, req safety.ConfirmationRequest) (bool, error) {
	return false, nil
}

func (d *detailedApproveConfirmer) ConfirmDetailed(ctx context.Context, req safety.ConfirmationRequest) (safety.Response, error) {
	return safety.Response{Approved: true}, nil
}

// TestDetailedConfirmerRejectReasonFlowsToEnvelope confirms Phase 5A: when a
// DetailedConfirmer rejects with a reason, the reason reaches the tool-result
// envelope so the model can adjust on its next turn.
func TestDetailedConfirmerRejectReasonFlowsToEnvelope(t *testing.T) {
	toolUnderTest := &riskTool{risk: tool.RiskHigh}
	confirmer := &detailedRejectConfirmer{reason: "use a different approach"}
	result, err := runRiskToolLoop(toolUnderTest, confirmer)
	if err != nil {
		t.Fatal(err)
	}
	if toolUnderTest.executed {
		t.Fatal("expected rejected tool not to execute")
	}
	if !traceHasToolError(result.Trace.Events, "risk_tool", "use a different approach") {
		t.Fatalf("expected reject reason in tool envelope, got %#v", result.Trace.Events)
	}
}

// TestDetailedConfirmerApprovedRunsTool confirms a DetailedConfirmer that
// approves lets the tool execute — the type-assertion path works end to end and
// the legacy Confirm fallback is not consulted.
func TestDetailedConfirmerApprovedRunsTool(t *testing.T) {
	toolUnderTest := &riskTool{risk: tool.RiskHigh}
	confirmer := &detailedApproveConfirmer{}
	result, err := runRiskToolLoop(toolUnderTest, confirmer)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "done" {
		t.Fatalf("expected loop to continue after approval, got %q", result.FinalAnswer)
	}
	if !toolUnderTest.executed {
		t.Fatal("expected approved high risk tool to execute")
	}
}
