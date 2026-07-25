package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/command"
)

// Action ties a human-readable title and category to a key shortcut and a Run
// transition. The command palette (and later which-key) enumerate Actions so the
// user can discover and trigger anything the TUI can do without memorizing
// bindings. Slash commands, leader actions, toggles, and navigation all register
// here, making the palette the single discovery surface for the whole TUI.
type Action struct {
	// ID is a stable dotted identifier ("session.new", "view.verbose"). It is
	// also used as a fuzzy fallback when the title does not match the query.
	ID string
	// Title is the primary, human-readable label shown in the palette.
	Title string
	// Category groups actions in the palette ("Command", "View", "Session").
	Category string
	// Shortcut is a display-only hint ("ctrl+o", "/status"). It is NOT used for
	// dispatch — the key bindings in model.go remain the source of truth — so a
	// stale string here cannot break a binding, only the hint.
	Shortcut string
	// Run executes the action. It is called AFTER the palette overlay has been
	// closed, so implementations see a clean base state and may open their own
	// follow-up overlay (confirm, prompt, …).
	Run func(m Model) (Model, tea.Cmd)
}

// buildActionList enumerates every palette-discoverable action for the current
// state. Slash commands come from the registry plus a small set of TUI-local
// commands; view toggles, navigation, and mode switches fill out the rest.
func buildActionList(m Model) []Action {
	actions := make([]Action, 0, 32)

	// Registry commands (builtin + custom .bondcode/commands templates).
	if m.cfg.Commands != nil {
		for _, cmd := range m.cfg.Commands.List() {
			actions = append(actions, slashRunnable(cmd.Name, cmd.Description, commandCategory(cmd)))
		}
	}

	// TUI-local slash commands that are not in the registry.
	actions = append(actions, tuiLocalSlashActions()...)

	// Non-slash actions: toggles, navigation, modes.
	actions = append(actions, viewActions(m)...)

	sortActions(actions)
	return actions
}

// commandCategory keeps custom prompt-template commands visually distinct from
// builtin ones in the palette.
func commandCategory(cmd command.Command) string {
	if strings.TrimSpace(cmd.PromptTemplate) != "" {
		return "Custom"
	}
	return "Command"
}

// slashRunnable builds an Action that runs a /command via runCommand. The title
// defaults to the command's description (falling back to the name) so the
// palette reads as prose.
func slashRunnable(name, desc, category string) Action {
	title := strings.TrimSpace(desc)
	if title == "" {
		title = name
	}
	nameCopy := name
	return Action{
		ID:       "slash." + name,
		Title:    title,
		Category: category,
		Shortcut: "/" + name,
		Run: func(m Model) (Model, tea.Cmd) {
			return m.runCommand(m.cfg.Context, "/"+nameCopy)
		},
	}
}

// tuiLocalSlashActions covers commands handled inside the TUI package rather
// than the registry: copy/theme (TUI-local render), retry and exit (handled at
// Submit time), and compact (TUI special-cases it to startCompact).
func tuiLocalSlashActions() []Action {
	return []Action{
		slashRunnable("copy", "Copy latest output to clipboard", "Command"),
		slashRunnable("mouse", "Toggle mouse capture (off = drag-select/copy)", "Command"),
		slashRunnable("theme", "Switch accent color", "Command"),
		slashRunnable("compact", "Summarize context to reclaim space", "Command"),
		{
			ID:       "slash.retry",
			Title:    "Retry latest failed turn",
			Category: "Command",
			Shortcut: "/retry",
			Run:      func(m Model) (Model, tea.Cmd) { return m.retryLatestFailedTurn() },
		},
		{
			ID:       "slash.exit",
			Title:    "Quit BondCode",
			Category: "App",
			Shortcut: "/exit",
			Run:      func(m Model) (Model, tea.Cmd) { return m, tea.Quit },
		},
	}
}

func directKeyShortcut(id string) string {
	descriptor, ok := command.LookupDirectKeyDescriptor(id)
	if !ok {
		return ""
	}
	return descriptor.DisplayShortcut
}

