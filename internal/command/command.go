package command

import (
	"context"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/memory"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/skill"
	"github.com/junnhwan/bond-code/internal/todo"
)

type StatusProvider interface {
	StatusSnapshot() app.RuntimeStatus
}

type Env struct {
	SessionID      string
	ProjectRoot    string
	PermissionMode string
	Model          string
	ToolCount      int
	ContextSummary string
	MemoryStore    *memory.MemoryStore
	MemoryMaxChars int
	SkillLoader    *skill.Loader
	SkillMaxChars  int
	TaskStore      *todo.TaskStore
	StatusProvider StatusProvider
	Sessions       *session.JSONLStore
	// SwitchSession, when set, hot-switches the running app onto an existing
	// session (reload its history, rebuild per-session context stores) so
	// subsequent turns append to it. Nil in headless / --once mode; slash commands
	// detect nil and report switching is unavailable there. It is a callback (not
	// an app reference) so the command package never imports app, avoiding a
	// cycle (chat.go injects it).
	SwitchSession func(sessionID string) error
	// NewSession, when set, switches the running app onto a fresh empty session
	// and returns its id. Nil in headless / --once mode.
	NewSession func() (sessionID string, err error)
	// SwitchModel, when set, swaps the active model without restarting (the
	// runtime behind TUI /model <name>): it rebuilds the LLM client for the new
	// model and pushes it onto the loop. Nil in headless / --once mode; slash
	// commands detect nil and report switching is unavailable there. Like
	// SwitchSession it is a callback (not an app reference) so this package never
	// imports app, avoiding a cycle (chat.go injects it).
	SwitchModel func(model string) error
	// ModelSuggestions lists model names surfaced as hints by /model (no arg),
	// typically the configured overload-fallback models. Informational only.
	ModelSuggestions []string
	// SetPermissionMode changes the shared runtime policy and audits the transition.
	SetPermissionMode func(mode string) error
}

type Result struct {
	Output string
	// Panel, when set, is a structured description of the output for terminals
	// that can render a bordered panel (e.g. /status). It carries only data; the
	// TUI owns the styling so command packages stay free of theme dependencies.
	// When nil the TUI falls back to the plain-text Output.
	Panel *Panel
	// SessionSwitched, when non-nil, signals the TUI that the app switched onto a
	// different session; the TUI reloads its timeline from the app's rebuilt
	// history instead of rendering Output. It carries only the session id (a
	// signal), never seed data, so the command package stays free of tui imports
	// — the TUI re-fetches the seed via Config.ReloadSessionSeed. This is the
	// only side-effect channel a slash command has beyond plain text output.
	SessionSwitched *string
	// ModelSwitched, when non-nil, signals the TUI that the active model changed
	// (e.g. /model <name>) so it can refresh the header without rebuilding the
	// timeline (unlike SessionSwitched). Carries only the new model name.
	ModelSwitched *string
	// PermissionModeChanged tells the TUI to refresh header/sidebar state.
	PermissionModeChanged *string
	// OpenSessionManager, when true, asks the TUI to open the interactive
	// session-manager overlay (switch / rename / pin / delete) instead of
	// rendering Output — the Claude-Code-style "/resume lists sessions you can
	// pick from" flow. Headless consumers ignore it and fall back to Output
	// (the text session list). Like the other signals it carries no data; the
	// TUI owns the overlay.
	OpenSessionManager bool
}

// Panel is a render-ready, theme-neutral description of a command's output as
// a titled, sectioned key/value panel. Used by commands like /status whose
// output reads best as a bordered box rather than prose.
type Panel struct {
	Title    string
	Sections []PanelSection
}

// PanelSection is a labelled group of rows within a panel.
type PanelSection struct {
	Label string // section heading, e.g. "CONTEXT"; empty omits the heading
	Rows  []PanelRow
}

// PanelRow is one key/value line. State tints the value in the TUI:
// "" default, "ok" success, "warn" warning, "error" error.
type PanelRow struct {
	Key   string
	Value string
	State string
}

type Command struct {
	Name        string
	Description string
	RemoteSafe  bool
	Run         func(ctx context.Context, env Env, args []string) (Result, error)
	// PromptTemplate, when non-empty, marks this as a prompt-injecting command:
	// the TUI substitutes $ARGUMENTS (and $1, $2, ...) into the template and
	// submits the result as a user prompt — reusing the normal agent path, so
	// it still flows through safety / contextx like any typed prompt — instead
	// of calling Run. Used by custom .bondcode/commands/*.md commands. Run may
	// be nil when this is set.
	PromptTemplate string
}
