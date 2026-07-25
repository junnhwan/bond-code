package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/junnhwan/bond-code/internal/ask"
)

// Questioner bridges the ask_user tool (running inside the agent goroutine) to
// the TUI main loop. Ask blocks until Respond is called; the TUI polls
// HasPending on each Update (driven by the spinner tick while the agent is
// busy) and renders the pending question as a selectable panel.
//
// It mirrors tui.Confirmer, but returns a set of selected indices instead of a
// single bool.
type Questioner struct {
	mu      sync.Mutex
	pending *questionRequest
}

type questionRequest struct {
	question ask.Question
	answer   chan ask.Answer
}

// NewQuestioner builds a Questioner ready to mediate ask_user questions.
func NewQuestioner() *Questioner {
	return &Questioner{}
}

func (q *Questioner) Ask(ctx context.Context, question ask.Question) (ask.Answer, error) {
	if q == nil {
		return nil, fmt.Errorf("questioner is not configured")
	}
	req := &questionRequest{question: question, answer: make(chan ask.Answer, 1)}
	q.mu.Lock()
	q.pending = req
	q.mu.Unlock()
	select {
	case <-ctx.Done():
		q.mu.Lock()
		if q.pending == req {
			q.pending = nil
		}
		q.mu.Unlock()
		return nil, ctx.Err()
	case ans := <-req.answer:
		return ans, nil
	}
}

// Respond delivers the user's answer to the blocked Ask and clears the pending
// question. A nil/empty answer signals the user dismissed the question.
func (q *Questioner) Respond(ans ask.Answer) {
	if q == nil {
		return
	}
	q.mu.Lock()
	req := q.pending
	q.pending = nil
	q.mu.Unlock()
	if req != nil {
		req.answer <- ans
	}
}

