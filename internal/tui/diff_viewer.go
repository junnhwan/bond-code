package tui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/tool"
)

// The diff viewer is a full-screen change-review modal (<leader>d) — the TUI
// feature that makes a coding agent's work auditable at a glance. It aggregates
// every write_file/edit_file the agent made in the current session into a
// per-file list with add/remove counts, and expands each file to its diff. A
// second source cross-checks the net working-tree change via `git diff HEAD`.
//
// It reuses the overlay dispatch (so it is part of the unified modal system and
// never fights the composer for keys) but renders full-screen like the ctrl+h
// history browser rather than as a centered box.

type diffSource int

const (
	diffSourceSession diffSource = iota
	diffSourceGit
)

func (s diffSource) Label() string {
	if s == diffSourceGit {
		return "git working tree"
	}
	return "session tool calls"
}

// diffViewerState backs the change-review modal.
type diffViewerState struct {
	source   diffSource
	files    []diffFileEntry
	selected int
	expanded map[int]bool
	// splitView renders session edit diffs side-by-side (Phase 5D.1). Git source
	// is always unified — hunk alignment needs a real parser, out of scope here.
	splitView bool
	loadErr   string
	loaded    bool
}

// diffFileEntry is one file's aggregated change. Session source fills ops +
// per-op old/new text; git source fills gitDiff (the raw unified-diff body) and
// uses additions/deletions parsed from --numstat.
type diffFileEntry struct {
	path      string
	ops       []diffFileOp
	gitDiff   string
	additions int
	deletions int
}

type diffFileOp struct {
	kind    string // "write" / "edit"
	oldText string
	newText string
}

// openDiffViewer loads the session-source file list and enters the modal.
func (m Model) openDiffViewer() Model {
	dv := diffViewerState{
		source:   diffSourceSession,
		expanded: map[int]bool{},
	}
	dv.files, dv.loadErr = loadSessionDiffEntries(m)
	dv.loaded = true
	m.overlay = overlayState{kind: overlayDiff, diff: dv}
	return m
}

// switchDiffSource reloads file entries from the other source.
func (m Model) switchDiffSource(target diffSource) Model {
	dv := m.overlay.diff
	dv.source = target
	dv.expanded = map[int]bool{}
	dv.selected = 0
	dv.loadErr = ""
	switch target {
	case diffSourceSession:
		dv.files, dv.loadErr = loadSessionDiffEntries(m)
	case diffSourceGit:
		dv.files, dv.loadErr = loadGitDiffEntries(m.cfg.Status.ProjectRoot)
	}
	m.overlay.diff = dv
	return m
}

func loadSessionDiffEntries(m Model) ([]diffFileEntry, string) {
	controller := m.cfg.SessionHistory
	sessionID := strings.TrimSpace(m.cfg.Status.SessionID)
	if controller == nil || sessionID == "" {
		return nil, "no session history configured"
	}
	events, err := controller.LoadEvents(sessionID)
	if err != nil {
		return nil, err.Error()
	}
	return buildDiffEntriesFromEvents(events), ""
}

// buildDiffEntriesFromEvents aggregates write_file/edit_file tool calls from the
// audit into per-file entries, preserving first-touch order so the list reads
// chronologically.
func buildDiffEntriesFromEvents(events []session.Event) []diffFileEntry {
	byPath := map[string]*diffFileEntry{}
	var order []string
	for _, ev := range events {
		tc := ev.ToolCall
		if tc == nil {
			continue
		}
		if tc.Name != tool.WriteFile && tc.Name != tool.EditFile {
			continue
		}
		// Skip failed calls: they changed nothing on disk and would inflate the
		// list with no-op entries.
		if strings.TrimSpace(tc.Error) != "" {
			continue
		}
		path, oldS, newS, ok := parseFileToolInput(tc.Name, tc.Input)
		if !ok || strings.TrimSpace(path) == "" {
			continue
		}
		entry, exists := byPath[path]
		if !exists {
			order = append(order, path)
			entry = &diffFileEntry{path: path}
			byPath[path] = entry
		}
		add, del := countDiffLines(oldS, newS)
		entry.additions += add
		entry.deletions += del
		entry.ops = append(entry.ops, diffFileOp{kind: fileOpKind(tc.Name), oldText: oldS, newText: newS})
	}
	out := make([]diffFileEntry, 0, len(order))
	for _, p := range order {
		out = append(out, *byPath[p])
	}
	return out
}

func fileOpKind(name string) string {
	switch name {
	case tool.WriteFile:
		return "write"
	case tool.EditFile:
		return "edit"
	}
	return name
}

// parseFileToolInput pulls the path + before/after text from a tool call's raw
// JSON input. write_file only carries the new content; edit_file carries both.
func parseFileToolInput(name, input string) (path, oldS, newS string, ok bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", "", false
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return "", "", "", false
	}
	p, _ := raw["path"].(string)
	if strings.TrimSpace(p) == "" {
		return "", "", "", false
	}
	switch name {
	case tool.WriteFile:
		c, _ := raw["content"].(string)
		return p, "", c, true
	case tool.EditFile:
		o, _ := raw["old_string"].(string)
		n, _ := raw["new_string"].(string)
		return p, o, n, true
	}
	return "", "", "", false
}

