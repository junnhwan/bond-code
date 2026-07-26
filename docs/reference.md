# BondCode reference

[English](reference.md) | [中文](reference.zh-CN.md)

Usage and capability cheatsheet. UI walkthrough belongs in the README (screenshots / GIFs). Runtime truth for “what is registered” is `internal/app/bootstrap.go` + `bootstrap_tools.go`.

## TUI (short)

`bondcode` opens the TUI by default.

Layout: **transcript** → **turn status** (when busy) → **`❯` prompt** (model · mode · ctx · permission) → **shortcuts bar**. Sessions, permissions, status, diff, MCP, skills open as overlays or slash panels—no permanent sidebar.

### Keys

| Key | Action |
|-----|--------|
| `Enter` | Send (queues while agent is busy) |
| `Ctrl+J` / `Alt+Enter` / `Shift+Enter` | Newline (`Ctrl+J` is the reliable Windows path; Shift+Enter is often unavailable) |
| `Tab` | Toggle focus: prompt ↔ scrollback |
| `Space` | From scrollback (empty draft) → prompt |
| `Esc` | Close overlay / cancel run / clear draft / leave scrollback |
| `Ctrl+C` | Interrupt; if idle, clear draft or quit |
| `Ctrl+D` | Scrollback: half-page down; empty composer: quit |
| `Ctrl+U` | Scrollback: half-page up |
| `Shift+Tab` / `Alt+M` | Toggle normal / plan mode |
| `Ctrl+O` | Expand/compact tool details (paths, output). Does not show historical thinking |
| `Ctrl+T` | Show/hide full thinking text (default: history hidden; live thinking is one fixed dock line above the prompt) |
| `Ctrl+R` | Reverse-search prompt history |
| `Ctrl+Up` | Agent switcher (when sub-agents exist) |
| `Ctrl+G` | Edit draft in `$EDITOR` / `$VISUAL` |
| `Ctrl+S` | Stash / restore draft |
| `Ctrl+L` | Redraw |
| `PgUp` / `PgDown` / wheel | Scroll transcript |
| `Home` / `End` | Top / bottom of transcript |
| `@path` / `@path:42-60` | Path mention + context expand on send |

Use slash commands (type `/`) or overlays for sessions, permissions, status, and similar actions.

### Slash commands

Default discovery / `/help` order:

`/help` `/clear` `/resume` `/compact` `/status` `/context` `/memory` `/skills` `/undo` `/export` `/copy` `/retry` `/exit`

Still runnable but hidden from default discovery: `/model`, `/permissions`, `/diff`, `/history`, `/new`, `/sessions` (→ `/resume`), `/session`, `/cost`, `/theme`, `/quit`, `/q` (→ `/exit`).

## Built-in tools

Coding surface stays small: read/edit/search/shell. Git, tests, formatters go through `run_command`.

### Core (always registered when enabled by defaults)

| Tool | Role | Typical risk |
|------|------|----------------|
| `read_file` | Read file | low |
| `write_file` | Write file | medium |
| `edit_file` | Patch existing file | medium |
| `list_dir` | List directory | low |
| `search_text` | Search text | low |
| `run_command` | Local shell (git, go test, …) | by command |
| `ask_user` | Ask the user | low |
| `memory_search` | Search memdir topics | low |
| `memory_save` | Save topic + MEMORY.md index | medium |
| `todo_read` | Read session todos | low |
| `todo_write` | Replace session todo list | medium |
| `task` | Sync subagent (Claude Code–style Task) | medium |
| `skill` | Expand a local skill by name | low |

### Extended (config-gated)

| Tools | When registered |
|-------|-----------------|
| `skill` | `skills.enabled` (default on) |
| `task` | `subagent.enabled` (default on) |
| `agent_task` / `task_*` / `task_backend` | `collaboration.enabled` (default on) |
| `team_*` | `collaboration.enabled` (default on) |
| `mailbox_*` | `collaboration.enabled` (default on) |
| `mcp__<server>__<tool>` | `mcp.enabled` **and** `mcp.inject_tools` |

Skills load from `~/.bondcode/skills` and `<project>/.bondcode/skills` (optional `skills.root`):

- **Slash menu**: user-invocable skills appear as `/name` (type `/` to list). User-only skills (`disable-model-invocation: true`) are still slash-runnable.
- **Model surface**: only model-invocable skills are listed under Available Skills; the model uses the `skill` tool.
- **`/skills`**: lists all skills with model/user-only counts; long lines wrap in the transcript.

### Delegation

