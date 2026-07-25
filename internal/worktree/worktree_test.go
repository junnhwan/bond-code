package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitInit 在 temp 目录建一个有初始 commit 的 git 仓库，返回其路径。
func gitInit(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.local")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("init"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return root
}

func TestManagerCreateAndRemove(t *testing.T) {
	root := gitInit(t)
	m := NewManager(root)

	wt, err := m.Create("task-1", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "README.md")); err != nil {
		t.Fatalf("worktree should contain the committed file: %v", err)
	}
	if wt.Branch != "bondcode/task-1" {
		t.Fatalf("expected default branch bondcode/task-1, got %q", wt.Branch)
	}

	if err := m.Remove(wt, true); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree path should be removed, got err=%v", err)
	}
}

func TestManagerRemoveRefusesDirtyByDefault(t *testing.T) {
	root := gitInit(t)
	m := NewManager(root)

	wt, err := m.Create("task-2", "custom-branch")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if wt.Branch != "custom-branch" {
		t.Fatalf("expected custom branch, got %q", wt.Branch)
	}
	// 在 worktree 里写未提交文件。
	if err := os.WriteFile(filepath.Join(wt.Path, "uncommitted.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.Remove(wt, false); err == nil {
		t.Fatal("Remove with discardChanges=false should refuse a dirty worktree")
	}
	// 强制删除应成功。
	if err := m.Remove(wt, true); err != nil {
		t.Fatalf("Remove with discardChanges=true failed: %v", err)
	}
}

func TestHasUncommittedChanges(t *testing.T) {
	root := gitInit(t)
	m := NewManager(root)
	wt, err := m.Create("task-3", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Remove(wt, true)

	if dirty, err := HasUncommittedChanges(wt.Path); err != nil || dirty {
		t.Fatalf("fresh worktree should be clean, got dirty=%v err=%v", dirty, err)
	}
	os.WriteFile(filepath.Join(wt.Path, "new.txt"), []byte("x"), 0o644)
	if dirty, err := HasUncommittedChanges(wt.Path); err != nil || !dirty {
		t.Fatalf("worktree with new file should be dirty, got dirty=%v err=%v", dirty, err)
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"Task 1":     "task-1",
		"../escape":  "escape",
		"ok_name-2":  "ok-name-2",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