// HasPending reports whether a question is awaiting an answer.
func (q *Questioner) HasPending() bool {
	if q == nil {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pending != nil
}

// PendingQuestion returns the question awaiting an answer, or the zero Question
// when none is pending.
func (q *Questioner) PendingQuestion() ask.Question {
	if q == nil {
		return ask.Question{}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pending == nil {
		return ask.Question{}
	}
	return q.pending.question
}

// syncPendingQuestion pulls a pending ask_user question into local UI state so
// it renders as a selectable panel. It is a no-op while a question is already
// being answered, so a fresh question only appears once the previous one is
// confirmed/cancelled.
func (m Model) syncPendingQuestion() Model {
	if m.questioner == nil || m.question != nil {
		return m
	}
	if m.agent.Pending != nil {
		return m
	}
	if !m.questioner.HasPending() {
		return m
	}
	q := m.questioner.PendingQuestion()
	m.question = &q
	m = m.closeHistoryOverlay()
	if m.composer.Suggestions != nil {
		m.composer.Suggestions.Hide()
	}
	m.questionCursor = 0
	m.questionSelected = nil
	return m
}

func (m Model) deferQuestionDock() Model {
	m.question = nil
	m.questionCursor = 0
	m.questionSelected = nil
	return m
}

// handleQuestionKey intercepts keys while a question panel is open: arrows
// move the cursor, space toggles a multi-select option, enter confirms, esc
// dismisses. Returns handled=false for keys it does not own.
func (m Model) handleQuestionKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.question == nil {
		return m, nil, false
	}
	maxIdx := len(m.question.Options) - 1
	switch msg.String() {
	case "up", "k", "ctrl+p":
		if maxIdx >= 0 {
			m.questionCursor = (m.questionCursor - 1 + len(m.question.Options)) % len(m.question.Options)
		}
		return m, nil, true
	case "down", "j", "ctrl+n":
		if maxIdx >= 0 {
			m.questionCursor = (m.questionCursor + 1) % len(m.question.Options)
		}
		return m, nil, true
	case " ":
		if m.question.Multi {
			if m.questionSelected == nil {
				m.questionSelected = map[int]bool{}
			}
			m.questionSelected[m.questionCursor] = !m.questionSelected[m.questionCursor]
		}
		return m, nil, true
	case "enter":
		m = m.confirmQuestion()
		return m, m.waitForAgent(), true
	case "esc":
		m = m.cancelQuestion()
		return m, m.waitForAgent(), true
	}
	if idx, ok := questionNumberShortcut(msg); ok {
		if idx >= 0 && idx <= maxIdx {
			m.questionCursor = idx
			if m.question.Multi {
				if m.questionSelected == nil {
					m.questionSelected = map[int]bool{}
				}
				m.questionSelected[idx] = !m.questionSelected[idx]
				return m, nil, true
			}
			m = m.confirmQuestion()
			return m, m.waitForAgent(), true
		}
		return m, nil, true
	}
	key := msg.String()
	switch key {
	case "ctrl+c":
		return m, nil, false
	default:
		if isTimelineScrollKey(key) {
			return m, nil, false
		}
		return m, nil, true
	}
}

func questionNumberShortcut(msg tea.KeyMsg) (int, bool) {
	if len(msg.Runes) != 1 {
		return 0, false
	}
	r := msg.Runes[0]
	if r < '1' || r > '9' {
		return 0, false
	}
	return int(r - '1'), true
}

// confirmQuestion sends the current selection to the blocked Ask and closes the
// panel. Multi-select defaults to the cursor when nothing was toggled.
func (m Model) confirmQuestion() Model {
	if m.question == nil || m.questioner == nil {
		return m
	}
	var ans ask.Answer
	if m.question.Multi {
		for i := range m.question.Options {
			if m.questionSelected[i] {
				ans = append(ans, i)
			}
		}
		if len(ans) == 0 {
			ans = ask.Answer{m.questionCursor}
		}
	} else {
		ans = ask.Answer{m.questionCursor}
	}
	m.questioner.Respond(ans)
	m.question = nil
	m.questionSelected = nil
	return m
}

func (m Model) cancelQuestion() Model {
	if m.questioner != nil {
		m.questioner.Respond(nil)
	}
	m.question = nil
	m.questionSelected = nil
	return m
}

// renderQuestionPanel draws the pending question and its choices as a focused
// panel pinned above the composer, mirroring renderPermissionPanel.
func renderQuestionPanel(question *ask.Question, cursor int, selected map[int]bool, width int) string {
	return renderQuestionPanelForHeight(question, cursor, selected, width, 0)
}

func renderQuestionPanelForHeight(question *ask.Question, cursor int, selected map[int]bool, width int, maxHeight int) string {
	if question == nil {
		return ""
	}
	if width < 24 {
		width = 24
	}
	lines := []string{confirmStyle.Render("◆ " + strings.TrimSpace(question.Prompt))}
	optionLines := renderQuestionOptionLines(question, cursor, selected, width)
	if maxHeight > 0 {
		availableOptions := maxHeight - 3 // prompt + blank line + hint
		optionLines = windowQuestionOptions(optionLines, cursor, availableOptions)
	}
	lines = append(lines, optionLines...)
	hint := "↑↓ select · Enter confirm · Esc skip"
	if question.Multi {
		hint = "↑↓ select · Space toggle · Enter confirm · Esc skip"
	}
	lines = append(lines, "", dimStyle.Render(hint))
	for i := range lines {
		lines[i] = truncatePlain(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

func renderQuestionOptionLines(question *ask.Question, cursor int, selected map[int]bool, width int) []string {
	lines := make([]string, 0, len(question.Options))
	for i, opt := range question.Options {
		var prefix, label string
		if question.Multi {
			box := "☐"
			if selected[i] {
				box = "☑"
			}
			if i == cursor {
				prefix = confirmStyle.Render("❯ " + box + " ")
			} else {
				prefix = dimStyle.Render("  " + box + " ")
			}
			label = opt.Label
		} else {
			if i == cursor {
				prefix = confirmStyle.Render("❯ ")
				label = opt.Label
			} else {
				prefix = "  "
				label = dimStyle.Render(opt.Label)
			}
		}
		line := prefix + label
		if desc := strings.TrimSpace(opt.Description); desc != "" {
			limit := width - lipgloss.Width(line) - 4
			if limit < 8 {
				limit = 8
			}
			line += "  " + dimStyle.Render(truncatePlain(desc, limit))
		}
		lines = append(lines, line)
	}
	return lines
}

func windowQuestionOptions(lines []string, cursor int, maxVisible int) []string {
	if maxVisible < 1 {
		maxVisible = 1
	}
	if len(lines) <= maxVisible {
		return lines
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(lines) {
		cursor = len(lines) - 1
	}
	start := cursor - maxVisible/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(lines) - maxVisible; start > maxStart {
		start = maxStart
	}
	return lines[start : start+maxVisible]
}