| Goal | Use |
|------|-----|
| One bounded sync subtask | `task` |
| A few independent subtasks | multiple `task`, or `tasks[]` + `mode=parallel` |
| Pipeline A → B | `mode=chain` |
| Continue a prior subagent | `task` + `resume_task_id` |
| Long-lived / team collaboration | `agent_task` + team / mailbox |

### File tools (read-before-write)

`write_file` / `edit_file` on an existing file require a prior successful `read_file` whose bytes still match. After session switch or `/undo`, read again before mutating. Shell/MCP are outside this boundary.

## Safety

Every real tool execution goes through `safety.Policy` + confirmer:

| Level | Meaning |
|-------|---------|
| low | Mostly read-only |
| medium | Controlled writes / tests / external tools |
| high | Destructive / force-push-class / risky network |
| blocked | Hard deny (e.g. `rm -rf /`, `git push --force`, `curl \| sh`) |

- `--yes` auto-approves **low / medium** only.  
- **high** still needs explicit confirm.  
- **blocked** never runs (not bypassed by mode, `--yes`, or subagents).

### Permission modes

Switch in the TUI with `/permissions [mode]`, or set `safety.permission_mode` in config.

| Mode | Behavior |
|------|----------|
| `default` | Standard policy |
| `accept-edits` | Auto-accept ordinary file edits |
| `plan` | Block write/exec-class tools; plan-oriented |
| `bypass` | Only if `safety.enable_bypass` is also true in config |

Mode changes are recorded on the session JSONL before taking effect.

## Context, memory, todos (summary)

- **Context**: per-turn integrity + tool-result micro-trim/spill; threshold/manual `/compact` with structured checkpoint; `prompt_too_long` emergency shrink + retry.  
- **Memory**: local memdir (`MEMORY.md` index + topic `*.md`); tools `memory_save` / `memory_search`. Not a vector DB.  
- **Todos**: `todo_write` replaces the whole list; stored per session under the project data dir.

## Sessions & debug

Resume from the shell without typing `/resume` first:

```powershell
bondcode --resume              # open TUI on the session picker
bondcode --resume <session-id> # open TUI already on that conversation
```

Day-to-day session work is also in the TUI (`/resume`, session manager overlay). Optional power-user CLI (hidden from `bondcode --help`, still invokable by name):

```powershell
bondcode session list
bondcode session show <id>
bondcode session export <id> <path>
bondcode session import <id> <path>
bondcode session fork <src> <dst>
bondcode session delete <id>
bondcode session trace [id]           # recent if omitted
bondcode session trace [id] --debug   # + model decision layer
```

Opt-in decision trace on the main entry: `bondcode --debug` or `BONDCODE_DEBUG=1` → `<session-dir>/<id>.debug.jsonl`.

## MCP

stdio servers only. Prefer config (`mcp.enabled` / `mcp.inject_tools` / `servers`). Optional hidden CLI: `bondcode mcp list|connect|disconnect|reload`.  
Tools inject only when both `mcp.enabled` and `mcp.inject_tools` are true, as `mcp__<server>__<tool>`. Resources/prompts/subscriptions are out of scope.

## CLI surface (product)

| Entry | Role |
|-------|------|
| `bondcode` | Open the interactive TUI (main product path) |
| `bondcode --resume` | Open TUI on the session picker |
| `bondcode --resume <id>` | Open TUI already on that conversation |
| `bondcode config show\|example` | Inspect config |
| `bondcode headless` | JSON-line stdin/stdout driver (embedding / automation) |
| Hidden: `session`, `mcp`, slash-equivalent names | Power-user / debug |

Developer-only: hidden `--fake` (fixed local reply, no API key) for smoke tests.

## Capability boundaries

- **Main path**: ReAct loop + core tools + safety + context + session audit + `task` + skills + collaboration tools (default on; set `collaboration.enabled: false` to drop).  
- **Config-on**: MCP tool injection; memory extract/dream env flags.  
- **Model I/O**: internal `llm.Client` + Anthropic-compatible HTTP/SSE.  
- **Skills**: local `SKILL.md` only (no remote marketplace).  
- **Subagents**: `task` (sync) + optional collaboration (`agent_task` / team / mailbox). Same Policy/Confirmer as the parent.

## Config pointers

| Item | Where |
|------|--------|
| Example full YAML | `configs/config.example.yaml` |
| Project override | `bondcode.yaml` (local) |
| User default | `~/.bondcode/config.yaml` |
| State root | `~/.bondcode/projects/<encoded-cwd>/` (or `BONDCODE_HOME`) |
