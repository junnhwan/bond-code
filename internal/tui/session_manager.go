package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The session manager (Phase 2.1/2.2) is a full-screen overlay that lists every
// session with its title / preview, pin state, age, and message count, and lets
// the user switch, rename, pin, or delete without leaving the TUI. The legacy
// numeric quick-switch helper remains available for compatibility.
//
// It is the third full-screen overlay (after the history browser and the diff
// viewer) and reuses the menu / prompt / confirm overlays for the per-session
// actions, so the actual management UI is built from already-proven pieces.

// sessionManagerState backs the manager overlay.
type sessionManagerState struct {
	entries  []SessionInfo
	selected int
	// marked are session IDs toggled for bulk delete (space). Active session
	// cannot be marked.
	marked  map[string]bool
	loadErr string
	loaded  bool
}

// openSessionManager loads the session list and enters the overlay.
func (m Model) openSessionManager() Model {
	sm := sessionManagerState{marked: map[string]bool{}}
	sm.entries, sm.loadErr = m.loadSessionList()
	sm.loaded = true
	// Land the cursor on the active session if present.
	for i, e := range sm.entries {
		if e.Active {
			sm.selected = i
			break
		}
	}
	m.overlay = overlayState{kind: overlaySessions, sessions: sm}
	return m
}

func (m Model) loadSessionList() ([]SessionInfo, string) {
	if m.cfg.SessionManager == nil {
		return nil, "session management is not configured"
	}
	entries, err := m.cfg.SessionManager.List()
	if err != nil {
		return nil, err.Error()
	}
	return entries, ""
}

// quickSwitchSession switches to the Nth session (1-based) in the
// pin-then-recency order. It remains as a compatibility helper; out-of-range is
// a no-op so callers remain safe even with few sessions.
func (m Model) quickSwitchSession(slot int) (Model, tea.Cmd) {
	if m.cfg.SessionManager == nil || slot < 1 {
		return m, nil
	}
	entries, err := m.cfg.SessionManager.List()
	if err != nil || slot > len(entries) {
		return m, nil
	}
	target := entries[slot-1]
	if target.Active {
		return m, nil
	}
	return m.switchToSession(target.ID)
}

// switchToSession flips the app onto id and reloads the view. The bookkeeping
// (push history, restore scroll) mirrors switchSessionFull.
func (m Model) switchToSession(id string) (Model, tea.Cmd) {
	if m.cfg.CommandEnv.SwitchSession == nil || m.cfg.ReloadSessionSeed == nil {
		return m, nil
	}
	if cur := m.cfg.Status.SessionID; cur != "" {
		m.sessionScrolls[cur] = m.scroll
	}
	if err := m.cfg.CommandEnv.SwitchSession(id); err != nil {
		return m.pushToast("switch failed: "+err.Error(), toastError), nil
	}
	m = m.reloadSessionView(id)
	m = m.pushSessionHistory(id)
	m = m.pushToast("switched session", toastInfo)
	return m, nil
}

