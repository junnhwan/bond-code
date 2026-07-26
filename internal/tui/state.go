package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
)

type LayoutState struct {
	Width     int
	Height    int
	HeaderH   int
	TimelineW int
	TimelineH int
	ComposerH int
	FooterH   int
}

// ContextBreakdownView is the neutral TUI view of the context window's token
// composition, mirrored from app.ContextBreakdown so the TUI need not import
// the app package. Rendered in /context and related panels.
type ContextBreakdownView struct {
	System       int
	Conversation int
	ToolResult   int
	Summary      int
}

type ComposerState struct {
	Input        textarea.Model
	History      []string
	HistoryIndex int
	HistoryDraft string
	Suggestions  *SuggestionList
	// Pastes holds collapsed multi-line paste payloads (Phase 5C.1): the
	// textarea shows a [Pasted ~N lines] chip instead of the raw text so a huge
	// paste cannot push the composer to its max height; Submit re-expands them
	// and joins them onto the typed prompt.
	Pastes []PasteEntry

	// RawPasteCandidateAt and the burst fields track non-bracketed input.
	// Windows console input exposes a paste as individual rune/Enter events, so
	// a sufficiently fast rune burst followed by Enter is draft data, not submit.
	RawPasteCandidateAt    time.Time
	RawPasteBurstStartedAt time.Time
	RawPasteBurstRunes     int
	RawPasteActive         bool
}

// PasteEntry is one collapsed paste: a short display marker shown as a chip
// above the composer, plus the original text re-expanded into the prompt on
// submit.
type PasteEntry struct {
	Marker string
	Text   string
}

type streamBodyBuffer struct {
	body    string
	builder strings.Builder
}

func newStreamBodyBuffer(body string) *streamBodyBuffer {
	buffer := &streamBodyBuffer{}
	buffer.builder.Grow(len(body))
	_, _ = buffer.builder.WriteString(body)
	buffer.body = buffer.builder.String()
	return buffer
}

func (b *streamBodyBuffer) matches(body string) bool {
	return b != nil && b.body == body
}

func (b *streamBodyBuffer) append(chunk string) string {
	_, _ = b.builder.WriteString(chunk)
	b.body = b.builder.String()
	return b.body
}

type liveStreamState struct {
	kind       BlockKind
	body       string
	visibleLen int
	buffer     *streamBodyBuffer
	generation uint64
}

func (s *liveStreamState) visibleBody() string {
	if s == nil || s.visibleLen <= 0 {
		return ""
	}
	visibleLen := min(s.visibleLen, len(s.body))
	return s.body[:visibleLen]
}

type AgentRunState struct {
	Busy    bool
	Err     error
	Stream  <-chan tea.Msg
	Cancel  context.CancelFunc
	Pending *agent.Event
	// ConfirmChoice is the user's current selection at a permission prompt
	// (Phase 5A): once / always / reject. choiceAlways is reachable only at
	// non-high risk when Config.RuleSource is set; high-risk cycles once/reject.
	// Reset to choiceOnce whenever a new prompt appears (adapter).
	ConfirmChoice confirmChoice
	// ConfirmRejectReason is the in-progress text when the user has picked
	// reject and is typing an optional reason to feed back to the model.
	ConfirmRejectReason string
	// ConfirmEnteringReject is true while the reject-reason input is focused.
	ConfirmEnteringReject bool
	// LiveStream is the only owner of in-progress assistant or reasoning text.
	// Its buffer is pointer-backed for amortized appends; appendLiveChunk clones
	// it whenever a value-copied Model branch no longer matches the body snapshot.
	LiveStream     *liveStreamState
	LiveGeneration uint64
	LiveDetail     string
	// RunGeneration and TerminalHandled are lifecycle fields used by the later
	// terminal-message work. Tasks 1-3 only establish their state ownership.
	RunGeneration   uint64
	TerminalHandled bool
	// ContextTokens / ContextMaxTokens are the most recent context-window usage
	// reported via EventContextUpdated; 0 means no report yet.
	ContextTokens    int
	ContextMaxTokens int
	// MeasuredTokens is the real input-token count the model reports via
	// EventContextMeasured; the header prefers it over ContextTokens (a chars/3
	// estimate) so ctx % matches /status. 0 until the first reply.
	MeasuredTokens int
	// QueuedPrompts holds prompts submitted while the agent was busy; they run
	// one after another as each turn finishes instead of being rejected.
	QueuedPrompts []string
}

