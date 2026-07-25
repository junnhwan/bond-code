package app

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/hook"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/tool/builtin"
)

// TestPlanModeDisablesEveryMutatingBuiltinTool guards the read-only plan-mode
// boundary against tool-name drift. An earlier revision listed "edit_file" in
// planModeDisabledTools while the edit tool's Name() returned "edit", so plan
// mode did NOT actually block edits — a silent hole in the safety boundary.
// This test ties the disabled list to each mutating tool's real Name(), so a
// future rename that forgets the list fails here instead of in production.
func TestPlanModeDisablesEveryMutatingBuiltinTool(t *testing.T) {
	mutating := []tool.Tool{
		builtin.NewWriteFileTool(),
		builtin.NewEditFileTool(),
		builtin.NewRunCommandTool(),
	}
	disabled := make(map[string]bool, len(planModeDisabledTools))
	for _, n := range planModeDisabledTools {
		disabled[n] = true
	}
	for _, tl := range mutating {
		name := tl.Name()
		if !disabled[name] {
			t.Errorf("plan mode does not disable %q (real Name() of %T); read-only boundary broken", name, tl)
		}
	}
	// Sanity: the list must not trivially block read-only inspection, or the
	// assertion above would pass for the wrong reason.
	for _, name := range []string{"read_file", "list_dir", "search_text"} {
		if disabled[name] {
			t.Errorf("plan mode should leave read-only %q available", name)
		}
	}
}

// TestProtectedPathHookCoversRealEditToolName guards the .git / lock-file write
// hook against the same drift: it must block the edit tool under the name the
// tool actually registers, not a hardcoded guess.
func TestProtectedPathHookCoversRealEditToolName(t *testing.T) {
	edit := builtin.NewEditFileTool()
	write := builtin.NewWriteFileTool()
	for _, tl := range []tool.Tool{edit, write} {
		d := blockProtectedWritePaths(context.Background(), hook.PreToolUseInput{
			ToolName: tl.Name(),
			Input:    `{"path":".git/HEAD","old_string":"a","new_string":"b","content":"b"}`,
		})
		if !d.IsBlocking() {
			t.Errorf("protected-path hook did not block %q on .git/HEAD — guard drift", tl.Name())
		}
	}
}
