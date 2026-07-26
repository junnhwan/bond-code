package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// scrollEntry is one navigable unit in the transcript for Simple-mode
// selection (user prompt or a committed block).
type scrollEntry struct {
	key      string
	turnIdx  int
	blockIdx int // -1 = user echo
	kind     string
	foldable bool
}

// scrollEntries walks the committed timeline into a flat selection list.
func (m Model) scrollEntries() []scrollEntry {
	entries := make([]scrollEntry, 0, 16)
	for ti, turn := range m.timeline.Turns {
		if strings.TrimSpace(turn.User.Body) != "" {
			entries = append(entries, scrollEntry{
				key:      fmt.Sprintf("t%d/u", ti),
				turnIdx:  ti,
				blockIdx: -1,
				kind:     "user",
				foldable: false,
			})
		}
		for bi, block := range turn.Blocks {
			// Hidden completed tools when details are off are not selectable.
			if block.Kind == BlockTool && block.Tool != nil && !m.showToolDetails && block.Tool.Status == ToolDone {
				continue
			}
			// Hidden thinking (CC default) is not selectable until revealed.
			if block.Kind == BlockReasoning {
				if strings.TrimSpace(block.Body) == "" || !m.reasoningVisible(scrollEntryKey(ti, bi, block)) {
					continue
				}
			}
			foldable := block.Kind == BlockTool || block.Kind == BlockReasoning || block.Kind == BlockAssistant
			entries = append(entries, scrollEntry{
				key:      scrollEntryKey(ti, bi, block),
				turnIdx:  ti,
				blockIdx: bi,
				kind:     string(block.Kind),
				foldable: foldable,
			})
		}
	}
	return entries
}

func scrollEntryKey(turnIdx, blockIdx int, block Block) string {
	if block.ID != "" {
		return block.ID
	}
	return fmt.Sprintf("t%d/b%d", turnIdx, blockIdx)
}

func (m Model) clampScrollSelection() Model {
	entries := m.scrollEntries()
	if len(entries) == 0 {
		m.scrollSel = -1
		return m
	}
	if m.scrollSel < 0 {
		return m
	}
	if m.scrollSel >= len(entries) {
		m.scrollSel = len(entries) - 1
	}
	return m
}

// moveScrollSelection shifts the scrollback selection by delta (+/-1) and
// ensures an initial selection when entering navigation from -1.
func (m Model) moveScrollSelection(delta int) Model {
	entries := m.scrollEntries()
	if len(entries) == 0 {
		m.scrollSel = -1
		return m
	}
	if m.scrollSel < 0 {
		if delta < 0 {
			m.scrollSel = len(entries) - 1
		} else {
			m.scrollSel = 0
		}
		return m
	}
	m.scrollSel += delta
	if m.scrollSel < 0 {
		m.scrollSel = 0
	}
	if m.scrollSel >= len(entries) {
		m.scrollSel = len(entries) - 1
	}
	return m
}

// selectedScrollEntry returns the current selection, or false when none.
func (m Model) selectedScrollEntry() (scrollEntry, bool) {
	entries := m.scrollEntries()
	if m.scrollSel < 0 || m.scrollSel >= len(entries) {
		return scrollEntry{}, false
	}
	return entries[m.scrollSel], true
}

// isEntryFolded reports whether a timeline entry key is manually force-folded.
func (m Model) isEntryFolded(key string) bool {
	if m.foldedEntries == nil {
		return false
	}
	return m.foldedEntries[key]
}

// isEntryExpanded reports whether a timeline entry key is manually force-expanded.
func (m Model) isEntryExpanded(key string) bool {
	if m.expandedEntries == nil {
		return false
	}
	return m.expandedEntries[key]
}

