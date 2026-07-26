package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/contextx"
)

// keyRoute identifies the highest-priority keyboard owner. Only one route may
// receive a key, even if stale state temporarily leaves multiple layers open.
type keyRoute uint8

const (
	keyRouteComposer keyRoute = iota
	keyRouteSearch
	keyRouteOverlay
	keyRouteHistory
	keyRouteQuestion
	keyRouteConfirmation
)

func (m Model) activeKeyRoute() keyRoute {
	switch {
	case m.agent.Pending != nil:
		return keyRouteConfirmation
	case m.question != nil:
		return keyRouteQuestion
	case m.history.visible:
		return keyRouteHistory
	case m.overlay.active():
		return keyRouteOverlay
	case m.search.Active:
		return keyRouteSearch
	default:
		return keyRouteComposer
	}
}

func (m Model) handleViewportMessage(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m = m.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.MouseMsg:
		return m.handleMouseMsg(msg)

	default:
		return m, nil
	}
}

func (m Model) routeModalKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch m.activeKeyRoute() {
	case keyRouteConfirmation:
		return m.handleConfirmationKey(msg)
	case keyRouteQuestion:
		return m.handleQuestionKey(msg)
	case keyRouteHistory:
		return m.handleHistoryKey(msg)
	case keyRouteOverlay:
		return m.handleOverlayKey(msg)
	case keyRouteSearch:
		return m.handleSearchKey(msg)
	default:
		return m, nil, false
	}
}

// handleGlobalKey contains the small set of keys allowed to escape a selected
// modal owner. Keeping this list narrow prevents an unhandled key from reaching
// a stale lower modal or activating an unrelated composer shortcut.
func (m Model) handleGlobalKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c":
		if m.agent.Busy {
			return m.cancelRunningAgent(), nil, true
		}
		if m.inputValue() != "" {
			return m.clearInput(), nil, true
		}
		// Leave a stable idle title on the tab when quitting.
		return m, tea.Batch(tea.SetWindowTitle(m.idleTerminalTitle()), tea.Quit), true
	case "pgup":
		return m.scrollBy(pageStep(m.height)), nil, true
	case "pgdown":
		return m.scrollBy(-pageStep(m.height)), nil, true
	default:
		return m, nil, false
	}
}

func (m Model) handleKeyMessage(msg tea.KeyMsg) (Model, tea.Cmd) {
	next, cmd := m.handleKeyMessageInner(msg)
	next, focusCmd := next.applyComposerFocus()
	return next, tea.Batch(cmd, focusCmd)
}

