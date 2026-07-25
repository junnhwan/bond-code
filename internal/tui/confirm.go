package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/safety"
)

// confirmChoice is the user's selection at a permission prompt (Phase 5A).
// choiceOnce/choiceReject apply at every risk level; choiceAlways (persist a
// session allow rule via Config.RuleSource) is only offered at non-high risk.
type confirmChoice int

const (
	choiceOnce confirmChoice = iota
	choiceAlways
	choiceReject
)

// approved reports whether this choice lets the tool run.
func (c confirmChoice) approved() bool { return c == choiceOnce || c == choiceAlways }

type Confirmer struct {
	mu             sync.Mutex
	pending        *confirmationRequest
	queuedResponse *safety.Response
}

type confirmationRequest struct {
	req      safety.ConfirmationRequest
	response chan safety.Response
}

func NewConfirmer() *Confirmer {
	return &Confirmer{}
}

// Confirm satisfies the base safety.Confirmer interface (bool return) for
// legacy callers. It routes through ConfirmDetailed and discards the Phase 5A
// extras so the type is usable wherever a plain Confirmer is wanted.
func (c *Confirmer) Confirm(ctx context.Context, req safety.ConfirmationRequest) (bool, error) {
	resp, err := c.ConfirmDetailed(ctx, req)
	return resp.Approved, err
}

// ConfirmDetailed satisfies safety.DetailedConfirmer (Phase 5A): it blocks
// until the TUI collects a full Response (approve / reject-with-reason). The
// loop prefers this method via type assertion and falls back to Confirm for
// non-detailed confirmers, so both kinds interoperate.
func (c *Confirmer) ConfirmDetailed(ctx context.Context, req safety.ConfirmationRequest) (safety.Response, error) {
	if c == nil {
		return safety.Response{}, nil
	}
	request := confirmationRequest{req: req, response: make(chan safety.Response, 1)}
	c.mu.Lock()
	if c.queuedResponse != nil {
		resp := *c.queuedResponse
		c.queuedResponse = nil
		c.mu.Unlock()
		return resp, nil
	}
	c.pending = &request
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		c.mu.Lock()
		if c.pending == &request {
			c.pending = nil
		}
		c.mu.Unlock()
		return safety.Response{}, ctx.Err()
	case resp := <-request.response:
		return resp, nil
	}
}

// Respond satisfies the legacy bool responder: it forwards to RespondDetailed
// so existing callers (tests, scripted paths) keep working.
func (c *Confirmer) Respond(approved bool) {
	c.RespondDetailed(safety.Response{Approved: approved})
}

// RespondDetailed delivers a full Phase 5A Response to the blocked
// ConfirmDetailed call. If no call is pending, the response is queued for the
// next Confirm/ConfirmDetailed (mirroring the legacy queuedResponse behavior so
// a Respond that races ahead of the prompt is not lost).
func (c *Confirmer) RespondDetailed(resp safety.Response) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.pending != nil {
		request := c.pending
		c.pending = nil
		c.mu.Unlock()
		request.response <- resp
		return
	}
	respCopy := resp
	c.queuedResponse = &respCopy
	c.mu.Unlock()
}

func confirmationPrompt(event agent.Event) string {
	if event.Risk == "high" {
		return fmt.Sprintf("Approve to execute %s", event.ToolName)
	}
	return fmt.Sprintf("Type y or yes to execute %s", event.ToolName)
}

// renderPermissionPanel renders the agent-driven confirmation panel. Phase 5A
// extends it from a yes/no toggle to a three-way choice (Allow once / Always /
// Reject) at non-high risk, plus an inline reject-reason input mode. High-risk
// prompts keep the strict Yes/No — Allow-always is never offered there (safety
// invariant: high-risk always re-confirms).
//
// alwaysAvailable controls whether the Always option is selectable: it requires
// a configured RuleSource AND a non-high risk. When false the Always entry is
// shown dim and the cursor skips over it.
func renderPermissionPanel(event *agent.Event, choice confirmChoice, enteringReject bool, rejectReason string, alwaysAvailable bool, width int, animFrame int) string {
	if event == nil {
		return ""
	}
	if width < 24 {
		width = 24
	}
	tool := toolBlockFromEvent(*event, ToolPending)
	subject := toolActivitySubject(tool)
	if subject == "" {
		subject = event.Message
	}
	risk := defaultString(event.Risk, "unknown")
	riskStyle := warningStyle
	if strings.EqualFold(risk, "high") {
		riskStyle = errorStyle
	}
	// Left accent bar pulses while waiting for a click/key (Grok takeover feel).
	bar := animPermissionBar(animFrame)
	lines := []string{
		bar + confirmStyle.Render("Permission required"),
		bar + accentStyle.Render(event.ToolName) + " " + pathStyle.Render(subject),
		bar + riskStyle.Render("Risk: "+risk),
	}
	// For edit/write tools, show the actual change so the user can see what they
	// are approving before deciding.
	if toolIsRegistered(event.ToolName) {
		if detail := RendererFor(event.ToolName).Detail(tool, width, false); detail != "" {
			lines = append(lines, "")
			for _, l := range strings.Split(detail, "\n") {
				lines = append(lines, truncatePlain(l, max(1, width)))
			}
		}
	}

	switch {
	case enteringReject:
		lines = append(lines, "")
		lines = append(lines, confirmStyle.Render("Reject reason (optional):"))
		reason := rejectReason
		if reason == "" {
			reason = dimStyle.Render("type a reason, or press Enter for none")
		}
		lines = append(lines, accentStyle.Render("❯ ")+reason+"_")
		lines = append(lines, dimStyle.Render("enter submit · esc back"))
	case strings.EqualFold(event.Risk, "high"):
		// High-risk: strict Yes/No as vertical option rows (Grok permission language).
		active := 0
		if choice != choiceOnce {
			active = 1
		}
		lines = append(lines, "", FormatPermissionOptionList([]string{"Yes", "No"}, active))
		// Keep space-separated tokens so existing tests match "enter confirm".
		lines = append(lines, dimStyle.Render("↑↓ select · enter confirm · esc reject"))
	default:
		// Non-high: vertical Allow once / Always / Reject rows.
		opts := []string{"Allow once", "Always", "Reject"}
		active := 0
		switch choice {
		case choiceAlways:
			if alwaysAvailable {
				active = 1
			}
		case choiceReject:
			active = 2
		}
		optionBlock := FormatPermissionOptionList(opts, active)
		if !alwaysAvailable {
			// Dim the Always row while keeping vertical structure.
			optionLines := strings.Split(optionBlock, "\n")
			if len(optionLines) >= 2 {
				if active == 1 {
					optionLines[0] = FormatPermissionOptionRow("Allow once", true)
				}
				optionLines[1] = dimStyle.Render("  Always")
				optionBlock = strings.Join(optionLines, "\n")
			}
		}
		lines = append(lines, "", optionBlock)
		// Space-separated tokens keep "y allow once" discoverable for tests/docs.
		hint := "y allow once · a always · n reject · r w/ reason"
		if !alwaysAvailable {
			hint = "y allow once · n reject · r w/ reason"
		}
		lines = append(lines, dimStyle.Render(hint))
	}

	for i, line := range lines {
		lines[i] = truncatePlain(line, max(1, width))
	}
	return strings.Join(lines, "\n")
}