type ToolFocusState struct {
	LatestToolID string
	ExpandedAll  bool
}

type SearchState struct {
	Active     bool
	Query      string
	MatchIndex int
}

// AgentTrace captures one child agent's (subagent or orchestrator node)
// observable execution: its prompt, tool-call stream rendered as Blocks, status
// and final answer. Populated from EventSubagent* events so the TUI can render
// the child's run inside its own window instead of folding it to one line.
type AgentTrace struct {
	TaskID      string
	Title       string
	AgentType   string
	Status      string // running / completed / failed / cancelled
	Prompt      string
	Blocks      []Block
	FinalAnswer string
	Draft       string
	Unread      bool
	Generation  uint64
	LiveStream  *liveStreamState
	// StartedAt / EndedAt drive elapsed display in the agent switcher and window.
	// EndedAt is set on a terminal status (completed/failed/cancelled).
	StartedAt time.Time
	EndedAt   time.Time
}

// Focus identifies which region of the TUI owns keyboard input. The composer is
// the default; the agent bar (Ctrl+↑) and an open agent window are the other
// two focus targets for subagent observability.
type Focus string

const (
	FocusComposer    Focus = "composer"
	FocusScrollback  Focus = "scrollback"
	FocusAgentBar    Focus = "agentBar"
	FocusAgentWindow Focus = "agentWindow"
)

// upsertToolBlock folds one child-agent tool call into the trace. A "running"
// call appends a new block; a "done"/"failed" call updates the most recent
// running block with the same tool name (child agents run tools serially in
// practice, so this pairs a call with its completion without needing a tool
// call id on the event).
func (trace *AgentTrace) upsertToolBlock(event agent.Event) {
	status := ToolDone
	switch event.Message {
	case "running":
		status = ToolRunning
	case "failed":
		status = ToolFailed
	}
	if event.Error != "" {
		status = ToolFailed
	}
	tool := &ToolBlock{
		Name:   event.ToolName,
		Status: status,
		Input:  event.Input,
		Output: event.Output,
		Error:  event.Error,
	}
	if status == ToolRunning {
		trace.Blocks = append(trace.Blocks, toolBlockToBlock(tool, trace.nextBlockID()))
		return
	}
	for i := len(trace.Blocks) - 1; i >= 0; i-- {
		b := trace.Blocks[i]
		if b.Tool != nil && b.Tool.Name == event.ToolName && b.Tool.Status == ToolRunning {
			merged := mergeToolBlock(b.Tool, tool)
			trace.Blocks[i] = toolBlockToBlock(merged, b.ID)
			return
		}
	}
	trace.Blocks = append(trace.Blocks, toolBlockToBlock(tool, trace.nextBlockID()))
}

func (trace *AgentTrace) nextBlockID() string {
	return fmt.Sprintf("%s-trace-%d", trace.TaskID, len(trace.Blocks)+1)
}

// markEnded stamps EndedAt (and StartedAt if missing) when a child reaches a
// terminal status so the switcher/window can show a stable elapsed duration.
func (trace *AgentTrace) markEnded(event agent.Event) {
	if trace == nil {
		return
	}
	ended := eventTime(event)
	if ended.IsZero() {
		ended = time.Now()
	}
	if trace.StartedAt.IsZero() {
		trace.StartedAt = ended
	}
	if trace.EndedAt.IsZero() {
		trace.EndedAt = ended
	}
}

// toolCount returns how many tool blocks the child executed (running or done).
func (trace *AgentTrace) toolCount() int {
	if trace == nil {
		return 0
	}
	n := 0
	for _, b := range trace.Blocks {
		if b.Tool != nil {
			n++
		}
	}
	return n
}

// isEmptyCompletion is true when a child finished without executing any tools.
// That usually means the model returned a plan/text only — useful to surface so
// the parent/user does not treat status=completed as "work landed on disk".
func (trace *AgentTrace) isEmptyCompletion() bool {
	if trace == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(trace.Status)) {
	case "completed", "cancelled":
		return trace.toolCount() == 0
	default:
		return false
	}
}

// elapsed returns wall time for display. Running children use Now-StartedAt;
// terminal ones prefer EndedAt-StartedAt.
func (trace *AgentTrace) elapsed() time.Duration {
	if trace == nil || trace.StartedAt.IsZero() {
		return 0
	}
	end := time.Now()
	if !trace.EndedAt.IsZero() {
		end = trace.EndedAt
	}
	d := end.Sub(trace.StartedAt)
	if d < 0 {
		return 0
	}
	return d
}