// countDiffLines reports (+added, -removed) for an old->new text change. Empty
// old text means a pure addition; very large pairs fall back to raw line counts
// to avoid the O(n*m) LCS table.
func countDiffLines(oldS, newS string) (int, int) {
	oldLines := splitTrimmed(oldS)
	newLines := splitTrimmed(newS)
	if len(oldLines) == 0 {
		return len(newLines), 0
	}
	if len(oldLines)*len(newLines) > 20000 {
		return len(newLines), len(oldLines)
	}
	add, del := 0, 0
	for _, op := range lineDiff(oldLines, newLines) {
		switch op.kind {
		case diffAdd:
			add++
		case diffDel:
			del++
		}
	}
	return add, del
}

// loadGitDiffEntries builds the file list from `git diff HEAD --numstat` and
// attaches each file's unified diff body (from one `git diff HEAD` call) so the
// expanded view needs no extra exec.
func loadGitDiffEntries(root string) ([]diffFileEntry, string) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	numstat, err := exec.Command("git", "-C", root, "diff", "HEAD", "--numstat").Output()
	if err != nil {
		out := strings.TrimSpace(err.Error())
		if execErr, ok := err.(*exec.ExitError); ok && len(execErr.Stderr) > 0 {
			out = strings.TrimSpace(string(execErr.Stderr))
		}
		return nil, "git diff: " + out
	}
	full, _ := exec.Command("git", "-C", root, "diff", "HEAD", "--unified=3").Output()
	bodies := splitGitDiffByFile(string(full))

	var entries []diffFileEntry
	for _, line := range strings.Split(strings.TrimSpace(string(numstat)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		add, e1 := strconv.Atoi(parts[0])
		del, e2 := strconv.Atoi(parts[1])
		if e1 != nil || e2 != nil {
			// Binary / untracked files report "-" instead of counts.
			add, del = 0, 0
		}
		path := parts[2]
		entries = append(entries, diffFileEntry{
			path:      path,
			gitDiff:   bodies[path],
			additions: add,
			deletions: del,
		})
	}
	if len(entries) == 0 {
		return nil, "no uncommitted changes vs HEAD"
	}
	return entries, ""
}

// splitGitDiffByFile carves a full `git diff` output into per-file bodies keyed
// by path. Each body excludes the leading "diff --git" header (used as the
// delimiter) so rendering starts at the hunk metadata.
func splitGitDiffByFile(raw string) map[string]string {
	byFile := map[string]string{}
	var current string
	var body strings.Builder
	flush := func() {
		if current != "" {
			byFile[current] = strings.TrimRight(body.String(), "\n")
		}
		current = ""
		body.Reset()
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			current = gitDiffHeaderPath(line)
		} else if current != "" {
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	flush()
	return byFile
}

func gitDiffHeaderPath(header string) string {
	s := strings.TrimPrefix(header, "diff --git ")
	parts := strings.SplitN(s, " ", 2)
	if len(parts) < 2 {
		return ""
	}
	b := strings.TrimSpace(parts[1])
	return strings.TrimPrefix(b, "b/")
}

// --- key handling ---

func (m Model) handleDiffViewerKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	dv := m.overlay.diff
	if !dv.loaded {
		// While loading errored, only allow source switch / close.
		switch msg.String() {
		case "esc", "q", "ctrl+c", "ctrl+[":
			return m.closeOverlay(), nil, true
		case "d":
			target := diffSourceSession
			if dv.source == diffSourceSession {
				target = diffSourceGit
			}
			return m.switchDiffSource(target), nil, true
		}
		return m, nil, true
	}
	switch msg.String() {
	case "esc", "q", "ctrl+c", "ctrl+[":
		return m.closeOverlay(), nil, true
	case "up", "k":
		dv.selected = clampInt(dv.selected-1, 0, max(0, len(dv.files)-1))
		m.overlay.diff = dv
		return m, nil, true
	case "down", "j":
		dv.selected = clampInt(dv.selected+1, 0, max(0, len(dv.files)-1))
		m.overlay.diff = dv
		return m, nil, true
	case "home", "g":
		dv.selected = 0
		m.overlay.diff = dv
		return m, nil, true
	case "end", "G":
		dv.selected = max(0, len(dv.files)-1)
		m.overlay.diff = dv
		return m, nil, true
	case "pgup":
		dv.selected = clampInt(dv.selected-8, 0, max(0, len(dv.files)-1))
		m.overlay.diff = dv
		return m, nil, true
	case "pgdn":
		dv.selected = clampInt(dv.selected+8, 0, max(0, len(dv.files)-1))
		m.overlay.diff = dv
		return m, nil, true
	case "enter", " ", "right", "l":
		if dv.selected >= 0 && dv.selected < len(dv.files) {
			dv.expanded[dv.selected] = !dv.expanded[dv.selected]
			m.overlay.diff = dv
		}
		return m, nil, true
	case "left", "h":
		if dv.selected >= 0 && dv.selected < len(dv.files) {
			dv.expanded[dv.selected] = false
			m.overlay.diff = dv
		}
		return m, nil, true
	case "d":
		target := diffSourceSession
		if dv.source == diffSourceSession {
			target = diffSourceGit
		}
		return m.switchDiffSource(target), nil, true
	case "v":
		// Phase 5D.1: toggle split / unified for session edit diffs. Git source
		// is always unified, so the toggle is a no-op there.
		if dv.source == diffSourceSession {
			dv.splitView = !dv.splitView
			m.overlay.diff = dv
		}
		return m, nil, true
	}
	return m, nil, true
}

