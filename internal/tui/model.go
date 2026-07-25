package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/ask"
)

type Model struct {
	cfg      Config
	layout   LayoutState
	live     LiveStatus
	timeline TimelineState
	composer ComposerState
	agent    AgentRunState
	mode     Mode
	verbose  bool
	accent   string
	// Display-density toggles (Phase 2.3). Loaded from preferences; flipped via
	// toggleThinking / toggleTimestamps / toggleToolDetails and the palette.
	showThinking       bool
	showTimestamps     bool
	showToolDetails    bool
	showScrollbar      bool
	usage              UsageView
	questioner         *Questioner
	question           *ask.Question
	questionCursor     int
	questionSelected   map[int]bool
	width              int
	height             int
	scroll             int
	scrollPaused       bool
	newOutputBelow     bool
	newOutputCount     int
	newOutputNotice    string
	markdownRenderer   *MarkdownRenderer
	markdownCache      map[string]markdownCacheEntry
	timelineLinesCache *timelineLinesCache
	agentIDsCache      *agentIDsCache
	prof               *profiler
	spinner            spinner.Model
	// Subagent observability: per-child traces keyed by taskID, plus the focus
	// state that drives the agent bar (Ctrl+↑) and the zoom-in agent window.
	focus                  Focus
	subagentTraces         map[string]*AgentTrace
	traceMembershipVersion uint64
	agentBarSelected       string
	focusedTaskID          string
	coordinatorDraft       string
	leaderPending          bool
	whichKeyVisible        bool
	search                 SearchState
	reverseHistory         reverseHistorySearchState
	// history backs the ctrl+h session-tree browser (exploratory backtracking).
	// It is pure local view state: browsing never mutates the running agent
	// (invariant 4), and a fork only happens on Enter while the agent is idle.
	history historyState

	// navTurnIdx is the turn the user last jumped to via alt+ctrl+p/n message
	// navigation (-1 = not navigating, follow the bottom). Kept separate from
	// scroll so manual scrolling does not reset the navigation cursor.
	navTurnIdx int

	// sessionHistory is the browser-style back/forward stack of visited session
	// ids; sessionHistIdx points at the current one. sessionScrolls remembers
	// each session's scroll offset so switching back restores the position.
	sessionHistory []string
	sessionHistIdx int
	sessionScrolls map[string]int

	// overlay is the active modal layer (command palette, list menus,
	// alert/confirm/prompt dialogs). At most one is active at a time; see
	// overlay.go. It is distinct from the agent-driven confirm/question/history
	// panels, which carry agent-loop response contracts.
	overlay overlayState
	// toasts are transient non-blocking notifications rendered in the top-right
	// corner; see toast.go.
	toasts []toast
	// stash holds parked composer drafts (<leader>p); persisted to
	// Config.StashPath. See prompt_stash.go.
	stash []string

	// lastTerminalTitle dedupes tea.SetWindowTitle emissions.
	lastTerminalTitle string
	// titleSpinnerFrame advances on spinner ticks for status-aware titles.
	titleSpinnerFrame int

	// hover is the live mouse-hover target (prompt glow, menu ▸, etc.).
	hover mouseHover
	// welcomeMenuActive is the highlighted cold-open menu row (click/hover).
	welcomeMenuActive int
	// animFrame advances on spinner ticks; only dock/live/toast/flash read it.
	animFrame int
	// animTickArmed is true while a spinner.Tick chain is in flight. Mouse
	// motion must not re-arm Tick while this is set — concurrent timers
	// amplify animFrame and make the thinking spinner race.
	animTickArmed bool
	// flash is a short click-feedback pulse (Grok-like "tap" response).
	flash uiFlash

	// scrollSel is the selected timeline entry index while FocusScrollback
	// (-1 = none). foldedEntries / expandedEntries are per-entry overrides
	// for non-tool blocks (reasoning defaults collapsed; left/right can expand).
	scrollSel       int
	foldedEntries   map[string]bool
	expandedEntries map[string]bool

	// mouseEnabled is the runtime mouse-tracking flag (config seed + /mouse
	// toggle). When false, the terminal owns drag-select/copy.
	mouseEnabled bool
	// scrollbarDragging is true while the user is click-dragging the
	// transcript right-edge scrollbar thumb/track.
	scrollbarDragging bool
}

