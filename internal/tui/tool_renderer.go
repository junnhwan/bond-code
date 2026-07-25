package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/junnhwan/bond-code/internal/todo"
	"github.com/junnhwan/bond-code/internal/tool"
)

// ToolRenderer renders one tool's ToolBlock as a semantic, human-readable view,
// mirroring Claude Code's per-tool UI contract (userFacingName /
// getToolUseSummary / renderToolResultMessage) but kept in the TUI layer rather
// than hung off internal/tool — Go's Registry model makes a render contract on
// the tool interface a poor fit, and keeping it local avoids cross-layer churn.
//
// Implementations are looked up by tool name via toolRenderers; unregistered
// tools (MCP tools, task, memory_*, ...) fall back to
// defaultRenderer, whose empty returns make renderToolActivity behave exactly
// as it did before this table existed — the zero-regression safety net.
type ToolRenderer interface {
	// Verb is the human-facing action word ("Update" / "Read" / "Search" /
	// "Run"). Empty => caller falls back to the raw tool name.
	Verb(tool *ToolBlock) string
	// Subject is the primary argument (path / command / pattern), already
	// decorated with quotes / shortening. Empty => caller falls back to the
	// legacy subject derivation.
	Subject(tool *ToolBlock, verbose bool) string
	// Result is the one-line human summary for a finished state ("Found 12
	// matches" / "go test: ok 1"). Empty => caller falls back to the legacy
	// status line.
	Result(tool *ToolBlock, verbose bool) string
	// Detail is the expanded body (a real diff, an output preview) shown on
	// ctrl+e or in verbose mode. Empty => caller falls back to the legacy
	// details renderer, or omits the block entirely when there is no output.
	Detail(tool *ToolBlock, width int, verbose bool) string
}

var toolRenderers = map[string]ToolRenderer{
	tool.ReadFile:     pathToolRenderer{verb: "Read"},
	tool.ListDir:      pathToolRenderer{verb: "List"},
	tool.WriteFile:    writeFileRenderer{},
	tool.EditFile:     editRenderer{},
	tool.SearchText:   searchRenderer{},
	tool.RunCommand:   runCommandRenderer{},
	tool.TodoWrite:    todoWriteRenderer{},
	tool.TodoRead:     todoReadRenderer{},
	tool.Skill:        skillRenderer{},
	tool.MemorySave:   memorySaveRenderer{},
	tool.MemorySearch: memorySearchRenderer{},
}

// RendererFor returns the renderer for a tool name, or defaultRenderer.
func RendererFor(name string) ToolRenderer {
	if r, ok := toolRenderers[name]; ok {
		return r
	}
	return defaultRenderer{}
}

// toolIsRegistered reports whether a semantic renderer exists for name.
func toolIsRegistered(name string) bool {
	_, ok := toolRenderers[name]
	return ok
}

// renderVerbose is the global verbose flag toggled by ctrl+o. When set, tools
// render full paths and fuller details instead of shortened summaries. The TUI
// runs a single instance per process, so package-level state is acceptable.
var renderVerbose bool

func setRenderVerbose(v bool) { renderVerbose = v }

// defaultRenderer returns empty strings for every method, so renderToolActivity
// falls through to its existing logic — zero behavior change for unregistered
// tools (MCP tools, task, spawn, ask_user).
type defaultRenderer struct{}

func (defaultRenderer) Verb(*ToolBlock) string              { return "" }
func (defaultRenderer) Subject(*ToolBlock, bool) string     { return "" }
func (defaultRenderer) Result(*ToolBlock, bool) string      { return "" }
func (defaultRenderer) Detail(*ToolBlock, int, bool) string { return "" }

// paramValue reads one field from a tool's JSON input, trimmed.
func paramValue(input, key string) string {
	return strings.TrimSpace(parseToolInput(input)[key])
}