// --- rendering ---

func (m Model) renderDiffViewer() string {
	width := max(1, m.width)
	dv := m.overlay.diff
	header := " " + m.diffViewerHeader(dv)
	sep := strings.Repeat("─", clampInt(width-2, 1, 120))
	footer := " ↑/↓ move · enter expand/collapse · d source · v split · esc close"
	bodyLines := m.diffViewerBodyLines(dv, width)

	hFixed := 3 // header, separator, footer
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

func (m Model) diffViewerHeader(dv diffViewerState) string {
	if dv.loadErr != "" {
		return fmt.Sprintf("Changes · %s · error", dv.source.Label())
	}
	totalAdd, totalDel := 0, 0
	for _, f := range dv.files {
		totalAdd += f.additions
		totalDel += f.deletions
	}
	mode := "unified"
	if dv.splitView && dv.source == diffSourceSession {
		mode = "split"
	}
	return fmt.Sprintf("Changes · %s · %s · %d file%s  +%d -%d",
		dv.source.Label(), mode,
		len(dv.files), plural(len(dv.files)), totalAdd, totalDel)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (m Model) diffViewerBodyLines(dv diffViewerState, width int) []string {
	if dv.loadErr != "" {
		return []string{fmt.Sprintf("  error: %s", dv.loadErr),
			"",
			"  press d to switch source, esc to close"}
	}
	if len(dv.files) == 0 {
		return []string{"  (no file changes in this session)"}
	}
	var lines []string
	for i, f := range dv.files {
		isSel := i == dv.selected
		isExp := dv.expanded[i]
		lines = append(lines, m.renderDiffFileHeader(f, isSel, isExp, width))
		if isExp {
			for _, dl := range m.renderDiffFileBody(f, dv.source, width) {
				lines = append(lines, "  "+dl)
			}
		}
	}
	return lines
}

func (m Model) renderDiffFileHeader(f diffFileEntry, selected, expanded bool, width int) string {
	glyph := "▸"
	if expanded {
		glyph = "▾"
	}
	marker := " "
	prefix := "  "
	if selected {
		marker = "▶"
		prefix = ""
	}
	path := truncatePlain(f.path, max(10, width-22))
	addStats := diffAddStyle.Render(fmt.Sprintf("+%d", f.additions))
	delStats := diffRemoveStyle.Render(fmt.Sprintf("-%d", f.deletions))
	// Right-align the +/- stats: fill the gap between path and stats with spaces.
	head := fmt.Sprintf("%s%s %s %s", prefix, marker, glyph, path)
	tail := addStats + " " + delStats
	gap := width - lipgloss.Width(head) - lipgloss.Width(tail) - 2
	if gap < 1 {
		gap = 1
	}
	line := head + strings.Repeat(" ", gap) + " " + tail
	if selected {
		return accentStyle.Render(line)
	}
	return line
}

func (m Model) renderDiffFileBody(f diffFileEntry, source diffSource, width int) []string {
	contentW := max(10, width-6)
	if source == diffSourceGit {
		return renderGitDiffBody(f.gitDiff, contentW)
	}
	var out []string
	for _, op := range f.ops {
		label := dimStyle.Render("[" + op.kind + "]")
		out = append(out, label)
		var diff string
		if m.overlay.diff.splitView {
			diff = renderEditDiffSplit(op.oldText, op.newText, contentW)
		} else {
			diff = renderEditDiff(op.oldText, op.newText, contentW)
		}
		if diff == "" {
			// write_file with no content preview → show a count line.
			out = append(out, dimStyle.Render("(no content)"))
			continue
		}
		out = append(out, strings.Split(diff, "\n")...)
	}
	return out
}

// renderGitDiffBody colors a raw unified-diff body: + lines green, - lines red,
// @@ hunk headers dim. Everything else passes through as context.
func renderGitDiffBody(raw string, width int) []string {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return []string{dimStyle.Render("(empty diff)")}
	}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			out = append(out, dimStyle.Render(truncatePlain(line, width)))
		case strings.HasPrefix(line, "@@"):
			out = append(out, dimStyle.Render(truncatePlain(line, width)))
		case strings.HasPrefix(line, "+"):
			out = append(out, renderDiffLine(line, width))
		case strings.HasPrefix(line, "-"):
			out = append(out, renderDiffLine(line, width))
		default:
			out = append(out, truncatePlain(line, width))
		}
	}
	return out
}