// viewActions are palette entries for keybindings that are not slash commands:
// display toggles, transcript navigation, and mode switches.
func viewActions(m Model) []Action {
	return []Action{
		{
			ID: "view.verbose", Title: "Toggle verbose tool output", Category: "View", Shortcut: directKeyShortcut("key.details"),
			Run: func(m Model) (Model, tea.Cmd) { return m.toggleVerbose(), nil },
		},
		{
			ID: "view.thinking", Title: "Toggle thinking blocks (folded / expanded)", Category: "View", Shortcut: "",
			Run: func(m Model) (Model, tea.Cmd) { return m.toggleThinking(), nil },
		},
		{
			ID: "prompt.stash", Title: "Stash / pop composer draft", Category: "Input", Shortcut: directKeyShortcut("key.stash"),
			Run: func(m Model) (Model, tea.Cmd) { return m.stashLeaderAction(), nil },
		},
		{
			ID: "prompt.editor", Title: "Edit draft in $EDITOR", Category: "Input", Shortcut: directKeyShortcut("key.external-editor"),
			Run: func(m Model) (Model, tea.Cmd) { return m.openExternalEditor() },
		},
		{
			ID: "view.timestamps", Title: "Toggle turn timestamps", Category: "View",
			Run: func(m Model) (Model, tea.Cmd) { return m.toggleTimestamps(), nil },
		},
		{
			ID: "view.tooldetails", Title: "Toggle completed tool detail lines", Category: "View",
			Run: func(m Model) (Model, tea.Cmd) { return m.toggleToolDetails(), nil },
		},
		{
			ID: "view.scrollbar", Title: "Toggle transcript scrollbar", Category: "View",
			Run: func(m Model) (Model, tea.Cmd) { return m.toggleScrollbar(), nil },
		},
		{
			ID: "view.diff", Title: "Review session file changes (diff viewer)", Category: "View", Shortcut: "",
			Run: func(m Model) (Model, tea.Cmd) { return m.openDiffViewer(), nil },
		},
		{
			ID: "view.plan", Title: "Toggle plan mode (read-only)", Category: "View", Shortcut: directKeyShortcut("key.mode-cycle"),
			Run: func(m Model) (Model, tea.Cmd) { return m.cycleMode(), nil },
		},
		{
			ID: "view.search", Title: "Search transcript", Category: "View", Shortcut: "",
			Run: func(m Model) (Model, tea.Cmd) { return m.startTranscriptSearch(), nil },
		},
		{
			ID: "view.history", Title: "Browse session timeline (fork-resume)", Category: "Session", Shortcut: "",
			Run: func(m Model) (Model, tea.Cmd) {
				if m.inputValue() != "" || m.agent.Pending != nil || m.question != nil || m.cfg.SessionHistory == nil {
					return m, nil
				}
				return m.enterHistory(), nil
			},
		},
		{
			ID: "session.manage", Title: "Manage sessions (switch / rename / pin / delete)", Category: "Session", Shortcut: "",
			Run: func(m Model) (Model, tea.Cmd) {
				if m.cfg.SessionManager == nil {
					return m.runCommand(m.cfg.Context, "/resume")
				}
				return m.openSessionManager(), nil
			},
		},
		{
			ID: "view.back", Title: "Back to previous session", Category: "Session", Shortcut: "",
			Run: func(m Model) (Model, tea.Cmd) { return m.navigateSession(-1) },
		},
		{
			ID: "view.forward", Title: "Forward to next session", Category: "Session", Shortcut: "",
			Run: func(m Model) (Model, tea.Cmd) { return m.navigateSession(+1) },
		},
		{
			ID: "view.theme", Title: "List accent color presets", Category: "View", Shortcut: "/theme",
			Run: func(m Model) (Model, tea.Cmd) { return m.runCommand(m.cfg.Context, "/theme") },
		},
	}
}

// sortActions orders actions by category then title so the unfiltered palette
// is predictable and rescans the same way each time it opens.
func sortActions(actions []Action) {
	sort.SliceStable(actions, func(i, j int) bool {
		if actions[i].Category != actions[j].Category {
			return actions[i].Category < actions[j].Category
		}
		return actions[i].Title < actions[j].Title
	})
}