func (m Model) handleSessionManagerKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	sm := m.overlay.sessions
	if sm.marked == nil {
		sm.marked = map[string]bool{}
	}
	if !sm.loaded {
		switch msg.String() {
		case "esc", "q", "ctrl+c", "ctrl+[":
			return m.closeOverlay(), nil, true
		}
		return m, nil, true
	}
	switch msg.String() {
	case "esc", "q", "ctrl+c", "ctrl+[":
		// Esc with marks: clear selection first so multi-select is easy to abort.
		if len(sm.marked) > 0 {
			sm.marked = map[string]bool{}
			m.overlay.sessions = sm
			return m, nil, true
		}
		return m.closeOverlay(), nil, true
	case "up", "k":
		sm.selected = clampInt(sm.selected-1, 0, max(0, len(sm.entries)-1))
		m.overlay.sessions = sm
		return m, nil, true
	case "down", "j":
		sm.selected = clampInt(sm.selected+1, 0, max(0, len(sm.entries)-1))
		m.overlay.sessions = sm
		return m, nil, true
	case "home", "g":
		sm.selected = 0
		m.overlay.sessions = sm
		return m, nil, true
	case "end", "G":
		sm.selected = max(0, len(sm.entries)-1)
		m.overlay.sessions = sm
		return m, nil, true
	case "pgup":
		sm.selected = clampInt(sm.selected-8, 0, max(0, len(sm.entries)-1))
		m.overlay.sessions = sm
		return m, nil, true
	case "pgdn":
		sm.selected = clampInt(sm.selected+8, 0, max(0, len(sm.entries)-1))
		m.overlay.sessions = sm
		return m, nil, true
	case " ":
		// Space toggles bulk-delete mark on the cursor row (not the active session).
		if sm.selected >= 0 && sm.selected < len(sm.entries) {
			entry := sm.entries[sm.selected]
			if entry.Active {
				return m, nil, true
			}
			if sm.marked[entry.ID] {
				delete(sm.marked, entry.ID)
			} else {
				sm.marked[entry.ID] = true
			}
			m.overlay.sessions = sm
			return m, nil, true
		}
		return m, nil, true
	case "d", "delete", "backspace":
		// Delete marked rows, or the cursor row when nothing is marked.
		ids := sm.deletableIDs()
		if len(ids) == 0 {
			return m, nil, true
		}
		return m.openBulkDeleteConfirm(ids), nil, true
	case "enter", "right", "l":
		// Enter with marks → bulk delete confirm; otherwise per-row actions.
		if n := len(sm.marked); n > 0 {
			ids := sm.deletableIDs()
			if len(ids) == 0 {
				return m, nil, true
			}
			return m.openBulkDeleteConfirm(ids), nil, true
		}
		if sm.selected >= 0 && sm.selected < len(sm.entries) {
			return m.openSessionActionsMenu(sm.selected), nil, true
		}
		return m, nil, true
	case "a":
		// Toggle-all markable (non-active) sessions — handy for cleanup.
		markable := 0
		marked := 0
		for _, e := range sm.entries {
			if e.Active {
				continue
			}
			markable++
			if sm.marked[e.ID] {
				marked++
			}
		}
		if markable == 0 {
			return m, nil, true
		}
		if marked == markable {
			sm.marked = map[string]bool{}
		} else {
			sm.marked = map[string]bool{}
			for _, e := range sm.entries {
				if !e.Active {
					sm.marked[e.ID] = true
				}
			}
		}
		m.overlay.sessions = sm
		return m, nil, true
	case "p":
		// Quick toggle pin on the selected session without opening the menu.
		if sm.selected >= 0 && sm.selected < len(sm.entries) {
			entry := sm.entries[sm.selected]
			if m.cfg.SessionManager != nil {
				_ = m.cfg.SessionManager.SetPinned(entry.ID, !entry.Pinned)
			}
			// Preserve marks across pin refresh.
			marks := sm.marked
			sel := sm.selected
			sm.entries, _ = m.loadSessionList()
			sm.marked = marks
			sm.selected = clampInt(sel, 0, max(0, len(sm.entries)-1))
			m.overlay.sessions = sm
			return m, nil, true
		}
		return m, nil, true
	}
	return m, nil, true
}

// deletableIDs returns marked session ids, or the cursor session when the mark
// set is empty. Active sessions are never included.
func (sm sessionManagerState) deletableIDs() []string {
	var ids []string
	if len(sm.marked) > 0 {
		for _, e := range sm.entries {
			if sm.marked[e.ID] && !e.Active {
				ids = append(ids, e.ID)
			}
		}
		return ids
	}
	if sm.selected >= 0 && sm.selected < len(sm.entries) {
		e := sm.entries[sm.selected]
		if !e.Active {
			return []string{e.ID}
		}
	}
	return nil
}

