# BondCode 参考手册

[English](reference.md) | [中文](reference.zh-CN.md)

使用与能力边界速查。界面演示放在 README（截图 / GIF）。「什么工具真正注册」以 `internal/app/bootstrap.go` + `bootstrap_tools.go` 为准。

## TUI（简）

`bondcode` 默认进入 TUI（`bondcode chat` 为兼容别名）。

布局：**transcript** → **turn status**（忙碌时）→ **`❯` 输入框**（model · mode · ctx · permission）→ **快捷键提示条**。会话、权限、状态、diff、MCP、skills 等按需用 overlay 或 slash 打开，无常驻侧栏。

### 快捷键

| 按键 | 作用 |
|------|------|
| `Enter` | 发送（Agent 忙时入队） |
| `Shift+Enter` / `Alt+Enter` | 换行（Windows 无法区分 Shift+Enter 时用 `Alt+Enter`） |
| `Tab` | 在 prompt 与 scrollback 焦点间切换 |
| `Space` | scrollback 且草稿为空时回到 prompt |
| `Esc` | 关 overlay / 取消运行 / 清空草稿 / 离开 scrollback |
| `Ctrl+C` | 中断；空闲时清空草稿或退出 |
| `Ctrl+D` | scrollback 半页下滚；composer 为空则退出 |
| `Ctrl+U` | scrollback 半页上滚 |
| `Shift+Tab` / `Alt+M` | 切换 normal / plan mode |
| `Ctrl+O` | 展开/折叠工具详情与 transcript 密度 |
| `Ctrl+R` | 反向搜索 prompt 历史 |
| `Ctrl+Up` | Agent 切换器（有子 agent 时） |
| `Ctrl+G` | 用 `$EDITOR` / `$VISUAL` 编辑草稿 |
| `Ctrl+S` | 暂存 / 取回草稿 |
| `Ctrl+L` | 重绘终端 |
| `PgUp` / `PgDown` / 滚轮 | 滚动 transcript |
| `Home` / `End` | 到 transcript 顶 / 底 |
| `@path` / `@path:42-60` | 路径提及，提交前展开文件上下文 |

无 command palette（`Ctrl+P`）或 leader 键；用 slash 或 overlay。

### Slash 命令

默认发现 / `/help` 顺序：

`/help` `/clear` `/resume` `/model` `/permissions` `/compact` `/status` `/context` `/memory` `/skills` `/undo` `/export` `/copy` `/retry` `/diff` `/history` `/exit`

说明：

- `/history` — session 时间线 / fork-resume 浏览器  
- `/diff` — 本 session 文件变更  
- `/permissions [mode]` — 查看或切换权限模式  

仍可执行但不在默认发现列表：`/new`、`/sessions`（→ `/resume`）、`/session`、`/cost`、`/theme`、`/quit`、`/q`（→ `/exit`）。

## 内置工具

工具面保持精简：读写 / 搜索 / shell。git、测试、格式化走 `run_command`。

### Core（默认注册）

| 工具 | 作用 | 典型风险 |
|------|------|----------|
| `read_file` | 读文件 | low |
| `write_file` | 写文件 | medium |
| `edit_file` | 精确改已有文件 | medium |
| `list_dir` | 列目录 | low |
| `search_text` | 搜文本 | low |
| `run_command` | 本地 shell（git、go test 等） | 按命令 |
| `ask_user` | 向用户提问 | low |
| `memory_search` | 搜 memdir 主题 | low |
| `memory_save` | 写主题并更新 MEMORY.md 索引 | medium |
| `todo_read` | 读 session todo | low |
| `todo_write` | 整表替换 session todo | medium |
| `task` | 同步子 agent（类 Claude Code Task） | medium |
| `skill` | 按名展开本地 skill | low |

### Extended（配置开启）

| 工具 | 注册条件 |
|------|----------|
| `skill` | `skills.enabled`（默认开） |
| `task` | `subagent.enabled`（默认开） |
| `agent_task` / `task_*` / `task_backend` | `collaboration.enabled`（默认开） |
| `team_*` | `collaboration.enabled`（默认开） |
| `mailbox_*` | `collaboration.enabled`（默认开） |
| `mcp__<server>__<tool>` | `mcp.enabled` **且** `mcp.inject_tools` |

