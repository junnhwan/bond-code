package tool

// Canonical tool names. Centralizing them here prevents the drift class where a
// consumer (plan-mode disable list, protected-path hook, TUI renderer, subagent
// allowlist/denylist) spells a tool name differently from the tool's own Name()
// and silently breaks a safety or UX invariant — exactly the bug where the edit
// tool was named "edit" while plan mode / the .git hook / the renderer matched
// "edit_file", punching a hole in the read-only plan-mode boundary.
//
// Every Tool's Name() returns one of these, and every consumer that switches on
// a tool name references the same constant, so a rename is a single edit that
// the compiler propagates everywhere.
const (
	// Core builtins (always registered). Coding primitives only — git/go/project
	// inspection are done via run_command, not dedicated thin wrappers.
	ReadFile   = "read_file"
	WriteFile  = "write_file"
	EditFile   = "edit_file"
	ListDir    = "list_dir"
	SearchText = "search_text"
	RunCommand = "run_command"

	// Core runtime tools.
	AskUser      = "ask_user"
	MemorySearch = "memory_search"
	MemorySave   = "memory_save"
	TodoRead     = "todo_read"
	TodoWrite    = "todo_write"
	// Skill is the Claude Code-style single skill invocation tool.
	// Discovery is via dynamic skill listing (system-reminder), not skill_list.
	Skill = "skill"
	Task  = "task"
	Spawn = "spawn"
)