// openBulkDeleteConfirm asks before removing one or many sessions.
func (m Model) openBulkDeleteConfirm(ids []string) Model {
	if len(ids) == 0 {
		return m
	}
	title := "Delete session?"
	msg := "this permanently removes the session audit log."
	if len(ids) > 1 {
		title = fmt.Sprintf("Delete %d sessions?", len(ids))
		msg = fmt.Sprintf("this permanently removes %d session audit logs.", len(ids))
	}
	// Capture ids for the callback (confirm closes overlay first).
	toDelete := append([]string(nil), ids...)
	return m.openConfirm(title, msg, true, func(mmm Model, ok bool) (Model, tea.Cmd) {
		if !ok || mmm.cfg.SessionManager == nil {
			return mmm.openSessionManager(), nil
		}
		var failed int
		for _, id := range toDelete {
			if err := mmm.cfg.SessionManager.Delete(id); err != nil {
				failed++
			}
		}
		deleted := len(toDelete) - failed
		switch {
		case failed > 0 && deleted == 0:
			mmm = mmm.pushToast("delete failed", toastError)
		case failed > 0:
			mmm = mmm.pushToast(fmt.Sprintf("deleted %d, %d failed", deleted, failed), toastError)
		case deleted == 1:
			mmm = mmm.pushToast("session deleted", toastSuccess)
		default:
			mmm = mmm.pushToast(fmt.Sprintf("deleted %d sessions", deleted), toastSuccess)
		}
		return mmm.openSessionManager(), nil
	})
}

// openSessionActionsMenu builds the per-session action menu (switch / rename /
// pin / delete) for the entry at idx.
func (m Model) openSessionActionsMenu(idx int) Model {
	sm := m.overlay.sessions
	if idx < 0 || idx >= len(sm.entries) {
		return m
	}
	entry := sm.entries[idx]
	hasManager := m.cfg.SessionManager != nil
	busy := m.agent.Busy
	subtitle := entry.Title
	if subtitle == "" {
		subtitle = entry.ID
	}

	items := []menuItem{
		{
			label:    "Switch to this session",
			shortcut: "s",
			disabled: busy || entry.Active,
			hint:     switchHint(busy, entry.Active),
			run: func(mm Model) (Model, tea.Cmd) {
				mm, cmd := mm.switchToSession(entry.ID)
				return mm, cmd
			},
		},
		{
			label:    "Rename session",
			shortcut: "r",
			disabled: !hasManager,
			run: func(mm Model) (Model, tea.Cmd) {
				return mm.openPrompt("Rename session", "enter a new title (empty to clear)", entry.Title, func(mmm Model, text string) (Model, tea.Cmd) {
					if mmm.cfg.SessionManager != nil {
						if err := mmm.cfg.SessionManager.SetTitle(entry.ID, text); err != nil {
							return mmm.pushToast(err.Error(), toastError), nil
						}
					}
					mmm = mmm.pushToast("session renamed", toastSuccess)
					return mmm.openSessionManager(), nil
				}), nil
			},
		},
		{
			label:    pinToggleLabel(entry.Pinned),
			shortcut: "p",
			disabled: !hasManager,
			run: func(mm Model) (Model, tea.Cmd) {
				if mm.cfg.SessionManager != nil {
					_ = mm.cfg.SessionManager.SetPinned(entry.ID, !entry.Pinned)
				}
				mm = mm.pushToast(pinToastLabel(!entry.Pinned), toastInfo)
				return mm.openSessionManager(), nil
			},
		},
		{
			label:    "Delete session",
			shortcut: "d",
			disabled: !hasManager || entry.Active || busy,
			hint:     deleteHint(entry.Active, busy),
			run: func(mm Model) (Model, tea.Cmd) {
				return mm.openBulkDeleteConfirm([]string{entry.ID}), nil
			},
		},
	}
	return m.openMenu("Session · "+truncatePlain(entry.ID, 20), subtitle, items)
}

func pinToggleLabel(pinned bool) string {
	if pinned {
		return "Unpin session"
	}
	return "Pin session"
}

func pinToastLabel(pinned bool) string {
	if pinned {
		return "session pinned"
	}
	return "session unpinned"
}

func switchHint(busy, active bool) string {
	switch {
	case busy:
		return "agent is running"
	case active:
		return "already current"
	}
	return ""
}

func deleteHint(active, busy bool) string {
	switch {
	case active:
		return "cannot delete the active session"
	case busy:
		return "agent is running"
	}
	return "permanent"
}

