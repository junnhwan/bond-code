package tui

// Mode is the agent interaction mode the TUI is in. It is local UI state
// toggled with shift+tab (normal <-> plan); it does not come from the app
// config. Plan mode is the "read-only planning" posture: the agent researches,
// asks clarifying questions, and emits a plan rather than editing files.
type Mode string

const (
	ModeNormal  Mode = "normal"
	ModePlan    Mode = "plan"
	ModeHistory Mode = "history"
)

// Toggle flips between normal and plan. shift+tab cycles these; keep this a
// pure two-state toggle so the keybinding is predictable. ModeHistory is
// deliberately excluded: it is entered via ctrl+h and exited via Esc/u, so the
// shift+tab cycle stays a predictable two-state normal<->plan switch.
func (m Mode) Toggle() Mode {
	if m == ModePlan {
		return ModeNormal
	}
	return ModePlan
}

// Label is the short human-readable mode name shown in the header/live.
func (m Mode) Label() string {
	switch m {
	case ModePlan:
		return "plan"
	case ModeHistory:
		return "history"
	default:
		return "normal"
	}
}

// IsPlan reports whether the TUI is in plan mode.
func (m Mode) IsPlan() bool {
	return m == ModePlan
}

// IsHistory reports whether the TUI is in the session-tree history browser.
func (m Mode) IsHistory() bool {
	return m == ModeHistory
}
