package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/tool/builtin"
)

func TestToolRenderersVerbSubjectResult(t *testing.T) {
	cases := []struct {
		name    string
		tool    *ToolBlock
		verb    string
		subject string
		result  string
	}{
		{
			name:    "read_file done counts lines",
			tool:    &ToolBlock{Name: "read_file", Status: ToolDone, Input: `{"path":"README.md"}`, Output: "line1\nline2\nline3"},
			verb:    "Read",
			subject: "README.md",
			result:  "3 lines",
		},
		{
			name:    "write_file done counts content lines",
			tool:    &ToolBlock{Name: "write_file", Status: ToolDone, Input: `{"path":"src/foo.go","content":"a\nb"}`},
			verb:    "Write",
			subject: "src/foo.go",
			result:  "2 lines",
		},
		{
			name:    "edit_file update",
			tool:    &ToolBlock{Name: "edit_file", Status: ToolDone, Input: `{"path":"a.go","old_string":"x","new_string":"y"}`},
			verb:    "Update",
			subject: "a.go",
			result:  "updated",
		},
		{
			name:   "edit_file create when old empty",
			tool:   &ToolBlock{Name: "edit_file", Status: ToolDone, Input: `{"path":"new.go","old_string":"","new_string":"y"}`},
			verb:   "Create",
			result: "created",
		},
		{
			name:    "search parses match and file counts",
			tool:    &ToolBlock{Name: "search_text", Status: ToolDone, Input: `{"pattern":"foo"}`, Output: "12 matches in 3 files"},
			verb:    "Search",
			subject: `"foo"`,
			result:  "Found 12 matches in 3 files",
		},
		{
			name:    "run_command prefers summary",
			tool:    &ToolBlock{Name: "run_command", Status: ToolDone, Input: `{"command":"go test ./..."}`, Summary: "go test: ok 1", Duration: 120 * time.Millisecond},
			verb:    "Run",
			subject: `"go test ./..."`,
			result:  "go test: ok 1",
		},
		{
			name: "todo_write progress",
			tool: &ToolBlock{
				Name:   "todo_write",
				Status: ToolDone,
				Input:  `{"items":[{"id":"1","subject":"Wire auth","status":"completed"},{"id":"2","subject":"Add tests","status":"in_progress","active_form":"Adding tests"}]}`,
			},
			verb:    "Todo",
			subject: "1/2 · Adding tests",
			result:  "1/2 · Adding tests",
		},
		{
			name:    "todo_write cleared when all completed",
			tool:    &ToolBlock{Name: "todo_write", Status: ToolDone, Input: `{"items":[{"subject":"Done","status":"completed"}]}`},
			verb:    "Todo",
			subject: "1 done · cleared",
			result:  "cleared",
		},
		{
			name:    "skill loaded",
			tool:    &ToolBlock{Name: "skill", Status: ToolDone, Input: `{"skill":"commit"}`},
			verb:    "Skill",
			subject: "commit",
			result:  "loaded",
		},
		{
			name:    "memory_save",
			tool:    &ToolBlock{Name: "memory_save", Status: ToolDone, Input: `{"type":"feedback","name":"prefer tabs","description":"d","content":"c"}`, Output: "Saved feedback memory to feedback_prefer_tabs.md and updated MEMORY.md index."},
			verb:    "Remember",
			subject: "feedback · prefer tabs",
			result:  "feedback_prefer_tabs.md",
		},
		{
			name:    "memory_search",
			tool:    &ToolBlock{Name: "memory_search", Status: ToolDone, Input: `{"query":"tabs"}`, Output: "a.md [feedback]\n\nb.md [user]\n"},
			verb:    "Search",
			subject: `memory "tabs"`,
			result:  "2 hits",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := RendererFor(tc.tool.Name)
			if got := r.Verb(tc.tool); got != tc.verb {
				t.Errorf("Verb = %q, want %q", got, tc.verb)
			}
			if tc.subject != "" {
				if got := r.Subject(tc.tool, false); got != tc.subject {
					t.Errorf("Subject = %q, want %q", got, tc.subject)
				}
			}
			if got := r.Result(tc.tool, false); got != tc.result {
				t.Errorf("Result = %q, want %q", got, tc.result)
			}
		})
	}
}

// TestToolRenderersCoverRealFileToolNames is the contract test for the drift
// class where a tool's Name() and the renderer map key fall out of sync: the
// edit tool was once named "edit" while the renderer keyed on "edit_file", so
// edits silently fell back to the default renderer (no diff). This asserts the
// renderer map has an entry under each file tool's REAL Name(), so a rename
// that forgets the renderer fails here.
func TestToolRenderersCoverRealFileToolNames(t *testing.T) {
	tools := []tool.Tool{
		builtin.NewReadFileTool(),
		builtin.NewWriteFileTool(),
		builtin.NewEditFileTool(),
		builtin.NewListDirTool(),
		builtin.NewSearchTextTool(),
		builtin.NewRunCommandTool(),
	}
	for _, tl := range tools {
		name := tl.Name()
		if _, ok := toolRenderers[name]; !ok {
			t.Errorf("no renderer registered under %q (real Name() of %T); edits/writes would lose their diff view", name, tl)
		}
	}
}

// TestDefaultRendererAllEmpty is the zero-regression contract: every tool not
// in toolRenderers (MCP tools, task, spawn, ask_user) must still fall through.
func TestDefaultRendererAllEmpty(t *testing.T) {
	for _, name := range []string{"mcp__foo__bar", "task", "spawn", "ask_user"} {
		r := RendererFor(name)
		tool := &ToolBlock{Name: name, Status: ToolDone, Input: `{"x":1}`}
		if r.Verb(tool) != "" || r.Subject(tool, false) != "" || r.Result(tool, false) != "" || r.Detail(tool, 80, false) != "" {
			t.Fatalf("renderer for %s should be defaultRenderer (all empty)", name)
		}
	}
}

