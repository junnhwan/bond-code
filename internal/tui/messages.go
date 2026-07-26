package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/session"
)

type ToolStatus string

const (
	ToolPending  ToolStatus = "pending"
	ToolRunning  ToolStatus = "running"
	ToolDone     ToolStatus = "done"
	ToolFailed   ToolStatus = "failed"
	ToolRejected ToolStatus = "rejected"
	ToolBlocked  ToolStatus = "blocked"
)

func isTerminalToolStatus(status ToolStatus) bool {
	switch status {
	case ToolDone, ToolFailed, ToolRejected, ToolBlocked:
		return true
	default:
		return false
	}
}

type ToolBlock struct {
	ID        string
	Name      string
	Status    ToolStatus
	Risk      string
	Input     string
	Output    string
	Error     string
	Summary   string
	Collapsed bool
	StartedAt time.Time
	EndedAt   time.Time
	Duration  time.Duration
}

// TaskView is a neutral, todo-package-free view of one checklist item for the
// TUI chrome (prompt chip, refreshStatus), so the TUI need not import
// internal/todo just to surface progress.
type TaskView struct {
	ID         string
	Subject    string
	Status     string // pending / in_progress / completed
	Owner      string
	ActiveForm string // present-continuous label while in_progress
}

type TeamMemberView struct {
	ID             string
	Name           string
	Role           string
	State          string
	Backend        string
	PermissionMode string
	TaskID         string
	Unread         int
}
type TeamView struct {
	ID      string
	Name    string
	State   string
	Unread  int
	Members []TeamMemberView
}

type Status struct {
	SessionID       string
	ProjectRoot     string
	Model           string
	PermissionMode  string
	ToolCount       int
	GitBranch       string
	ContextSummary  string
	MemorySummary   string
	PlanningSummary string
	SubagentSummary string
	MCPStatus       string
	// Tasks carries the structured session todo list for the prompt chip and
	// live status refresh. Empty => no todo chrome.
	Tasks []TaskView
	// ContextBreakdown is the token composition (system / conversation / tool
	// results / summary) shown as a proportional bar in /context. Zero when no
	// breakdown is available.
	ContextBreakdown ContextBreakdownView
	// Usage carries the cumulative model token usage surfaced live in the
	// footer (Phase 3.3). Mirrors app.Usage; zero when not measured yet.
	Usage UsageView

	// Teams carries durable collaboration membership and mailbox activity.
	Teams []TeamView
}

// UsageView is the neutral TUI view of cumulative model usage, mirrored from
// app.Usage so the footer can show a live cost indicator without importing app.
type UsageView struct {
	ModelCalls        int
	TotalInputTokens  int
	TotalOutputTokens int
}

type ChatRunner interface {
	RunWithEvents(ctx context.Context, prompt string, sink agent.EventSink) (*agent.RunResult, error)
	// Compact summarizes the conversation history via the model; progress is
	// streamed through sink as EventCompactionStarted/Finished. Used by /compact.
	Compact(ctx context.Context, sink agent.EventSink) error
}

