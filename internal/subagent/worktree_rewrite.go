package subagent

import "strings"

// RewriteWorktreePaths 把工具输入 JSON 里的 RepoRoot 路径前缀替换为 WorktreePath。
//
// 工具输入是 JSON 字符串，路径在其中以转义形式出现（Windows D:\repo → JSON "D:\\repo"）。
// 直接用 OS 路径 ReplaceAll 在 Windows 会失配（找不到单反斜杠形式），所以先把 RepoRoot
// 与 WorktreePath 都转成 JSON 转义形式（反斜杠→双）再替换。Unix 路径无反斜杠，不受影响。
//
// 这是 worktree 物理隔离的强制层：即便模型把主仓库的绝对路径写进工具输入，也会被改写到
// worktree，保证 coder 节点不污染主仓库。复用 agent.Loop 后由 app 层 LoopFactory 包成
// PreToolUse hook 挂到子 Loop。
func RewriteWorktreePaths(args, repoRoot, worktreePath string) string {
	if repoRoot == "" || worktreePath == "" || repoRoot == worktreePath {
		return args
	}
	repoEsc := strings.ReplaceAll(repoRoot, `\`, `\\`)
	wtEsc := strings.ReplaceAll(worktreePath, `\`, `\\`)
	if repoEsc == wtEsc {
		return args
	}
	return strings.ReplaceAll(args, repoEsc, wtEsc)
}