func NewModel(cfg Config) Model {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	suggestions := NewSuggestionList(cfg.Commands)
	setDisplayProjectRoot(cfg.Status.ProjectRoot)
	composer := newComposerState(76, suggestions)
	if cfg.PromptHistoryPath != "" {
		composer.History = loadPromptHistory(cfg.PromptHistoryPath)
	}
	stash := loadStash(cfg.StashPath)
	prefs := loadTUIPreferences(cfg.PreferencesPath)
	setRenderVerbose(prefs.Verbose)
	// Coalesce display-density prefs: tool details default to ON (existing
	// behavior) unless the user explicitly hid them.
	showToolDetails := !prefs.HideToolDetails
	// Resolve the accent preset: a persisted /theme choice wins, then config,
	// then the default. ApplyAccent mutates the theme + rebuilds styles so the
	// very first render uses the chosen color.
	accentName := prefs.Accent
	if accentName == "" {
		accentName = cfg.Accent
	}
	if accentName == "" {
		accentName = DefaultAccentName()
	}
	ApplyAccent(ResolveAccentColor(accentName))

	// Create markdown renderer (ignore error, will fallback to plain text)
	markdownRenderer, _ := NewMarkdownRenderer(80)

	m := Model{
		cfg:                cfg,
		mode:               ModeNormal,
		verbose:            prefs.Verbose,
		accent:             accentName,
		showThinking:       prefs.ShowThinking,
		showTimestamps:     prefs.ShowTimestamps,
		showToolDetails:    showToolDetails,
		showScrollbar:      prefs.ShowScrollbar,
		questioner:         cfg.Questioner,
		composer:           composer,
		width:              80,
		height:             24,
		layout:             CalculateLayout(80, 24, composer.Input.Height()),
		live:               LiveStatusFromStatus(cfg.Status),
		timeline:           SeedTimeline(cfg.SeedHistory),
		markdownRenderer:   markdownRenderer,
		markdownCache:      map[string]markdownCacheEntry{},
		timelineLinesCache: &timelineLinesCache{},
		agentIDsCache:      &agentIDsCache{},
		prof:               newProfiler(),
		spinner:            spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		focus:              FocusComposer,
		subagentTraces:     map[string]*AgentTrace{},
		navTurnIdx:         -1,
		sessionHistory:     []string{cfg.Status.SessionID},
		sessionScrolls:     map[string]int{},
		stash:              stash,
		scrollSel:          -1,
		foldedEntries:      map[string]bool{},
		expandedEntries:    map[string]bool{},
		mouseEnabled:       cfg.MouseCapture,
		// Init schedules one spinner.Tick; mark the chain armed so mouse
		// motion cannot stack a second concurrent timer on cold start.
		animTickArmed: true,
	}
	// Seed title dedupe so the first agent event does not force a BatchMsg
	// solely to re-set the idle window title.
	m.lastTerminalTitle = m.terminalTitle()
	// Cold start: prompt owns focus and the cursor may blink.
	m, _ = m.applyComposerFocus()
	return m
}

func (m Model) Init() tea.Cmd {
	title := m.terminalTitle()
	m.lastTerminalTitle = title
	// Blink only while the prompt is focused; Blur'd textarea hides the cursor.
	cmds := []tea.Cmd{
		m.spinner.Tick,
		tea.SetWindowTitle(title),
	}
	if m.focus == FocusComposer {
		cmds = append(cmds, m.composer.Input.Focus())
	}
	// Mouse is enabled via ProgramOption in Run when cfg.MouseCapture is set.
	// Do not EnableMouse* from Init (bubbletea: use WithMouse* program opts).
	// Runtime /mouse and ctrl+shift+m use EnableMouseCellMotion / DisableMouse.
	return tea.Batch(cmds...)
}

