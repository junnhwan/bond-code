package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/junnhwan/bond-code/internal/hook"
	"github.com/junnhwan/bond-code/internal/tool"
)

// registerBuiltinHooks installs the built-in lifecycle hooks (roadmap C1). The
// hook plumbing existed but bootstrap wired an empty registry; these prove the
// pipeline is live and add value beyond the loop's built-in checks. They are
// safety-extending only — they never bypass safety.Policy's hard "blocked".
func registerBuiltinHooks(r *hook.Registry) {
	if r == nil {
		return
	}
	r.RegisterPreToolUse(blockProtectedWritePaths)
}

// blockProtectedWritePaths stops write_file / edit_file from silently rewriting
// version-control internals and dependency lock files. The agent may still do
// these with an explicit user request via run_command; this just blocks the
// quiet write/edit path so they can't happen as a side effect of editing.
func blockProtectedWritePaths(_ context.Context, in hook.PreToolUseInput) hook.PreToolUseDecision {
	if in.ToolName != tool.WriteFile && in.ToolName != tool.EditFile {
		return hook.PreToolUseDecision{Action: hook.ActionAllow}
	}
	path := hookInputPath(in.Input)
	if path == "" {
		return hook.PreToolUseDecision{Action: hook.ActionAllow}
	}
	clean := filepath.Clean(path)
	if isProtectedWritePath(clean) {
		return hook.PreToolUseDecision{Action: hook.ActionBlock, Reason: "write to protected path blocked: " + clean}
	}
	return hook.PreToolUseDecision{Action: hook.ActionAllow}
}

// isProtectedWritePath reports paths the agent must not overwrite on its own:
// version-control internals and dependency lock files.
func isProtectedWritePath(path string) bool {
	normalized := filepath.ToSlash(path)
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == ".git" || strings.HasPrefix(normalized, ".git/") {
		return true
	}
	switch filepath.Base(normalized) {
	case "go.sum", "Cargo.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml":
		return true
	}
	return false
}

// hookInputPath extracts the "path" field from a tool's raw JSON input.
func hookInputPath(rawInput string) string {
	if strings.TrimSpace(rawInput) == "" {
		return ""
	}
	var parsed struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(rawInput), &parsed); err != nil {
		return ""
	}
	return parsed.Path
}
