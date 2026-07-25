package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/fsx"
)

// Prompt stash (Phase 3.1). A lightweight "park this draft" buffer for moments
// when the user wants to type something else first but does not want to lose the
// in-progress prompt. Stashes persist to disk (Config.StashPath) so they survive
// a restart, and are surfaced through a small menu overlay (Ctrl+S when the
// composer is empty) so popping one is keyboard-only.
//
// It is deliberately separate from prompt history: history is what you typed and
// submitted; the stash is what you have NOT submitted yet but want to keep.

const maxStashEntries = 20

// stashDraft parks the current composer draft (if non-empty) onto the stash,
// clears the composer, and persists. Returns the model unchanged when the draft
// is empty or whitespace.
func (m Model) stashDraft() Model {
	draft := strings.TrimSpace(m.inputValue())
	if draft == "" {
		return m.pushToast("nothing to stash — composer is empty", toastWarn)
	}
	before := m.stash
	m.stash = append([]string{draft}, m.stash...)
	if len(m.stash) > maxStashEntries {
		m.stash = m.stash[:maxStashEntries]
	}
	if !sameStash(before, m.stash) {
		_ = saveStash(m.cfg.StashPath, m.stash)
	}
	m = m.clearInput()
	return m.pushToast("stashed draft · Ctrl+S to pop", toastSuccess)
}

// popStashIntoComposer moves the stash entry at idx into the composer and
// removes it from the stash. Out-of-range idx is a no-op.
func (m Model) popStashIntoComposer(idx int) Model {
	if idx < 0 || idx >= len(m.stash) {
		return m
	}
	draft := m.stash[idx]
	m.stash = append(m.stash[:idx], m.stash[idx+1:]...)
	_ = saveStash(m.cfg.StashPath, m.stash)
	m = m.SetInput(draft)
	m.navTurnIdx = -1
	return m.pushToast("popped stashed draft", toastInfo)
}

// openStashMenu builds a menu of stashed drafts (most recent first); selecting
// one pops it into the composer. Empty stash surfaces an alert instead.
func (m Model) openStashMenu() Model {
	if len(m.stash) == 0 {
		return m.openAlert("Stash", "no stashed drafts", toastInfo)
	}
	items := make([]menuItem, 0, len(m.stash))
	for i, draft := range m.stash {
		idx := i
		label := strings.ReplaceAll(truncatePlain(draft, 56), "\n", " ")
		if label == "" {
			label = "(empty)"
		}
		items = append(items, menuItem{
			label: label,
			run: func(mm Model) (Model, tea.Cmd) {
				return mm.popStashIntoComposer(idx), nil
			},
		})
	}
	return m.openMenu("Stashed drafts", "enter to pop into composer", items)
}

// stashLeaderAction backs the canonical Ctrl+S route: stash when there is a
// draft (even whitespace, which then warns), pop (via the menu) only when the
// composer is truly empty. The legacy name is retained for compatibility.
func (m Model) stashLeaderAction() Model {
	if m.inputValue() != "" {
		return m.stashDraft()
	}
	return m.openStashMenu()
}

// --- persistence ---

func loadStash(path string) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []string
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}

func saveStash(path string, entries []string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(path, append(data, '\n'), 0o600)
}

func sameStash(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