// reasoningVisible reports whether a committed thinking block should paint.
// Default is fully hidden (Claude Code prompt mode). Only an explicit
// showThinking toggle (Ctrl+T) or a per-entry expand reveals it.
// Ctrl+O verbose expands tools/timestamps only — it must NOT re-open
// historical thinking, or a one-shot tool expand leaves thinking stuck on.
func (m Model) reasoningVisible(blockID string) bool {
	if blockID != "" {
		if m.isEntryExpanded(blockID) {
			return true
		}
		if m.isEntryFolded(blockID) {
			return false
		}
	}
	return m.showThinking
}

// reasoningExpanded is true when thinking content should show in full.
// With CC-style default hide, this matches reasoningVisible (no folded header).
func (m Model) reasoningExpanded(blockID string) bool {
	return m.reasoningVisible(blockID)
}

// toggleSelectedFold folds/expands the selected entry when it supports fold.
// Tools toggle ToolBlock.Collapsed; reasoning/assistant use expand/fold maps.
func (m Model) toggleSelectedFold() Model {
	entry, ok := m.selectedScrollEntry()
	if !ok || !entry.foldable {
		return m
	}
	if entry.blockIdx < 0 || entry.turnIdx < 0 || entry.turnIdx >= len(m.timeline.Turns) {
		return m
	}
	turn := m.timeline.Turns[entry.turnIdx]
	if entry.blockIdx >= len(turn.Blocks) {
		return m
	}
	block := turn.Blocks[entry.blockIdx]
	if block.Tool != nil {
		m.timeline = m.timeline.setToolCollapsed(entry.turnIdx, entry.blockIdx, !block.Tool.Collapsed)
		return m.clampScroll(m.currentLayout())
	}
	if m.foldedEntries == nil {
		m.foldedEntries = map[string]bool{}
	}
	if m.expandedEntries == nil {
		m.expandedEntries = map[string]bool{}
	}
	// Reasoning: default is fully hidden (CC). When a block is selectable
	// (global show on), left/right still force-fold / force-expand one entry.
	if block.Kind == BlockReasoning {
		if m.reasoningVisible(entry.key) && !m.isEntryFolded(entry.key) {
			// Currently showing → force-fold this entry only.
			delete(m.expandedEntries, entry.key)
			m.foldedEntries[entry.key] = true
		} else {
			delete(m.foldedEntries, entry.key)
			m.expandedEntries[entry.key] = true
		}
	} else {
		// Assistant (and other foldables): default expanded; toggle force-fold.
		if m.isEntryFolded(entry.key) {
			delete(m.foldedEntries, entry.key)
		} else {
			m.foldedEntries[entry.key] = true
		}
	}
	// Fold is view-only for non-tool blocks; invalidate line cache so re-render
	// picks up the new fold without thrashing Timeline.Version on every delta.
	if m.timelineLinesCache != nil {
		m.timelineLinesCache.initialized = false
		m.timelineLinesCache.blockLines = nil
	}
	return m.clampScroll(m.currentLayout())
}

// foldedEntriesStamp is a stable cache-key fingerprint of manual fold/expand.
func (m Model) foldedEntriesStamp() string {
	parts := make([]string, 0, len(m.foldedEntries)+len(m.expandedEntries))
	for k, on := range m.foldedEntries {
		if on {
			parts = append(parts, "f:"+k)
		}
	}
	for k, on := range m.expandedEntries {
		if on {
			parts = append(parts, "e:"+k)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	for i := 1; i < len(parts); i++ {
		j := i
		for j > 0 && parts[j] < parts[j-1] {
			parts[j], parts[j-1] = parts[j-1], parts[j]
			j--
		}
	}
	return strings.Join(parts, ",")
}

// highlightScrollSelection paints a Hover background on the first line of the
// selected entry's rendered lines when scrollback owns focus.
func highlightScrollSelection(lines []string, selected bool) []string {
	if !selected || len(lines) == 0 {
		return lines
	}
	out := append([]string(nil), lines...)
	for i, line := range out {
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			continue
		}
		out[i] = lipgloss.NewStyle().
			Background(DefaultTheme.Hover).
			Render(line)
		break
	}
	return out
}