func (m Model) handleKeyMessageInner(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.activeKeyRoute() != keyRouteComposer {
		next, cmd, handled := m.routeModalKey(msg)
		if handled {
			return next, cmd
		}
		if next, cmd, handled = next.handleGlobalKey(msg); handled {
			return next, cmd
		}
		// The selected modal remains the sole owner. An unhandled key may only
		// reach the explicit global list above, never lower hidden layers.
		return next, nil
	}
	if next, cmd, handled := m.handleFocusKey(msg); handled {
		return next, cmd
	}
	if next, cmd, handled := m.handleGlobalKey(msg); handled {
		return next, cmd
	}
	key := msg.String()
	if isComposerNewlineKey(key) {
		return m.insertNewline(), nil
	}
	if isModeCycleKey(key) {
		return m.cycleMode(), nil
	}
	if !msg.Paste && msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		m.composer = m.composer.observeRawPasteRunes(time.Now(), len(msg.Runes))
	}

	switch key {
	case "ctrl+o":
		// Single semantic: expand/collapse transcript tool details (Claude
		// ctrl+o). Do not thrash showToolDetails — that hid completed tools
		// while verbose tried to expand them. Thinking is Ctrl+T only.
		return m.toggleExpandedTranscript(), nil
	case "ctrl+t":
		// Historical thinking on/off. Independent of Ctrl+O tool density.
		return m.toggleThinking(), nil
	case "ctrl+shift+m":
		// Toggle mouse capture: off restores terminal drag-select/copy.
		return m.toggleMouseCapture()
	case "ctrl+g":
		return m.openExternalEditor()
	case "ctrl+s":
		return m.stashLeaderAction(), nil
	case "ctrl+l":
		return m, tea.ClearScreen
	case "ctrl+u":
		// Half-page scroll only in scrollback focus; composer keeps line-edit semantics.
		if m.focus == FocusScrollback {
			return m.scrollBy(pageStep(m.height) / 2), nil
		}
	case "ctrl+d":
		if m.focus == FocusScrollback {
			return m.scrollBy(-pageStep(m.height) / 2), nil
		}
		if m.inputValue() == "" {
			return m, tea.Batch(tea.SetWindowTitle(m.idleTerminalTitle()), tea.Quit)
		}
	case "ctrl+r":
		if m.focus != FocusComposer {
			return m, nil
		}
		return m.startReverseHistorySearch(), nil
	case "alt+left", "alt+right":
		if m.inputValue() == "" {
			// Bubbles' word-navigation loop does not advance on an empty
			// textarea. There is nothing to edit, so avoid entering it.
			return m, nil
		}
	case "esc":
		if m.composer.Suggestions != nil && m.composer.Suggestions.IsVisible() {
			m.composer.Suggestions.Hide()
			if isEmptySuggestionPrompt(m.inputValue()) {
				m = m.clearInput()
			}
			return m, nil
		}
		if m.agent.Pending != nil {
			return m.respondToConfirmation(choiceReject, "")
		}
		if m.agent.Busy {
			return m.cancelRunningAgent(), nil
		}
		if m.focus == FocusScrollback {
			return m.withFocus(FocusComposer)
		}
		return m.clearInput(), nil
	case "up":
		if m.focus == FocusScrollback {
			return m.moveScrollSelection(-1), nil
		}
		if m.composer.Suggestions != nil && m.composer.Suggestions.IsVisible() {
			m.composer.Suggestions.SelectPrev(m.getCommandFilter())
			return m, nil
		}
		if m.canUsePromptHistory() {
			return m.previousHistory(), nil
		}
		// Otherwise fall through to textarea cursor movement.
	case "down":
		if m.focus == FocusScrollback {
			return m.moveScrollSelection(1), nil
		}
		if m.composer.Suggestions != nil && m.composer.Suggestions.IsVisible() {
			m.composer.Suggestions.SelectNext(m.getCommandFilter())
			return m, nil
		}
		if m.composer.HistoryIndex >= 0 {
			return m.nextHistory(), nil
		}
		// Otherwise fall through to textarea cursor movement.
	case "left", "right":
		// Fold/expand selected entry. Prefer arrows only — letter keys (h/l)
		// must letter-auto-focus the prompt in Grok Simple mode.
		if m.focus == FocusScrollback {
			return m.toggleSelectedFold(), nil
		}
	case "tab":
		if m.composer.Suggestions != nil && m.composer.Suggestions.IsVisible() {
			filter := m.getCommandFilter()
			selected := m.composer.Suggestions.GetSelected(filter)
			if selected != "" {
				m = m.completeSelectedSuggestion(filter, selected)
				m.composer.Suggestions.Hide()
			}
			return m, nil
		}
		// Toggle prompt ↔ scrollback focus (Grok Simple mode).
		if m.focus == FocusScrollback {
			return m.withFocus(FocusComposer)
		}
		if m.focus == FocusComposer {
			next, cmd := m.withFocus(FocusScrollback)
			// Seed selection at the latest entry when entering scrollback.
			if next.scrollSel < 0 {
				entries := next.scrollEntries()
				if len(entries) > 0 {
					next.scrollSel = len(entries) - 1
				}
			}
			return next, cmd
		}
		return m, nil
	case " ":
		// Space focuses prompt from scrollback without inserting a space,
		// even when the draft is already non-empty (Grok Simple mode).
		if m.focus == FocusScrollback {
			return m.withFocus(FocusComposer)
		}
	case "enter":
		// Open latest/running subagent fullscreen when scrollback-focused.
		if m.focus == FocusScrollback {
			if id := m.preferredSubagentID(); id != "" {
				m = m.enterAgentWindow(id)
				return m, nil
			}
			// Enter on empty scrollback also focuses prompt (discoverability).
			return m.withFocus(FocusComposer)
		}
		// Windows console input exposes a paste as rune/Enter events and Bubble
		// Tea reads those events in small batches. Classify the preceding burst
		// by typing speed instead of relying on a fragile 20 ms last-event timer.
		var pastedNewline bool
		m.composer, pastedNewline = m.composer.consumeRawPasteEnter(time.Now())
		if pastedNewline {
			return m.insertNewline(), nil
		}
		// Accept the highlighted suggestion first. File mentions only fill the
		// composer. Slash commands follow Claude Code: complete into `/name `
		// always; auto-submit only when the command does not expect free-text
		// args (skills / custom prompt templates stay open so the user can type
		// a prompt after the name — Enter alone used to fire too early).
		if m.composer.Suggestions != nil && m.composer.Suggestions.IsVisible() {
			filter := m.getCommandFilter()
			item, ok := m.composer.Suggestions.GetSelectedItem(filter)
			if ok && item.Name != "" {
				prefix := m.composer.Suggestions.CurrentPrefix()
				m = m.completeSelectedSuggestion(filter, item.Name)
				m.composer.Suggestions.Hide()
				if prefix == "/" && slashSuggestionAutoSubmits(item, m.cfg.Commands) {
					return m.Submit(m.cfg.Context)
				}
				return m, nil
			}
		}
		return m.Submit(m.cfg.Context)
	}
	// Scrollback focus: Grok Simple mode — printable letters auto-focus the
	// prompt and insert; Space/Tab already handled above (focus only).
	if m.focus == FocusScrollback {
		if !msg.Paste && msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			if isLetterAutoFocusRunes(msg.Runes) {
				next, focusCmd := m.withFocus(FocusComposer)
				next, typeCmd := next.updateComposer(msg)
				return next, tea.Batch(focusCmd, typeCmd)
			}
		}
		return m, nil
	}
	// Main prompt, or agent-window child draft when input routing is enabled.
	if m.focus == FocusComposer || (m.focus == FocusAgentWindow && m.cfg.SendSubagentInput != nil) {
		return m.updateComposer(msg)
	}
	return m, nil
}

