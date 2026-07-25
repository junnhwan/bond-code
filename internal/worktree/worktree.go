// Package worktree 管理 git worktree 的创建/绑定/清理，为 subagent 的
// 并行写节点提供物理隔离：每个 coder 节点独占一个 worktree，避免并发改同一工作区。
//
// 这是 worktree 隔离的基础设施层。subagent 派发 coder 任务时调 Create
// 得到独立工作目录，任务结束调 Remove 清理；discardChanges=false 时若有未提交改动则
// 拒绝删除，防误丢工作。
package worktree

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manager 在指定根仓库下创建/移除 worktree。
type Manager struct {
	rootDir string // 主仓库根（git worktree add 的 base，需是 git 仓库）
}

func NewManager(rootDir string) *Manager { return &Manager{rootDir: rootDir} }

// RootDir 返回主仓库根（worktree 的 base，用作路径重写的源前缀）。
func (m *Manager) RootDir() string { return m.rootDir }

// Worktree 是一个已创建的 git worktree。
type Worktree struct {
	Path   string // worktree 检出路径
	Branch string // 关联分支
	TaskID string // 绑定的任务 ID
}

// Create 在 rootDir/.bondcode-worktrees/<taskID> 创建一个新 worktree + 分支，绑定 taskID。
// branch 为空时默认 bondcode/<taskID>。基于 HEAD 创建。
func (m *Manager) Create(taskID, branch string) (*Worktree, error) {
	if taskID == "" {
		return nil, fmt.Errorf("taskID is required")
	}
	if branch == "" {
		branch = "bondcode/" + sanitize(taskID)
	}
	path := filepath.Join(m.rootDir, ".bondcode-worktrees", sanitize(taskID))
	cmd := exec.Command("git", "worktree", "add", "-b", branch, path, "HEAD")
	cmd.Dir = m.rootDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return &Worktree{Path: path, Branch: branch, TaskID: taskID}, nil
}

// Remove 移除 worktree 及其分支。discardChanges=false 时若 worktree 有未提交改动则拒绝
// 删除（防误丢工作）；true 则强制。
func (m *Manager) Remove(w *Worktree, discardChanges bool) error {
	if w == nil {
		return nil
	}
	if !discardChanges {
		if dirty, err := HasUncommittedChanges(w.Path); err == nil && dirty {
			return fmt.Errorf("worktree %s has uncommitted changes; pass discardChanges=true to force", w.Path)
		}
	}
	// best-effort：清理失败不阻断调用方（worktree 残留可手动 git worktree prune）
	cmd := exec.Command("git", "worktree", "remove", "--force", w.Path)
	cmd.Dir = m.rootDir
	_ = cmd.Run()
	cmd2 := exec.Command("git", "branch", "-D", w.Branch)
	cmd2.Dir = m.rootDir
	_ = cmd2.Run()
	return nil
}

// HasUncommittedChanges 报告给定工作目录是否有未提交改动。
func HasUncommittedChanges(path string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// sanitize 把任意字符串转成 filesystem/git 安全的分支/目录段。
func sanitize(s string) string {
	r := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(s))
	return strings.Trim(r, "-")
}
