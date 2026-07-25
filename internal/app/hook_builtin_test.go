package app

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/hook"
)

func TestBlockProtectedWritePaths(t *testing.T) {
	cases := []struct {
		name    string
		tool    string
		input   string
		blocked bool
	}{
		{"git config", "write_file", `{"path":".git/config","content":"x"}`, true},
		{"git HEAD edit", "edit_file", `{"path":".git/HEAD","old_string":"a","new_string":"b"}`, true},
		{"go.sum lock", "write_file", `{"path":"go.sum","content":"x"}`, true},
		{"nested go.sum", "write_file", `{"path":"pkg/mod/go.sum","content":"x"}`, true},
		{"source file allowed", "write_file", `{"path":"src/main.go","content":"x"}`, false},
		{"prefixed source allowed", "write_file", `{"path":"./internal/app/foo.go","content":"x"}`, false},
		{"read not blocked", "read_file", `{"path":".git/config"}`, false},
		{"readme allowed", "write_file", `{"path":"README.md","content":"x"}`, false},
		{"empty path allowed", "write_file", `{"content":"x"}`, false},
	}
	for _, tc := range cases {
		d := blockProtectedWritePaths(context.Background(), hook.PreToolUseInput{ToolName: tc.tool, Input: tc.input})
		if d.IsBlocking() != tc.blocked {
			t.Errorf("%s: expected blocked=%v, got action=%s reason=%s", tc.name, tc.blocked, d.Action, d.Reason)
		}
	}
}

func TestRegisterBuiltinHooksNilSafe(t *testing.T) {
	registerBuiltinHooks(nil) // must not panic
}
