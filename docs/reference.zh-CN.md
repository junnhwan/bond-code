# BondCode 参考手册

[English](reference.md) | [中文](reference.zh-CN.md)

使用与能力边界速查。界面演示放在 README（截图 / GIF）。「什么工具真正注册」以 `internal/app/bootstrap.go` + `bootstrap_tools.go` 为准。

## TUI（简）

`bondcode` 默认进入 TUI。

布局：**transcript** → **turn status**（忙碌时）→ **`❯` 输入框**（model · mode · ctx · permission）→ **快捷键提示条**。会话、权限、状态、diff、MCP、skills 等按需用 overlay 或 slash 打开，无常驻侧栏。

### 快捷键

| 按键 | 作用 |
|------|------|
| `Enter` | 发送（Agent 忙时入队） |
| `Ctrl+J` / `Alt+Enter` / `Shift+Enter` | 换行（Windows 推荐 `Ctrl+J`；Shift+Enter 常不可用） |
| `Tab` | 在 prompt 与 scrollback 焦点间切换 |
| `Space` | scrollback 且草稿为空时回到 prompt |
| `Esc` | 关 overlay / 取消运行 / 清空草稿 / 离开 scrollback |
| `Ctrl+C` | 中断；空闲时清空草稿或退出 |
| `Ctrl+D` | scrollback 半页下滚；composer 为空则退出 |
| `Ctrl+U` | scrollback 半页上滚 |
| `Shift+Tab` / `Alt+M` | 切换 normal / plan mode |
| `Ctrl+O` | 展开/收起工具详情（路径、输出）。不展开历史 thinking |
| `Ctrl+T` | 显示/隐藏完整 thinking（默认历史隐藏；流式时只在输入框上方 dock 显示一行预览，避免滚动抖动） |
| `Ctrl+R` | 反向搜索 prompt 历史 |
| `Ctrl+Up` | Agent 切换器（有子 agent 时） |
| `Ctrl+G` | 用 `$EDITOR` / `$VISUAL` 编辑草稿 |
| `Ctrl+S` | 暂存 / 取回草稿 |
| `Ctrl+L` | 重绘终端 |
| `PgUp` / `PgDown` / 滚轮 | 滚动 transcript |
| `Home` / `End` | 到 transcript 顶 / 底 |
| `@path` / `@path:42-60` | 路径提及，提交前展开文件上下文 |

会话、权限、状态等用 slash（输入 `/`）或 overlay 打开。

### Slash 命令

默认发现 / `/help` 顺序：

`/help` `/clear` `/resume` `/compact` `/status` `/context` `/memory` `/skills` `/undo` `/export` `/copy` `/retry` `/exit`

仍可执行但不在默认发现列表：`/model`、`/permissions`、`/diff`、`/history`、`/new`、`/sessions`（→ `/resume`）、`/session`、`/cost`、`/theme`、`/quit`、`/q`（→ `/exit`）。

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

Skills 目录：`~/.bondcode/skills` 与 `<project>/.bondcode/skills`（可选 `skills.root`）：

- **斜杠菜单**：`user-invocable` 的 skill 会出现在 `/` 补全里（`/name` 直接展开）。`disable-model-invocation: true` 的 user-only skill 仍可斜杠调用。
- **模型面**：只有 model-invocable skill 进入 Available Skills；模型通过 `skill` 工具调用。
- **`/skills`**：列出全部 skill 并标注 model/user-only 数量；长行在 transcript 中自动换行。

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

在 TUI 用 `/permissions [mode]` 切换，或在配置里写 `safety.permission_mode`。

| 模式 | 行为 |
|------|------|
| `default` | 标准策略 |
| `accept-edits` | 自动接受普通文件编辑 |
| `plan` | 阻止写/执行类工具，偏规划 |
| `bypass` | 仅当配置里 `safety.enable_bypass` 也为 true |

模式切换会先写入 session JSONL 再生效。

## 上下文、记忆、Todo（摘要）

- **上下文**：每轮完整性 + tool-result 微裁剪/落盘；阈值或 `/compact` 结构化 checkpoint；`prompt_too_long` 时 emergency shrink 后重试。  
- **记忆**：本地 memdir（`MEMORY.md` 索引 + 主题 `*.md`）；工具 `memory_save` / `memory_search`。不是向量库。  
- **Todo**：`todo_write` 整表替换；按 session 落在项目数据目录。

## Session 与 debug

从 shell 直接续聊，不必先进 TUI 再敲 `/resume`：

```powershell
bondcode --resume              # 打开 TUI 并进入会话选择器
bondcode --resume <session-id> # 直接打开该会话
```

日常会话也可在 TUI 内用 `/resume` / session manager overlay。可选 power-user CLI（`bondcode --help` 不展示，但仍可按名调用）：

```powershell
bondcode session list
bondcode session show <id>
bondcode session export <id> <path>
bondcode session import <id> <path>
bondcode session fork <src> <dst>
bondcode session delete <id>
bondcode session trace [id]           # 省略 id = 最近一次
bondcode session trace [id] --debug   # 叠加模型决策层
```

主入口决策 trace：`bondcode --debug` 或 `BONDCODE_DEBUG=1` → `<session-dir>/<id>.debug.jsonl`。

## MCP

仅 stdio server。优先用配置（`mcp.enabled` / `mcp.inject_tools` / `servers`）。可选隐藏 CLI：`bondcode mcp list|connect|disconnect|reload`。  
仅当 `mcp.enabled` 与 `mcp.inject_tools` 同时为真时注入工具，命名 `mcp__<server>__<tool>`。resources / prompts / subscriptions 不在当前范围。

## CLI 表面（产品）

| 入口 | 作用 |
|------|------|
| `bondcode` | 打开交互式 TUI（主路径） |
| `bondcode --resume` | 打开 TUI 并进入会话选择器 |
| `bondcode --resume <id>` | 直接打开该会话 |
| `bondcode config show\|example` | 查看配置 |
| `bondcode headless` | stdin/stdout JSON-line（嵌入 / 自动化） |
| 隐藏：`session`、`mcp`、slash 同名命令 | 调试 / 进阶 |

开发用：隐藏 flag `--fake`（固定假回复、无需 API Key，仅烟雾测试）。

## 能力边界

- **主路径**：ReAct 循环 + 核心工具 + 安全 + 上下文 + session 审计 + `task` + skills + 协作工具（默认开；`collaboration.enabled: false` 可关）  
- **配置开启**：MCP 工具注入；memory extract/dream 等环境开关  
- **模型 I/O**：内部 `llm.Client` + Anthropic-compatible HTTP/SSE  
- **Skills**：仅本地 `SKILL.md`（无远端市场）  
- **子 agent**：`task`（同步）+ 可选协作（`agent_task` / team / mailbox）。与主 agent 同一套 Policy/Confirmer  

## 配置指针

| 项 | 位置 |
|----|------|
| 完整 YAML 示例 | `configs/config.example.yaml` |
| 项目覆盖 | `bondcode.yaml`（本地） |
| 用户默认 | `~/.bondcode/config.yaml` |
| 状态根目录 | `~/.bondcode/projects/<编码cwd>/`（或 `BONDCODE_HOME`） |
