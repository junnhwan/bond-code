# BondCode

[English](README.md) | [中文](README.zh-CN.md)

A minimal coding agent written in Go.

BondCode is a lightweight coding-agent runtime: an LLM drives tools (read/edit/run) through a safety layer, with a terminal UI for interactive work. The model API is Anthropic-compatible HTTP/SSE.

## Quick start

```powershell
go install ./cmd/bondcode
bondcode
```

Or during development:

```powershell
go run ./cmd/bondcode
go test ./...
```

Configure the model (PowerShell):

```powershell
$env:BONDCODE_BASE_URL="https://your-endpoint/api/anthropic"
$env:BONDCODE_API_KEY="..."
$env:BONDCODE_MODEL="your-model"
```

| Variable | Purpose |
|----------|---------|
| `BONDCODE_BASE_URL` | Anthropic-compatible endpoint |
| `BONDCODE_API_KEY` | API key |
| `BONDCODE_MODEL` | Model name |

Optional project config: copy fields from [`configs/config.example.yaml`](configs/config.example.yaml) into a local `bondcode.yaml` (not committed). Lookup order: `--config` → `bondcode.yaml` → `~/.bondcode/config.yaml`.

Session data defaults to `~/.bondcode/projects/<encoded-cwd>/`.

## Screenshots & demos

### Welcome

![Welcome](docs/images/welcome.png)

### Demo

<video src="docs/images/demo.mp4" controls width="100%"></video>

[Demo video (mp4)](docs/images/demo.mp4)

## What it does

- **ReAct loop** — model chooses tools → policy/confirm → execute → structured results back to the model
- **Coding tools** — files, search, shell; git/tests via `run_command`
- **Safety** — risk levels; `--yes` only auto-approves low/medium; high still needs confirm; blocked never runs
- **TUI** — single-column terminal workspace (details in the reference)
- **Optional** — memory, todos, skills, subagent `task`, multi-agent collaboration, MCP (see config)

## Docs

- **Reference:** [English](docs/reference.md) · [中文](docs/reference.zh-CN.md) — keys, slash commands, tools, safety, capability boundaries
- **[configs/config.example.yaml](configs/config.example.yaml)** — full config field list

## License

See repository metadata / license file if present.