func TestTodoWriteDetailListsChecklist(t *testing.T) {
	tool := &ToolBlock{
		Name:   "todo_write",
		Status: ToolDone,
		Input: `{"items":[
			{"id":"1","subject":"Done item","status":"completed"},
			{"id":"2","subject":"Active","status":"in_progress","active_form":"Doing active"},
			{"id":"3","subject":"Later","status":"pending"}
		]}`,
		Collapsed: false,
	}
	view := renderToolActivity(tool, 100)
	for _, want := range []string{"Todo", "Doing active", "Later", "Done item"} {
		if !strings.Contains(view, want) {
			t.Fatalf("todo detail missing %q:\n%s", want, view)
		}
	}
}

func TestSkillAndMemoryRenderInActivity(t *testing.T) {
	skill := &ToolBlock{Name: "skill", Status: ToolDone, Input: `{"skill":"/review-pr"}`}
	if view := renderToolActivity(skill, 80); !strings.Contains(view, "Skill") || !strings.Contains(view, "review-pr") {
		t.Fatalf("skill activity:\n%s", view)
	}
	mem := &ToolBlock{Name: "memory_save", Status: ToolDone, Input: `{"type":"user","name":"name"}`, Output: "Saved user memory to user_name.md and updated MEMORY.md index."}
	if view := renderToolActivity(mem, 80); !strings.Contains(view, "Remember") || !strings.Contains(view, "user · name") {
		t.Fatalf("memory activity:\n%s", view)
	}
}

func TestUnregisteredToolKeepsRawNameInActivity(t *testing.T) {
	tool := &ToolBlock{Name: "mcp__foo__bar", Status: ToolDone, Input: `{"x":1}`, Summary: "ran"}
	view := renderToolActivity(tool, 100)
	if !strings.Contains(view, "mcp__foo__bar") {
		t.Fatalf("unregistered tool must keep its raw name (no verb substitution):\n%s", view)
	}
}

func TestEditRendererDiffShowsAddedAndRemoved(t *testing.T) {
	tool := &ToolBlock{
		Name:   "edit_file",
		Status: ToolDone,
		Input:  `{"path":"a.go","old_string":"old line\nkeep","new_string":"new line\nkeep"}`,
	}
	view := renderToolActivity(tool, 100)
	for _, want := range []string{"-old line", "+new line"} {
		if !strings.Contains(view, want) {
			t.Fatalf("edit diff missing %q:\n%s", want, view)
		}
	}
	// The compact edit diff drops unchanged context lines so the actual change
	// is not buried.
	if strings.Contains(view, "keep") {
		t.Fatalf("compact edit diff should drop unchanged context:\n%s", view)
	}
}

func TestEditRendererCreateShowsNewContent(t *testing.T) {
	tool := &ToolBlock{
		Name:   "edit_file",
		Status: ToolDone,
		Input:  `{"path":"new.go","old_string":"","new_string":"package main\nfunc main() {}"}`,
	}
	view := renderToolActivity(tool, 100)
	if !strings.Contains(view, "+package main") {
		t.Fatalf("create edit should show new content as additions:\n%s", view)
	}
}

func TestRenderToolActivityRunCommandAppendsDuration(t *testing.T) {
	tool := &ToolBlock{
		Name:     "run_command",
		Status:   ToolDone,
		Input:    `{"command":"go test ./internal/tui"}`,
		Summary:  "go test: ok 1",
		Duration: 120 * time.Millisecond,
	}
	view := renderToolActivity(tool, 100)
	// Semantic result + trailing duration both appear on the result line.
	for _, want := range []string{"Run", "go test: ok 1", "120ms"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
}

func TestVerboseSubjectShowsFullPath(t *testing.T) {
	prevRoot := displayProjectRoot
	prevVerbose := renderVerbose
	defer func() { displayProjectRoot = prevRoot; renderVerbose = prevVerbose }()

	root := filepath.Join("tmp", "proj")
	setDisplayProjectRoot(root)
	abs := absPath(filepath.Join(root, "src", "foo.go"))
	tool := &ToolBlock{Name: "read_file", Status: ToolDone, Input: fmt.Sprintf(`{"path":%q}`, abs)}

	r := RendererFor("read_file")
	if got := r.Subject(tool, false); got != "src/foo.go" {
		t.Errorf("non-verbose subject = %q, want src/foo.go", got)
	}
	if got := r.Subject(tool, true); got != filepath.ToSlash(abs) {
		t.Errorf("verbose subject = %q, want full %q", got, filepath.ToSlash(abs))
	}
}

func TestPermissionPanelShowsEditDiff(t *testing.T) {
	event := &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "edit_file",
		Risk:     "medium",
		Input:    `{"path":"a.go","old_string":"old","new_string":"new"}`,
	}
	panel := renderPermissionPanel(event, choiceOnce, false, "", true, 80, 0)
	for _, want := range []string{"-old", "+new"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("edit confirmation should show the diff %q:\n%s", want, panel)
		}
	}
}

func TestPermissionPanelShowsWritePreview(t *testing.T) {
	event := &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"a.go","content":"package main\nfunc main() {}"}`,
	}
	panel := renderPermissionPanel(event, choiceOnce, false, "", true, 80, 0)
	if !strings.Contains(panel, "+package main") {
		t.Fatalf("write confirmation should show a content preview:\n%s", panel)
	}
}