// toggleMouseCapture flips runtime mouse tracking. Off restores native
// terminal drag-select/copy; on re-enables click/wheel for the TUI.
func (m Model) toggleMouseCapture() (Model, tea.Cmd) {
	m.mouseEnabled = !m.mouseEnabled
	if m.mouseEnabled {
		m = m.pushToast("mouse on · shift+drag still selects text", toastInfo)
		return m, tea.EnableMouseCellMotion
	}
	m.hover = mouseHover{}
	m.scrollbarDragging = false
	m = m.pushToast("mouse off · drag to select/copy", toastSuccess)
	return m, tea.DisableMouse
}

func (m Model) terminalTitle() string {
	project := projectName(m.cfg.Status.ProjectRoot)
	actionRequired := m.agent.Pending != nil || m.question != nil
	busy := m.agent.Busy || actionRequired
	activity := ""
	if busy {
		activity = m.currentAgentDetail()
		if d := strings.TrimSpace(m.agent.LiveDetail); d != "" && m.agent.Busy {
			activity = d
		}
	}
	// When action-required, composeTerminalTitle prefers that prefix over busy.
	return composeTerminalTitle(project, activity, m.agent.Busy, actionRequired, m.titleSpinnerFrame)
}

// maybeSetTerminalTitle returns a SetWindowTitle cmd when the composed title changed.
func (m Model) maybeSetTerminalTitle() (Model, tea.Cmd) {
	title := m.terminalTitle()
	if title == m.lastTerminalTitle {
		return m, nil
	}
	m.lastTerminalTitle = title
	return m, tea.SetWindowTitle(title)
}

// idleTerminalTitle is the stable title left on quit.
func (m Model) idleTerminalTitle() string {
	return composeTerminalTitle(projectName(m.cfg.Status.ProjectRoot), "", false, false, 0)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.prof.countMsg(msg)
	m = m.syncPendingQuestion()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg, tea.MouseMsg:
		return m.handleViewportMessage(msg)
	case tea.KeyMsg:
		return m.handleKeyMessage(msg)
	case runAgentMsg, agentEventMsg, agentDoneMsg, agentTickMsg, subagentInputResultMsg:
		return m.handleAgentMessage(msg)
	default:
		return m.handleMiscMessage(msg)
	}
}

func (m Model) cancelRunningAgent() Model {
	m = m.commitLiveStream()
	m.stopAgent()
	m.agent.Busy = false
	m.agent.Stream = nil
	m.agent.Pending = nil
	m.agent.LiveStream = nil
	m.agent.LiveDetail = ""
	m.agent.QueuedPrompts = nil
	m.question = nil
	m.timeline = m.timeline.MarkAgentEnded("cancelled", "", time.Now())
	m.agent.TerminalHandled = true
	return m
}

func (m Model) SetSize(width, height int) Model {
	if width > 0 {
		m.width = width
		// Update markdown renderer width
		if m.markdownRenderer != nil {
			_ = m.markdownRenderer.UpdateWidth(width - 4) // Leave some margin
		}
		m.invalidateMarkdownCache()
	}
	if height > 0 {
		m.height = height
	}
	m.composer.Input.SetWidth(max(20, m.width-4))
	m.composer = m.composer.syncHeight()
	m.layout = m.currentLayout()
	m = m.clampScroll(m.layout)
	return m
}

func (m Model) toggleVerbose() Model {
	// Tools show full paths / fuller details instead of shortened summaries.
	m.verbose = !m.verbose
	setRenderVerbose(m.verbose)
	m.invalidateMarkdownCache()
	return m.persistPreferences()
}

func (m Model) persistPreferences() Model {
	_ = saveTUIPreferences(m.cfg.PreferencesPath, tuiPreferences{
		Verbose:         m.verbose,
		Accent:          m.accent,
		ShowThinking:    m.showThinking,
		ShowTimestamps:  m.showTimestamps,
		HideToolDetails: !m.showToolDetails,
		ShowScrollbar:   m.showScrollbar,
	})
	return m
}