// isLetterAutoFocusRunes reports whether runes should auto-focus the prompt
// (Grok Simple mode). Space is handled separately (focus without insert);
// control/non-printable runes never auto-focus.
func isLetterAutoFocusRunes(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	for _, r := range runes {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return false
		}
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func (m Model) handleAgentMessage(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case runAgentMsg:
		return m.handleRunAgentMessage(msg)
	case agentEventMsg:
		return m.handleAgentEventMessage(msg)
	case agentDoneMsg:
		return m.handleAgentDoneMessage(msg)
	case agentTickMsg:
		return m.handleAgentTickMessage(msg)
	case subagentInputResultMsg:
		if msg.err != nil {
			if trace := m.subagentTraces[msg.taskID]; trace != nil {
				for i := range trace.Blocks {
					if trace.Blocks[i].ID == msg.blockID {
						trace.Blocks[i].Title = "you (failed)"
						trace.Blocks[i].Summary = msg.err.Error()
						break
					}
				}
				if trace.Draft == "" {
					trace.Draft = msg.input
				}
			}
			if m.focus == FocusAgentWindow && m.focusedTaskID == msg.taskID && strings.TrimSpace(m.inputValue()) == "" {
				m.composer = m.composer.setValue(msg.input)
			}
			m = m.pushToast("Agent input failed: "+msg.err.Error(), toastError)
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) handleRunAgentMessage(msg runAgentMsg) (Model, tea.Cmd) {
	started, cmd := m.startAgent(msg.prompt)
	started.animTickArmed = true
	return started, tea.Batch(cmd, started.spinner.Tick)
}

func (m Model) handleAgentEventMessage(msg agentEventMsg) (Model, tea.Cmd) {
	m.prof.count("event")
	if msg.runGeneration != 0 && msg.runGeneration != m.agent.RunGeneration {
		return m, nil
	}
	if m.agent.TerminalHandled {
		// Cancellation/terminal events freeze this run. An already-returned
		// event must not recreate live text or reset the terminal state.
		return m, nil
	}
	// Apply every event immediately. Bubble Tea's renderer coalesces terminal
	// writes; delaying structural events here only makes tools feel late.
	m = m.ApplyAgentEvent(msg.event)

	// Drain already-queued *text* chunks in this Update so many tokens become
	// one frame (line flushes feel snappier). No sleep/timer — non-blocking only.
	// Stop before structural/done events so waitForAgent still owns them.
	if m.agent.Stream != nil && isStreamTextEvent(msg.event.Type) {
		m = m.drainQueuedTextChunks(msg.runGeneration)
	}

	wait := m.waitForAgent()
	// Window-title updates only on structural transitions — never on model/
	// reasoning text chunks — so the sole agent-reader command stays a pure
	// channel wait (streaming invariant) and PowerShell tabs are not spammed.
	switch msg.event.Type {
	case agent.EventToolRequested, agent.EventToolResult, agent.EventToolConfirmationRequested,
		agent.EventToolApproved, agent.EventToolRejected, agent.EventAgentFinished,
		agent.EventAgentError, agent.EventSubagentStarted, agent.EventSubagentFinished,
		agent.EventSubagentFailed, agent.EventSubagentCancelled:
		m, titleCmd := m.maybeSetTerminalTitle()
		if titleCmd != nil {
			return m, tea.Batch(wait, titleCmd)
		}
	}
	return m, wait
}

func isStreamTextEvent(t agent.EventType) bool {
	return t == agent.EventModelChunk || t == agent.EventReasoningChunk
}

// drainQueuedTextChunks non-blockingly applies consecutive model/reasoning
// chunks already sitting on the agent stream. If the next message is not a
// text chunk, it is left for waitForAgent by only draining while peeks would
// require a push-back — since Go channels cannot unread, we only drain when
// the head is known to be text via a short select that applies text and stops
// on anything else by... we cannot unread.
//
// Practical approach: drain only while we successfully receive agentEventMsg
// with text types. On non-text or done, apply nothing and **stop** — but we
// already consumed the message. So we must apply non-text events and return,
// and process done via handleAgentDoneMessage without re-waiting on a nil stream.
//
// Safer minimal approach used here: only drain when select gets another text
// chunk; on non-text, apply it immediately (preserve order) and return; on
// done, run the done handler and clear the need for wait (caller checks Stream).
func (m Model) drainQueuedTextChunks(runGeneration uint64) Model {
	if m.agent.Stream == nil {
		return m
	}
	const maxDrain = 64
	for i := 0; i < maxDrain; i++ {
		select {
		case msg, ok := <-m.agent.Stream:
			if !ok {
				return m
			}
			switch msg := msg.(type) {
			case agentEventMsg:
				if msg.runGeneration != 0 && runGeneration != 0 && msg.runGeneration != runGeneration {
					continue
				}
				if m.agent.TerminalHandled {
					return m
				}
				if !isStreamTextEvent(msg.event.Type) {
					// Preserve order: apply structural event now, stop draining.
					return m.ApplyAgentEvent(msg.event)
				}
				m = m.ApplyAgentEvent(msg.event)
			case agentDoneMsg:
				// Consumed done — finish the run here; caller still invokes
				// waitForAgent which is safe only if Stream remains until done
				// handler clears it. handleAgentDoneMessage clears Stream.
				next, _ := m.handleAgentDoneMessage(msg)
				return next
			default:
				return m
			}
		default:
			return m
		}
	}
	return m
}

func (m Model) handleAgentDoneMessage(msg agentDoneMsg) (Model, tea.Cmd) {
	if msg.runGeneration != 0 && msg.runGeneration != m.agent.RunGeneration {
		// A reader from an older run completed after a replacement run began.
		// It must not clean up the replacement stream or drain its queue.
		return m, nil
	}
	cancelled := errors.Is(msg.err, context.Canceled)
	alreadyHandled := m.agent.TerminalHandled
	m = m.commitLiveStream()
	m.agent.Busy = false
	m.agent.Stream = nil
	m.agent.LiveStream = nil
	m.agent.LiveDetail = ""
	m.question = nil
	m.stopAgent()
	if !alreadyHandled {
		if msg.err != nil {
			m.agent.Err = msg.err
			state := "failed"
			if cancelled {
				state = "cancelled"
			}
			body := humanizeAgentError(msg.err.Error())
			m.timeline = m.timeline.MarkAgentEnded(state, body, time.Now())
			if !cancelled {
				m.timeline = m.timeline.AppendBlock(BlockError, "error", body)
				m = m.markNewOutputBelow()
			}
		} else {
			m.agent.Err = nil
			m.timeline = m.timeline.MarkAgentEnded("done", "", time.Now())
			m = m.markNewOutputNotice("agent done")
		}
		m.agent.TerminalHandled = true
	}
	if cancelled || m.latestRunState() == "cancelled" {
		// Local/remote cancellation never auto-runs prompts queued before the
		// user interrupted the turn.
		m.agent.QueuedPrompts = nil
	} else {
		m = m.refreshStatus()
	}
	// EventAgentFinished may have marked terminal before this cleanup message;
	// queue draining still belongs here and therefore cannot be skipped.
	if len(m.agent.QueuedPrompts) > 0 {
		next := m.agent.QueuedPrompts[0]
		m.agent.QueuedPrompts = m.agent.QueuedPrompts[1:]
		m = m.beginUserTurn(next)
		agentPrompt := contextx.ExpandPathMentions(next, m.cfg.Status.ProjectRoot)
		started, cmd := m.startAgent(agentPrompt)
		started.animTickArmed = true
		return started, tea.Batch(cmd, started.spinner.Tick)
	}
	return m, nil
}

func (m Model) handleAgentTickMessage(msg agentTickMsg) (Model, tea.Cmd) {
	if msg.runGeneration != 0 && msg.runGeneration != m.agent.RunGeneration {
		return m, nil
	}
	m = m.tickToasts()
	if !m.agent.Busy {
		return m, nil
	}
	return m, m.waitForAgent()
}

func (m Model) handleMiscMessage(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		m = m.tickToasts()
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		// Drive chrome animations (braille, accent wave, flash decay) without
		// touching committed timeline cache keys. Frame rate is governed by
		// the single spinner.Tick chain (animTickArmed prevents mouse motion
		// from stacking concurrent timers — that was the "spinner races when
		// I move the mouse" bug).
		m.animFrame++
		if m.flash.active() == false {
			m.flash = uiFlash{}
		}
		// Title glyph advances only every titleSpinnerDivisor ticks so PowerShell /
		// Windows Terminal tabs are not spammed with SetWindowTitle each frame.
		// Structural busy/pending transitions update the title on agent events;
		// spinner ticks only refresh the busy glyph on the divisor boundary.
		m.titleSpinnerFrame++
		var titleCmd tea.Cmd
		if m.titleSpinnerFrame%titleSpinnerDivisor == 0 {
			m, titleCmd = m.maybeSetTerminalTitle()
		}
		if m.needsAnimationTick() {
			m.animTickArmed = true
			return m, tea.Batch(cmd, titleCmd)
		}
		m.animTickArmed = false
		return m, titleCmd
	case editorDoneMsg:
		return m.applyEditorResult(msg)
	default:
		return m.updateComposer(msg)
	}
}

func (m Model) updateComposer(msg tea.Msg) (Model, tea.Cmd) {
	// Phase 5C.1: a bracketed paste arrives as a KeyMsg with Paste=true. Collapse
	// large pastes (>=3 lines or >150 chars) into a chip before the textarea
	// inserts the raw text, so a huge paste cannot push the composer to its max
	// height; the original text is re-expanded on Submit. Short pastes fall
	// through to the textarea as ordinary input.
	if kmsg, ok := msg.(tea.KeyMsg); ok && kmsg.Paste && shouldCollapsePaste(string(kmsg.Runes)) {
		m.composer = m.composer.addPaste(string(kmsg.Runes))
		m.layout = m.currentLayout()
		return m.clampScroll(m.layout), nil
	}

	beforeInput := m.composer.Input.Value()
	beforeHistoryIndex := m.composer.HistoryIndex
	var cmd tea.Cmd
	m.composer.Input, cmd = m.composer.Input.Update(msg)
	m.composer = m.composer.syncHeight()
	if beforeHistoryIndex >= 0 && m.composer.Input.Value() != beforeInput {
		m.composer.HistoryIndex = -1
		m.composer.HistoryDraft = ""
	}

	// Update suggestions based on input
	m = m.updateSuggestions()
	m.layout = m.currentLayout()
	m = m.clampScroll(m.layout)

	return m, cmd

}
