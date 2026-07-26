package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/safety"
)

func (m Model) handleConfirmationKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	key := strings.ToLower(strings.TrimSpace(msg.String()))

	// Reject-reason input mode (Phase 5A): dedicated handling. Enter submits
	// (reason may be empty), Esc returns to the choice panel without rejecting.
	if m.agent.ConfirmEnteringReject {
		return m.handleRejectReasonKey(msg, key)
	}
	if isTimelineScrollKey(key) {
		return m, nil, false
	}

	high := isHighRisk(m.agent.Pending)
	alwaysAvail := m.alwaysAvailable()

	switch key {
	case "enter":
		if high {
			return m.confirmCurrentChoice()
		}
		// Non-high: Enter confirms the highlighted vertical row (↑↓ to move).
		// Default selection is Allow once — same as an explicit y.
		return m.confirmCurrentChoice()
	case "up", "k", "left", "h":
		// Options are vertical; ↑/k move toward the top row. ←/h keep the
		// older horizontal binding as an alias so muscle memory still works.
		m.agent.ConfirmChoice = m.moveChoice(-1, alwaysAvail)
		return m, nil, true
	case "down", "j", "right", "l":
		m.agent.ConfirmChoice = m.moveChoice(+1, alwaysAvail)
		return m, nil, true
	case "tab":
		if high {
			// High-risk tab toggles once<->reject (original semantics).
			if m.agent.ConfirmChoice == choiceOnce {
				m.agent.ConfirmChoice = choiceReject
			} else {
				m.agent.ConfirmChoice = choiceOnce
			}
		} else {
			m.agent.ConfirmChoice = m.moveChoice(+1, alwaysAvail)
		}
		return m, nil, true
	case "y":
		m.agent.ConfirmChoice = choiceOnce
		return m.confirmCurrentChoice()
	case "a":
		// 'a' picks Always when available, else falls back to approve-once
		// (keeps the legacy "a = approve" behavior when Allow-always is off).
		if alwaysAvail {
			m.agent.ConfirmChoice = choiceAlways
		} else {
			m.agent.ConfirmChoice = choiceOnce
		}
		return m.confirmCurrentChoice()
	case "r":
		// Reject with an optional reason fed back to the model (Phase 5A).
		m.agent.ConfirmChoice = choiceReject
		m.agent.ConfirmEnteringReject = true
		m.agent.ConfirmRejectReason = ""
		return m, nil, true
	case "esc", "n":
		m.agent.ConfirmChoice = choiceReject
		return m.confirmCurrentChoice()
	default:
		// The confirmation panel owns all keys except Ctrl+C (cancel/exit) so a
		// pending prompt cannot be bypassed by typing into the composer.
		if key == "ctrl+c" {
			return m, nil, false
		}
		return m, nil, true
	}
}

// handleRejectReasonKey handles input while the user types an optional reject
// reason (Phase 5A). Mirrors overlayPrompt: Enter submits (reason may be
// empty), Esc returns to the choice panel, Backspace deletes one rune.
func (m Model) handleRejectReasonKey(msg tea.KeyMsg, key string) (Model, tea.Cmd, bool) {
	switch key {
	case "enter":
		return m.confirmCurrentChoice()
	case "esc":
		m.agent.ConfirmEnteringReject = false
		return m, nil, true
	case "backspace", "ctrl+h":
		m.agent.ConfirmRejectReason = trimLastByte(m.agent.ConfirmRejectReason)
		return m, nil, true
	case "ctrl+u", "ctrl+w":
		m.agent.ConfirmRejectReason = ""
		return m, nil, true
	default:
		if msg.Type == tea.KeyRunes {
			if added := printableRunes(msg); added != "" {
				m.agent.ConfirmRejectReason += added
			}
		}
		return m, nil, true
	}
}

// confirmCurrentChoice turns the current selection into a Response and dispatches
// it. Reject carries the in-progress reason (empty unless the user entered
// reject-reason mode via 'r').
func (m Model) confirmCurrentChoice() (Model, tea.Cmd, bool) {
	next, cmd := m.respondToConfirmation(m.agent.ConfirmChoice, m.agent.ConfirmRejectReason)
	return next, cmd, true
}

// moveChoice returns the next/prev selectable choice, cycling at both ends.
// Choices follow the vertical order renderPermissionPanel draws (top→bottom):
// high-risk Yes/No, or Allow once / Always / Reject. Positive delta moves down
// the list (and wraps); negative moves up. (High-risk never offers Always —
// alwaysAvail is false there.)
func (m Model) moveChoice(delta int, alwaysAvail bool) confirmChoice {
	choices := []confirmChoice{choiceOnce, choiceReject}
	if alwaysAvail {
		choices = []confirmChoice{choiceOnce, choiceAlways, choiceReject}
	}
	cur := m.agent.ConfirmChoice
	idx := 0
	for i, c := range choices {
		if c == cur {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(choices)) % len(choices)
	return choices[idx]
}

// alwaysAvailable reports whether Allow-always is selectable for the current
// prompt: a configured RuleSource AND non-high risk (high-risk never offers
// Always — safety invariant).
func (m Model) alwaysAvailable() bool {
	if m.cfg.RuleSource == nil || m.agent.Pending == nil {
		return false
	}
	return !isHighRisk(m.agent.Pending)
}

func isHighRisk(event *agent.Event) bool {
	return event != nil && strings.EqualFold(strings.TrimSpace(event.Risk), "high")
}
func (m Model) respondToConfirmation(choice confirmChoice, reason string) (Model, tea.Cmd) {
	pending := m.agent.Pending
	m.agent.Pending = nil
	m.agent.ConfirmEnteringReject = false
	m.agent.ConfirmRejectReason = ""
	if m.cfg.Confirmer == nil {
		body := "no TUI confirmer is configured"
		if pending != nil {
			tool := toolBlockFromEvent(*pending, ToolFailed)
			tool.Error = body
			tool.Summary = body
			m.timeline = m.timeline.UpsertToolBlock(tool)
			m = m.markNewOutputBelow()
		}
		m.timeline = m.timeline.AppendBlock(BlockError, "confirm", body)
		return m.markNewOutputBelow(), m.waitForAgent()
	}
	// Phase 5A: Allow-always persists a session rule so the next same-kind call
	// auto-approves. PatternKey returns "" for unsupported tools (the Always
	// option is then hidden); a failed Add is non-fatal — the call still runs
	// approved this once. High-risk never reaches here (alwaysAvailable is
	// false), enforcing the "high-risk always re-confirms" invariant.
	if choice == choiceAlways && pending != nil {
		if rs := m.cfg.RuleSource; rs != nil {
			if pk := safety.PatternKey(pending.ToolName, pending.Input); pk != "" {
				_ = rs.Add(safety.PermissionRule{Tools: []string{pending.ToolName}, Pattern: pk, Decision: "allow"})
			}
		}
	}
	resp := safety.Response{Approved: choice.approved()}
	if choice == choiceReject {
		resp.RejectReason = strings.TrimSpace(reason)
	}
	m.cfg.Confirmer.RespondDetailed(resp)
	return m, m.waitForAgent()
}
