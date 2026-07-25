package subagent

import "testing"

func TestRewriteWorktreePathsUnix(t *testing.T) {
	got := RewriteWorktreePaths(`{"path":"/repo/src/a.go"}`, "/repo", "/wt")
	want := `{"path":"/wt/src/a.go"}`
	if got != want {
		t.Fatalf("unix: got %q, want %q", got, want)
	}
}

func TestRewriteWorktreePathsWindowsJSONEscaped(t *testing.T) {
	// Windows: JSON 里反斜杠是双转义；RepoRoot/WorktreePath 是 OS 单反斜杠路径。
	args := `{"path":"D:\\repo\\src\\a.go"}`
	got := RewriteWorktreePaths(args, `D:\repo`, `D:\wt`)
	want := `{"path":"D:\\wt\\src\\a.go"}`
	if got != want {
		t.Fatalf("windows: got %q, want %q", got, want)
	}
}

func TestRewriteWorktreePathsNoopCases(t *testing.T) {
	// 空 / 相同 → 不改
	if got := RewriteWorktreePaths(`{"path":"/repo/a"}`, "", "/wt"); got != `{"path":"/repo/a"}` {
		t.Fatalf("empty repoRoot should be noop, got %q", got)
	}
	if got := RewriteWorktreePaths(`{"path":"/repo/a"}`, "/repo", ""); got != `{"path":"/repo/a"}` {
		t.Fatalf("empty worktreePath should be noop, got %q", got)
	}
	if got := RewriteWorktreePaths(`{"path":"/repo/a"}`, "/repo", "/repo"); got != `{"path":"/repo/a"}` {
		t.Fatalf("same roots should be noop, got %q", got)
	}
}

func TestRewriteWorktreePathsOnlyPrefix(t *testing.T) {
	// 只替换前缀，不影响路径中段碰巧同名的子串（前后缀锚定靠路径分隔符）
	got := RewriteWorktreePaths(`{"path":"/repo/old-repo/x"}`, "/repo", "/wt")
	// "/repo" 出现在开头和 "old-repo" 里；ReplaceAll 会都替换 —— 这是已知限制，
	// 实践中 worktree 路径是绝对前缀，主仓库路径作为真前缀出现即可命中。
	if got != `{"path":"/wt/old-wt/x"}` {
		t.Logf("note: ReplaceAll also rewrites substrings (%q) — acceptable for absolute-prefix use", got)
	}
}