// --- rendering ---

func (m Model) renderSessionManager() string {
	width := max(1, m.width)
	sm := m.overlay.sessions
	header := " " + m.sessionManagerHeader(sm)
	sep := strings.Repeat("─", clampInt(width-2, 1, 120))
	footer := sessionManagerFooter(sm)
	bodyLines := m.sessionManagerBodyLines(sm, width)

	hFixed := 3
	if m.height > 0 && m.height <= hFixed {
		return renderVisibleLinesWidth([]string{header, sep, footer}, m.height, 0, width)
	}
	bodyH := m.height - hFixed
	if m.height > 0 {
		bodyLines = windowLinesAroundMarker(bodyLines, "▶", bodyH)
		body := renderVisibleLinesWidth(bodyLines, bodyH, 0, width)
		lines := []string{header, sep}
		lines = append(lines, strings.Split(body, "\n")...)
		lines = append(lines, footer)
		return renderVisibleLinesWidth(lines, m.height, 0, width)
	}
	lines := []string{header, sep}
	lines = append(lines, bodyLines...)
	lines = append(lines, "", footer)
	return fitViewToTerminal(strings.Join(lines, "\n"), width)
}

func (m Model) sessionManagerHeader(sm sessionManagerState) string {
	if sm.loadErr != "" {
		return "Sessions · error"
	}
	pinned := 0
	for _, e := range sm.entries {
		if e.Pinned {
			pinned++
		}
	}
	head := fmt.Sprintf("Sessions · %d total · %d pinned", len(sm.entries), pinned)
	if n := len(sm.marked); n > 0 {
		head += fmt.Sprintf(" · %d selected", n)
	}
	return head
}

func sessionManagerFooter(sm sessionManagerState) string {
	if len(sm.marked) > 0 {
		return " ↑/↓ move · space toggle · enter/d delete · a all · esc clear"
	}
	return " ↑/↓ move · space select · enter actions · d delete · a all · p pin · esc close"
}

func (m Model) sessionManagerBodyLines(sm sessionManagerState, width int) []string {
	if sm.loadErr != "" {
		return []string{fmt.Sprintf("  error: %s", sm.loadErr), "", "  esc to close"}
	}
	if len(sm.entries) == 0 {
		return []string{"  (no sessions yet)"}
	}
	var lines []string
	for i, e := range sm.entries {
		lines = append(lines, renderSessionLine(e, i == sm.selected, sm.marked[e.ID], width))
	}
	return lines
}

func renderSessionLine(e SessionInfo, selected, marked bool, width int) string {
	cursor := " "
	if selected {
		cursor = "▶"
	}
	check := " "
	if marked {
		check = "✓"
	} else if !e.Active {
		check = "·"
	}
	pin := " "
	if e.Pinned {
		pin = "★"
	}
	title := e.Title
	if title == "" {
		title = "(no messages)"
	}
	title = truncatePlain(title, max(10, width-36))
	active := ""
	if e.Active {
		active = successStyle.Render("current")
	}
	age := humanizeSessionAge(e.LastActive)
	count := ""
	if e.Messages > 0 {
		count = dimStyle.Render(fmt.Sprintf("%d msgs", e.Messages))
	}
	head := fmt.Sprintf("%s %s %s %s", cursor, check, pin, title)
	tail := strings.TrimSpace(age + " " + count + " " + active)
	gap := width - visibleWidth(head) - visibleWidth(tail) - 2
	if gap < 1 {
		gap = 1
	}
	line := head + strings.Repeat(" ", gap) + " " + tail
	if selected {
		return accentStyle.Render(line)
	}
	return line
}

// humanizeSessionAge renders a short relative age (2h ago / 3d ago). Sessions
// embed a UTC timestamp in the id, so this is approximate display only.
func humanizeSessionAge(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// visibleWidth returns the visible (ANSI-stripped) cell width of s, used to align
// the right-aligned age/count tail in the session list.
func visibleWidth(s string) int {
	// lipgloss.Width counts visible cells ignoring ANSI escapes, which is exactly
	// what alignment after styled segments needs.
	return lipgloss.Width(s)
}