`spawn` 仅在 `subagent.enable_spawn` 时注册。Skills 目录：`~/.bondcode/skills` 与 `<project>/.bondcode/skills`（可选 `skills.root`）。

### 委派怎么选

| 目标 | 用什么 |
|------|--------|
| 单个有界同步子任务 | `task` |
| 若干独立子任务 | 多次 `task`，或 `tasks[]` + `mode=parallel` |
| 流水线 A → B | `mode=chain` |
| 续跑某次子 agent | `task` + `resume_task_id` |
| 长生命周期 / 团队协作 | `agent_task` + team / mailbox |

### 文件工具（先读后写）

对**已有文件**的 `write_file` / `edit_file` 需要先有一次成功的 `read_file`，且字节仍匹配。切换 session 或 `/undo` 后需重新 read。Shell / MCP 不在此边界内。

## 安全

真实工具执行都经过 `safety.Policy` + confirmer：

| 级别 | 含义 |
|------|------|
| low | 多为只读 |
| medium | 受控写入 / 测试 / 外部工具 |
| high | 破坏性 / force-push 类 / 高风险网络 |
| blocked | 硬拒绝（如 `rm -rf /`、`git push --force`、`curl \| sh`） |

- `--yes` 只自动批准 **low / medium**  
- **high** 仍须显式确认  
- **blocked** 永不执行（模式、`--yes`、子 agent 都不能绕过）

### 权限模式

`--permission-mode default|accept-edits|plan|bypass`（TUI：`/permissions`）。

| 模式 | 行为 |
|------|------|
| `default` | 标准策略 |
| `accept-edits` | 自动接受普通文件编辑 |
| `plan` | 阻止写/执行类工具，偏规划 |
| `bypass` | 仅当 `safety.enable_bypass` 或可信 `--enable-bypass` |

模式切换会先写入 session JSONL 再生效。

## 上下文、记忆、Todo（摘要）

- **上下文**：每轮完整性 + tool-result 微裁剪/落盘；阈值或 `/compact` 结构化 checkpoint；`prompt_too_long` 时 emergency shrink 后重试。  
- **记忆**：本地 memdir（`MEMORY.md` 索引 + 主题 `*.md`）；工具 `memory_save` / `memory_search`。不是向量库。  
- **Todo**：`todo_write` 整表替换；按 session 落在项目数据目录。

## Session 与 debug

```powershell
go run ./cmd/bondcode session list
go run ./cmd/bondcode session show <id>
go run ./cmd/bondcode session export <id> <path>
go run ./cmd/bondcode session import <id> <path>
go run ./cmd/bondcode session fork <src> <dst>
go run ./cmd/bondcode session delete <id>
go run ./cmd/bondcode session trace [id]           # 省略 id = 最近一次
go run ./cmd/bondcode session trace [id] --debug   # 叠加模型决策层
```

可选决策 trace：`--debug` 或 `BONDCODE_DEBUG=1` → `<session-dir>/<id>.debug.jsonl`。

## MCP

仅 stdio server。CLI：`bondcode mcp connect …`、`mcp list --config`、`mcp reload --config`。  
仅当 `mcp.enabled` 与 `mcp.inject_tools` 同时为真时注入工具，命名 `mcp__<server>__<tool>`。resources / prompts / subscriptions 不在当前范围。

## 能力边界

- **主路径**：ReAct 循环 + 核心工具 + 安全 + 上下文 + session 审计 + `task` + skills + 协作工具（默认开；`collaboration.enabled: false` 可关）  
- **配置开启**：MCP 工具注入  
- **默认关**：`spawn`  
- **模型 I/O**：内部 `llm.Client` + Anthropic-compatible HTTP/SSE  
- **Skills**：仅本地 `SKILL.md`（无远端市场）  
- **子 agent**：按 profile 限工具；真实执行复用同一套 Policy/Confirmer  

## 配置指针

| 项 | 位置 |
|----|------|
| 完整 YAML 示例 | `configs/config.example.yaml` |
| 项目覆盖 | `bondcode.yaml`（本地） |
| 用户默认 | `~/.bondcode/config.yaml` |
| 状态根目录 | `~/.bondcode/projects/<编码cwd>/`（或 `BONDCODE_HOME`） |