type Config struct {
	Context    context.Context
	Status     Status
	Commands   *command.Registry
	CommandEnv command.Env
	Chat       ChatRunner
	Confirmer  *Confirmer
	Questioner *Questioner
	// PlanMode, when set, is toggled by shift+tab to switch the agent between
	// normal execution and read-only planning. May be nil (mode switching then
	// only affects the UI badge, not the agent).
	PlanMode PlanModeController
	// SessionHistory, when set, powers the ctrl+h session-tree browser
	// (exploratory backtracking). May be nil (ctrl+h then does nothing).
	// app.App implements it via a cli-layer adapter; test models inject a fake.
	SessionHistory SessionHistoryController
	// SessionManager, when set, powers the session-manager overlay and quick
	// switch (<leader>1..9): list, rename, pin, delete, switch. May be nil
	// (those features are then hidden); app.App implements it via a cli adapter.
	SessionManager SessionManagerController
	// OpenSessionManagerOnStart opens the session-manager overlay on cold start
	// (bare `bondcode --resume`). Requires SessionManager; ignored when unset.
	OpenSessionManagerOnStart bool
	// SeedHistory pre-populates the timeline when resuming a session. It is a
	// neutral role/content view so the TUI does not import the llm package.
	SeedHistory []SeedMessage
	// ReloadSessionSeed, when set, is invoked after a slash command signals a
	// session switch (Result.SessionSwitched) so the TUI can rebuild its timeline
	// from the app's freshly-switched history. It mirrors sessionHistoryAdapter:
	// the app has already swapped its history onto the target session, so this
	// just re-projects it into the neutral SeedMessage view. Nil in headless mode.
	ReloadSessionSeed func(sessionID string) []SeedMessage
	// MouseCapture enables mouse tracking for wheel scroll / click focus.
	// Default true (wheel works). cli resolves tui.mouse_capture; /mouse toggles.
	MouseCapture bool
	// PromptHistoryPath, when set, persists safe composer prompt history across
	// TUI sessions. Unsafe prompts are filtered by the same in-memory history
	// policy before they ever reach disk.
	PromptHistoryPath string
	// StashPath, when set, persists stashed composer drafts across TUI sessions
	// so a parked prompt survives a restart. See prompt_stash.go.
	StashPath string
	// PreferencesPath, when set, persists local TUI view preferences such as
	// tool detail density and rail display mode across TUI sessions.
	PreferencesPath string
	// Accent optionally names an accent-color preset resolved at startup
	// (peach/blue/green/amber/magenta/cyan). A persisted /theme choice overrides
	// it; empty falls back to the default (peach).
	Accent string
	// RefreshStatus, when set, re-fetches context-window metadata (breakdown +
	// summary) after compaction and at turn end, so the live's composition
	// reflects the live window instead of the startup snapshot. Only the
	// context fields need to be populated; the TUI merges them into the live
	// without touching other state. Nil in headless/test models.
	RefreshStatus func() Status
	// CancelSubagent, when set, cancels one running child agent by task ID
	// (best-effort). Drives the per-child cancel from the agent bar. Nil in
	// headless/test models => the key is a no-op.
	CancelSubagent func(taskID string) bool
	// SendSubagentInput steers a running child while its transcript is open.
	SendSubagentInput func(taskID, input string) error
	// RuleSource, when set, backs the "Allow always" choice at permission
	// prompts (Phase 5A): picking Always persists a session allow rule via
	// safety.PatternKey so future same-kind calls auto-approve without a prompt.
	// nil => the Always option is hidden (only Allow once / Reject offered).
	// app.App injects the session's *session.RuleSource; test models leave it nil.
	RuleSource *session.RuleSource
}

// PlanModeController toggles the agent's read-only planning posture. The TUI
// calls it when the user cycles modes; app.App implements it. It is a separate
// optional interface from ChatRunner so test runners need not implement it.
type PlanModeController interface {
	SetPlanMode(plan bool)
}

// SessionHistoryController backs the session-tree browser: it loads the event
// tree of a session and forks it at a historical node so the agent resumes on a
// fresh branch. It returns session-package tree types because the tree
// algorithms (BuildTree/PathTo/BranchSummary) live in that package and the TUI
// reuses them directly; ResumeFromEvent returns the neutral SeedMessage view so
// the TUI never imports llm. app.App implements it through a cli-layer adapter.
type SessionHistoryController interface {
	// LoadEvents returns the flat event list for a session, used to rebuild the
	// tree for browsing.
	LoadEvents(sessionID string) ([]session.Event, error)
	// ResumeFromEvent forks the session at eventID onto a new branch (original
	// untouched) and returns the new session id plus a neutral seed view of the
	// rebuilt messages so the TUI can reset its timeline onto the new branch.
	ResumeFromEvent(sessionID, eventID string) (newSessionID string, seed []SeedMessage, err error)
}

// SessionInfo is the manager's neutral view of one session: enough to list,
// identify, and act on it without coupling the TUI to the session store.
type SessionInfo struct {
	ID         string
	Title      string // custom title if set, else the first-message preview
	Pinned     bool
	Active     bool
	Messages   int
	LastActive time.Time
}

// SessionManagerController backs the session-manager overlay and quick switch:
// list sessions with derived metadata, rename / pin / delete. Switching itself
// is handled by the existing switchToSession path (CommandEnv.SwitchSession),
// so the controller owns data + metadata mutations only.
type SessionManagerController interface {
	List() ([]SessionInfo, error)
	Delete(id string) error
	SetTitle(id, title string) error
	SetPinned(id string, pinned bool) error
}

// SeedMessage is a neutral view of one resumed conversation message, used to
// seed the timeline without coupling the TUI to the llm package.
type SeedMessage struct {
	Role     string // "user" | "assistant" | "tool"
	Content  string
	ToolName string // meaningful for tool messages
}

type agentEventMsg struct {
	event         agent.Event
	runGeneration uint64
}

type agentDoneMsg struct {
	err           error
	runGeneration uint64
}

type runAgentMsg struct {
	prompt string
}

type agentTickMsg struct {
	runGeneration uint64
}

func waitForAgentEvent(stream <-chan tea.Msg, runGeneration uint64) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-stream:
			if !ok {
				return agentDoneMsg{runGeneration: runGeneration}
			}
			return msg
		case <-time.After(time.Second):
			return agentTickMsg{runGeneration: runGeneration}
		}
	}
}

type subagentInputResultMsg struct {
	taskID  string
	blockID string
	input   string
	err     error
}