// splitTrimmed splits a string into lines without a trailing empty element.
func splitTrimmed(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// pathToolRenderer covers read_file / list_dir: path-keyed reads whose result
// is "how much did I read".
type pathToolRenderer struct{ verb string }

func (r pathToolRenderer) Verb(*ToolBlock) string { return r.verb }
func (r pathToolRenderer) Subject(tool *ToolBlock, verbose bool) string {
	return displayPath(paramValue(tool.Input, "path"), verbose)
}
func (r pathToolRenderer) Result(tool *ToolBlock, _ bool) string {
	switch tool.Status {
	case ToolFailed:
		return "failed"
	case ToolDone:
		if tool.Output != "" {
			return fmt.Sprintf("%d lines", lineCount(tool.Output))
		}
		return "done"
	}
	return ""
}
func (pathToolRenderer) Detail(*ToolBlock, int, bool) string { return "" }

// writeFileRenderer covers write_file: a full-content write.
type writeFileRenderer struct{}

func (writeFileRenderer) Verb(*ToolBlock) string { return "Write" }
func (writeFileRenderer) Subject(tool *ToolBlock, verbose bool) string {
	return displayPath(paramValue(tool.Input, "path"), verbose)
}
func (writeFileRenderer) Result(tool *ToolBlock, _ bool) string {
	switch tool.Status {
	case ToolFailed:
		return "failed"
	case ToolDone:
		if content := paramValue(tool.Input, "content"); content != "" {
			return fmt.Sprintf("%d lines", lineCount(content))
		}
		return "wrote"
	}
	return ""
}
func (writeFileRenderer) Detail(tool *ToolBlock, width int, _ bool) string {
	// Only preview content during the confirmation step (before the write
	// happens); afterwards the write is settled and re-printing the full
	// content would be noise in the timeline.
	if tool.Status != ToolPending {
		return ""
	}
	content := paramValue(tool.Input, "content")
	lines := splitTrimmed(content)
	if len(lines) == 0 {
		return ""
	}
	const maxPreview = 6
	contentWidth := max(1, width-6)
	out := make([]string, 0, maxPreview+1)
	for i, l := range lines {
		if i >= maxPreview {
			out = append(out, dimStyle.Render(fmt.Sprintf("... +%d more lines", len(lines)-maxPreview)))
			break
		}
		out = append(out, renderDiffLine("+"+l, contentWidth))
	}
	return strings.Join(out, "\n")
}

// editRenderer covers edit_file. A missing old_string means creation
// (Create/created); otherwise it is an in-place edit (Update/updated) whose
// Detail is a real unified diff of old_string -> new_string.
type editRenderer struct{}

func (editRenderer) Verb(tool *ToolBlock) string {
	if paramValue(tool.Input, "old_string") == "" {
		return "Create"
	}
	return "Update"
}
func (editRenderer) Subject(tool *ToolBlock, verbose bool) string {
	return displayPath(paramValue(tool.Input, "path"), verbose)
}
func (editRenderer) Result(tool *ToolBlock, _ bool) string {
	switch tool.Status {
	case ToolFailed:
		return "failed"
	case ToolDone:
		if paramValue(tool.Input, "old_string") == "" {
			return "created"
		}
		return "updated"
	}
	return ""
}
func (editRenderer) Detail(tool *ToolBlock, width int, _ bool) string {
	oldS := paramValue(tool.Input, "old_string")
	newS := paramValue(tool.Input, "new_string")
	if oldS == "" && newS == "" {
		return ""
	}
	return renderEditDiff(oldS, newS, width)
}

// searchRenderer covers search_text / search_code.
type searchRenderer struct{}

func (searchRenderer) Verb(*ToolBlock) string { return "Search" }
func (searchRenderer) Subject(tool *ToolBlock, _ bool) string {
	pattern := firstNonEmpty(paramValue(tool.Input, "query"), paramValue(tool.Input, "pattern"))
	if pattern == "" {
		return ""
	}
	return fmt.Sprintf("%q", pattern)
}
func (searchRenderer) Result(tool *ToolBlock, _ bool) string {
	switch tool.Status {
	case ToolFailed:
		return "failed"
	case ToolDone:
		if matches, files := searchMatchCounts(tool.Output + "\n" + tool.Summary); matches > 0 {
			if files > 0 {
				return fmt.Sprintf("Found %d matches in %d files", matches, files)
			}
			return fmt.Sprintf("Found %d matches", matches)
		}
		if strings.TrimSpace(tool.Summary) != "" {
			return tool.Summary
		}
		return "no matches"
	}
	return ""
}
func (searchRenderer) Detail(*ToolBlock, int, bool) string { return "" }

var (
	searchMatchRE = regexp.MustCompile(`(?i)(\d+)\s+(?:matches?|results?|hits?)`)
	searchFileRE  = regexp.MustCompile(`(?i)(\d+)\s+files?`)
)

// searchMatchCounts heuristically extracts match/file counts from a search
// tool's output or summary, tolerating "12 matches in 3 files" / "3 results"
// and similar phrasings. Zero matches means "could not tell".
func searchMatchCounts(s string) (matches, files int) {
	if m := searchMatchRE.FindStringSubmatch(s); m != nil {
		matches, _ = strconv.Atoi(m[1])
	}
	if m := searchFileRE.FindStringSubmatch(s); m != nil {
		files, _ = strconv.Atoi(m[1])
	}
	return
}

// runCommandRenderer covers run_command / execute_command.
type runCommandRenderer struct{}

func (runCommandRenderer) Verb(*ToolBlock) string { return "Run" }
func (runCommandRenderer) Subject(tool *ToolBlock, _ bool) string {
	cmd := paramValue(tool.Input, "command")
	if cmd == "" {
		return ""
	}
	return fmt.Sprintf("%q", truncatePlain(cmd, 60))
}
func (runCommandRenderer) Result(tool *ToolBlock, _ bool) string {
	switch tool.Status {
	case ToolFailed:
		if strings.TrimSpace(tool.Error) != "" {
			return "failed: " + truncatePlain(firstLine(tool.Error), 50)
		}
		return "failed"
	case ToolDone:
		// Prefer the structured summary the execution side fills (e.g. "go test:
		// ok 1"); fall back to the first output line; empty => legacy "done".
		summary := strings.TrimSpace(tool.Summary)
		if summary != "" && summary != paramValue(tool.Input, "command") {
			return truncatePlain(summary, 60)
		}
		if strings.TrimSpace(tool.Output) != "" {
			return truncatePlain(firstLine(tool.Output), 60)
		}
		return ""
	}
	return ""
}
func (runCommandRenderer) Detail(*ToolBlock, int, bool) string { return "" }

// --- runtime tools: todo / skill / memory ---------------------------------

// todoWriteRenderer renders Claude Code-style TodoWrite checklist updates.
type todoWriteRenderer struct{}

func (todoWriteRenderer) Verb(*ToolBlock) string { return "Todo" }

func (todoWriteRenderer) Subject(tool *ToolBlock, _ bool) string {
	items := parseTodoItems(tool.Input)
	if len(items) == 0 {
		return "cleared"
	}
	if allTodoCompleted(items) {
		return fmt.Sprintf("%d done · cleared", len(items))
	}
	return todoProgressSubject(items)
}

func (todoWriteRenderer) Result(tool *ToolBlock, _ bool) string {
	switch tool.Status {
	case ToolFailed:
		return "failed"
	case ToolDone:
		items := parseTodoItems(tool.Input)
		if len(items) == 0 || allTodoCompleted(items) {
			return "cleared"
		}
		return todoProgressSubject(items)
	}
	return ""
}

func (todoWriteRenderer) Detail(tool *ToolBlock, width int, _ bool) string {
	items := parseTodoItems(tool.Input)
	if len(items) == 0 {
		return dimStyle.Render("(empty list)")
	}
	contentWidth := max(8, width-4)
	lines := make([]string, 0, len(items))
	for _, item := range items {
		icon := todo.StatusIcon(todo.Status(item.Status))
		label := strings.TrimSpace(item.Subject)
		if label == "" {
			label = item.ID
		}
		if item.ActiveForm != "" && item.Status == string(todo.StatusInProgress) {
			label = item.ActiveForm
		}
		style := dimStyle
		switch item.Status {
		case string(todo.StatusInProgress):
			style = accentStyle
		case string(todo.StatusCompleted):
			style = successStyle
		}
		lines = append(lines, style.Render(icon+" "+truncatePlain(label, contentWidth)))
	}
	return strings.Join(lines, "\n")
}

// todoReadRenderer covers todo_read summary/json peeks.
type todoReadRenderer struct{}

func (todoReadRenderer) Verb(*ToolBlock) string { return "Todo" }

func (todoReadRenderer) Subject(tool *ToolBlock, _ bool) string {
	format := paramValue(tool.Input, "format")
	if format == "" || format == "summary" {
		return "read summary"
	}
	return "read " + format
}

func (todoReadRenderer) Result(tool *ToolBlock, _ bool) string {
	switch tool.Status {
	case ToolFailed:
		return "failed"
	case ToolDone:
		if strings.Contains(strings.ToLower(tool.Output), "no tasks") {
			return "empty"
		}
		if n := strings.Count(tool.Output, "\n"); n > 0 {
			return fmt.Sprintf("%d lines", n)
		}
		return "read"
	}
	return ""
}

func (todoReadRenderer) Detail(*ToolBlock, int, bool) string { return "" }

// skillRenderer covers the Claude Code-style Skill tool.
type skillRenderer struct{}

func (skillRenderer) Verb(*ToolBlock) string { return "Skill" }

func (skillRenderer) Subject(tool *ToolBlock, _ bool) string {
	name := strings.TrimPrefix(paramValue(tool.Input, "skill"), "/")
	if name == "" {
		return ""
	}
	if args := paramValue(tool.Input, "args"); args != "" {
		return name + " " + truncatePlain(args, 40)
	}
	return name
}

func (skillRenderer) Result(tool *ToolBlock, _ bool) string {
	switch tool.Status {
	case ToolFailed:
		return "failed"
	case ToolDone:
		return "loaded"
	}
	return ""
}

func (skillRenderer) Detail(*ToolBlock, int, bool) string { return "" }

// memorySaveRenderer covers memory_save (memdir topic write).
type memorySaveRenderer struct{}

func (memorySaveRenderer) Verb(*ToolBlock) string { return "Remember" }

func (memorySaveRenderer) Subject(tool *ToolBlock, _ bool) string {
	typ := paramValue(tool.Input, "type")
	name := paramValue(tool.Input, "name")
	switch {
	case typ != "" && name != "":
		return typ + " · " + name
	case name != "":
		return name
	case typ != "":
		return typ
	default:
		return ""
	}
}

func (memorySaveRenderer) Result(tool *ToolBlock, _ bool) string {
	switch tool.Status {
	case ToolFailed:
		return "failed"
	case ToolDone:
		if file := memorySavedFilename(tool.Output); file != "" {
			return file
		}
		return "saved"
	}
	return ""
}

func (memorySaveRenderer) Detail(*ToolBlock, int, bool) string { return "" }

// memorySearchRenderer covers memory_search.
type memorySearchRenderer struct{}

func (memorySearchRenderer) Verb(*ToolBlock) string { return "Search" }

func (memorySearchRenderer) Subject(tool *ToolBlock, _ bool) string {
	query := firstNonEmpty(paramValue(tool.Input, "query"), paramValue(tool.Input, "q"))
	if query == "" {
		if typ := paramValue(tool.Input, "type"); typ != "" {
			return "memory type:" + typ
		}
		return "memory"
	}
	return fmt.Sprintf("memory %q", truncatePlain(query, 40))
}

func (memorySearchRenderer) Result(tool *ToolBlock, _ bool) string {
	switch tool.Status {
	case ToolFailed:
		return "failed"
	case ToolDone:
		out := strings.TrimSpace(tool.Output)
		if out == "" || strings.Contains(strings.ToLower(out), "no matching") || strings.Contains(strings.ToLower(out), "no memories") {
			return "no matches"
		}
		// Count topic blocks separated by blank lines, or fallback to line count.
		blocks := 0
		for _, chunk := range strings.Split(out, "\n\n") {
			if strings.TrimSpace(chunk) != "" {
				blocks++
			}
		}
		if blocks > 0 {
			return fmt.Sprintf("%d hits", blocks)
		}
		return "done"
	}
	return ""
}

func (memorySearchRenderer) Detail(*ToolBlock, int, bool) string { return "" }

type todoItemView struct {
	ID         string `json:"id"`
	Subject    string `json:"subject"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form"`
}

func parseTodoItems(input string) []todoItemView {
	input = strings.TrimSpace(input)
	if input == "" || input == "{}" {
		return nil
	}
	var parsed struct {
		Items []todoItemView `json:"items"`
		// Claude Code / some models may still emit "todos".
		Todos []todoItemView `json:"todos"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil
	}
	if len(parsed.Items) > 0 {
		return parsed.Items
	}
	return parsed.Todos
}

func allTodoCompleted(items []todoItemView) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		status := item.Status
		if status == "" {
			status = string(todo.StatusPending)
		}
		if status != string(todo.StatusCompleted) {
			return false
		}
	}
	return true
}

func todoProgressSubject(items []todoItemView) string {
	var pending, inProgress, completed int
	var activeForm string
	for _, item := range items {
		switch item.Status {
		case string(todo.StatusInProgress):
			inProgress++
			if activeForm == "" {
				activeForm = firstNonEmpty(item.ActiveForm, item.Subject)
			}
		case string(todo.StatusCompleted):
			completed++
		default:
			pending++
		}
	}
	total := len(items)
	switch {
	case inProgress > 0 && activeForm != "":
		return fmt.Sprintf("%d/%d · %s", completed, total, truncatePlain(activeForm, 36))
	case inProgress > 0:
		return fmt.Sprintf("%d items · %d in progress", total, inProgress)
	case completed == total:
		return fmt.Sprintf("%d done", total)
	default:
		return fmt.Sprintf("%d items · %d pending", total, pending)
	}
}

func memorySavedFilename(output string) string {
	// "Saved project memory to foo.md and updated MEMORY.md index."
	const mid = " memory to "
	idx := strings.Index(output, mid)
	if idx < 0 {
		return ""
	}
	rest := output[idx+len(mid):]
	end := strings.IndexAny(rest, " \n")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// --- edit diff -------------------------------------------------------------

type diffOpKind int

const (
	diffEqual diffOpKind = iota
	diffDel
	diffAdd
)

type diffOp struct {
	kind diffOpKind
	line string
}

// lineDiff computes a longest-common-subsequence line diff of a -> b. It is the
// classic O(n*m) dynamic-programming formulation; callers guard against
// pathological sizes before invoking.
func lineDiff(a, b []string) []diffOp {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	ops := make([]diffOp, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			ops = append(ops, diffOp{diffEqual, a[i]})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			ops = append(ops, diffOp{diffDel, a[i]})
			i++
		} else {
			ops = append(ops, diffOp{diffAdd, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{diffDel, a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{diffAdd, b[j]})
	}
	return ops
}

// renderEditDiff builds a compact, color-coded diff of oldS -> newS. Removed
// lines first, then added lines (mirror of how a review reads), each capped to
// the available width and the overall block capped to
// maxExpandedToolOutputLines. Pure additions (empty old_string) show only the
// new content. Returns "" when there is nothing to show.
func renderEditDiff(oldS, newS string, width int) string {
	contentWidth := max(1, width-6)
	oldLines := splitTrimmed(oldS)
	newLines := splitTrimmed(newS)
	if len(newLines) == 0 && len(oldLines) == 0 {
		return ""
	}
	if len(oldLines) == 0 {
		return joinDiffLines(addPrefix(newLines, "+"), contentWidth)
	}
	// Guard the O(n*m) LCS table against huge inputs by falling back to a plain
	// addition view of the new content.
	if len(oldLines)*len(newLines) > 20000 {
		return joinDiffLines(addPrefix(newLines, "+"), contentWidth)
	}
	var out []string
	for _, op := range lineDiff(oldLines, newLines) {
		switch op.kind {
		case diffDel:
			out = append(out, renderDiffLine("-"+op.line, contentWidth))
		case diffAdd:
			out = append(out, renderDiffLine("+"+op.line, contentWidth))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return joinDiffLines(out, contentWidth)
}

// renderEditDiffSplit renders oldS -> newS as a two-column side-by-side diff
// (old on the left in red, new on the right in green, equal lines dim on both),
// reusing lineDiff so the alignment matches the unified view. Used by the diff
// viewer's split mode (Phase 5D.1, session source only). Returns "" when there
// is nothing to show.
func renderEditDiffSplit(oldS, newS string, width int) string {
	contentWidth := max(1, width-6)
	oldLines := splitTrimmed(oldS)
	newLines := splitTrimmed(newS)
	if len(oldLines) == 0 && len(newLines) == 0 {
		return ""
	}
	halfW := max(8, (contentWidth-1)/2)
	blank := strings.Repeat(" ", halfW)
	sep := dimStyle.Render("│")
	delStyle := errorStyle
	addStyle := successStyle
	pad := func(s string) string {
		s = truncatePlain(s, halfW)
		if p := halfW - lipgloss.Width(s); p > 0 {
			s += strings.Repeat(" ", p)
		}
		return s
	}
	// Guard the O(n*m) LCS table like renderEditDiff; fall back to additions on
	// the right column for huge inputs.
	if len(oldLines)*len(newLines) > 20000 {
		var out []string
		for _, l := range newLines {
			out = append(out, blank+sep+addStyle.Render(pad(l)))
		}
		return strings.Join(out, "\n")
	}
	var out []string
	for _, op := range lineDiff(oldLines, newLines) {
		switch op.kind {
		case diffEqual:
			l := pad(op.line)
			out = append(out, dimStyle.Render(l)+sep+dimStyle.Render(pad(op.line)))
		case diffDel:
			out = append(out, delStyle.Render(pad(op.line))+sep+blank)
		case diffAdd:
			out = append(out, blank+sep+addStyle.Render(pad(op.line)))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n")
}

func addPrefix(lines []string, prefix string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = renderDiffLine(prefix+l, max(1, len(l)+2))
	}
	return out
}

// joinDiffLines joins already-styled diff lines, truncating the block to
// maxExpandedToolOutputLines with a dim "... +N more" tail.
func joinDiffLines(lines []string, width int) string {
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > maxExpandedToolOutputLines {
		hidden := len(lines) - maxExpandedToolOutputLines
		lines = append(lines[:maxExpandedToolOutputLines], dimStyle.Render(fmt.Sprintf("... +%d more", hidden)))
	}
	return strings.Join(lines, "\n")
}
