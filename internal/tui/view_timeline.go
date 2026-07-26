package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) latestToolBlock() *ToolBlock {
	return m.timeline.latestToolBlock()
}

func (m Model) renderWorkspaceTimeline(layout LayoutState) string {
	var rendered string
	if len(m.timeline.Turns) == 0 {
		rendered = m.renderWelcomeScreenWidth(layout.TimelineH, layout.TimelineW)
	} else {
		// No "↓ N new / End latest" chrome: follow-pause still tracks new
		// output internally, but sticky counters near the prompt were noisy
		// and easy to leave stuck while scrolling.
		rendered = renderVisibleLinesWidth(m.workspaceTimelineLines(layout.TimelineW), layout.TimelineH, m.scroll, layout.TimelineW)
	}
	// Highlight every search match in the visible transcript while the search
	// dock is open. Skipped at idle (no query) so normal frames pay no cost.
	if m.search.Active {
		rendered = applySearchHighlight(rendered, m.search.Query)
	}
	return rendered
}

func (m Model) workspaceTimelineLines(width int) []string {
	lines, _ := m.renderTimelineLines(width)
	return lines
}

// renderTimelineLines renders the full timeline and also records each turn's
// starting line index (turnStarts), so message navigation (alt+ctrl+p/n) can
// jump the scroll position to a specific user prompt. workspaceTimelineLines
// discards the index; navigateTurn uses it.
//
// The cache separates display settings from timeline content. When only the
// active (normally last) turn changes during streaming, it retains the rendered
// prefix in-place and rebuilds only the changed suffix. This avoids walking,
// splitting, styling, and allocating every historical block once per frame.
type timelineLinesKey struct {
	width           int
	showToolDetails bool
	showTimestamps  bool
	showThinking    bool
	verbose         bool
	accent          string
	// Selection/fold are view-only but affect rendered lines; include them so
	// the history cache does not serve a stale unselected/unfolded frame.
	scrollSel   int
	focus       Focus
	foldedStamp string
}

type timelineBlockLinesKey struct {
	id              string
	kind            BlockKind
	title           string
	summary         string
	body            string
	tool            *ToolBlock
	createdAt       time.Time
	width           int
	showToolDetails bool
	showThinking    bool
	verbose         bool
	accent          string
	folded          bool
}

type timelineBlockLinesEntry struct {
	key   timelineBlockLinesKey
	lines []string
}

type liveStreamLinesKey struct {
	generation   uint64
	kind         BlockKind
	visibleLen   int
	width        int
	showThinking bool
	verbose      bool
	accent       string
}

type liveStreamLinesCache struct {
	initialized bool
	key         liveStreamLinesKey
	lines       []string
}

// timelineTurnKey uses the immutable timeline's structural sharing to identify
// an unchanged turn cheaply. Timeline mutation methods copy the changed turn's
// Blocks slice; untouched turns keep the same first-block address. The scalar
// fields cover committed run-status changes and empty turns. Unlike
// Timeline.Version alone, this also distinguishes a newly loaded session whose
// mutation count happens to collide with the previous session.
type timelineTurnKey struct {
	id            string
	userID        string
	userTitle     string
	userBody      string
	userCreatedAt time.Time
	blocksLen     int
	blocksFirst   *Block
	startedAt     time.Time
	endedAt       time.Time
	run           TurnRunStatus
}

func timelineKeyForTurn(turn Turn) timelineTurnKey {
	var first *Block
	if len(turn.Blocks) > 0 {
		first = &turn.Blocks[0]
	}
	return timelineTurnKey{
		id:            turn.ID,
		userID:        turn.User.ID,
		userTitle:     turn.User.Title,
		userBody:      turn.User.Body,
		userCreatedAt: turn.User.CreatedAt,
		blocksLen:     len(turn.Blocks),
		blocksFirst:   first,
		startedAt:     turn.StartedAt,
		endedAt:       turn.EndedAt,
		run:           turn.Run,
	}
}

// timelineLinesCache is a pointer (reference type) on Model so View's value
// receiver can update it across frames. lines contains committed history only;
// live and composed hold the independently cached live overlay and the dynamic
// latest-turn suffix. Returned line slices are ephemeral: a later render may
// update composed in the same backing array.
type timelineLinesCache struct {
	initialized     bool
	key             timelineLinesKey
	timelineVersion int
	timelineFirst   *Turn
	timelineLen     int
	turnKeys        []timelineTurnKey
	lines           []string
	turnStarts      []int
	blockLines      map[string]timelineBlockLinesEntry
	live            liveStreamLinesCache
	composed        []string
}

func (m Model) renderTimelineLines(width int) ([]string, []int) {
	history, turnStarts := m.renderTimelineHistoryLines(width)
	if len(m.timeline.Turns) == 0 {
		return history, turnStarts
	}

	liveLines := m.renderLiveStreamLines(width)
	latest := m.timeline.Turns[len(m.timeline.Turns)-1]
	// Grok stack: live busy/waiting status lives in the dock turn-status row
	// above the prompt — not also at the bottom of scrollback.
	runStatus := ""
	if !m.agent.Busy && m.agent.Pending == nil && m.question == nil {
		runStatus = m.renderTurnRunStatus(latest)
	}
	timestamp := ""
	if m.showTimestamps {
		timestamp = renderTurnTimestamp(latest)
	}
	if len(liveLines) == 0 && runStatus == "" && timestamp == "" {
		return history, turnStarts
	}

	prefixLen := len(history)
	if prefixLen > 0 && history[prefixLen-1] == "" {
		prefixLen--
	}
	needed := prefixLen + len(liveLines) + 1
	if runStatus != "" {
		needed++
	}
	if timestamp != "" {
		needed++
	}

	cache := m.timelineLinesCache
	if cache == nil {
		lines := make([]string, 0, needed)
		lines = append(lines, history[:prefixLen]...)
		lines = append(lines, liveLines...)
		if runStatus != "" {
			lines = append(lines, runStatus)
		}
		if timestamp != "" {
			lines = append(lines, timestamp)
		}
		return append(lines, ""), turnStarts
	}
	if cap(cache.composed) < needed {
		cache.composed = make([]string, 0, needed)
	} else {
		cache.composed = cache.composed[:0]
	}
	cache.composed = append(cache.composed, history[:prefixLen]...)
	cache.composed = append(cache.composed, liveLines...)
	if runStatus != "" {
		cache.composed = append(cache.composed, runStatus)
	}
	if timestamp != "" {
		cache.composed = append(cache.composed, timestamp)
	}
	cache.composed = append(cache.composed, "")
	return cache.composed, turnStarts
}

// renderTimelineHistoryLines renders and caches only committed timeline state.
// Live body/generation and dynamic spinner/detail values never participate in
// this key or in the cached line storage.
func (m Model) renderTimelineHistoryLines(width int) (lines []string, turnStarts []int) {
	key := timelineLinesKey{
		width:           width,
		showToolDetails: m.showToolDetails,
		showTimestamps:  m.showTimestamps,
		showThinking:    m.showThinking,
		verbose:         m.verbose,
		accent:          m.accent,
		scrollSel:       m.scrollSel,
		focus:           m.focus,
		foldedStamp:     m.foldedEntriesStamp(),
	}
	cache := m.timelineLinesCache
	if cache == nil {
		return m.computeTimelineHistoryLines(width)
	}
	var timelineFirst *Turn
	if len(m.timeline.Turns) > 0 {
		timelineFirst = &m.timeline.Turns[0]
	}
	if cache.initialized && cache.key == key && cache.timelineVersion == m.timeline.Version &&
		cache.timelineFirst == timelineFirst && cache.timelineLen == len(m.timeline.Turns) {
		return cache.lines, cache.turnStarts
	}

	firstChanged := 0
	if cache.initialized && cache.key == key {
		common := min(len(cache.turnKeys), len(m.timeline.Turns))
		for firstChanged < common && cache.turnKeys[firstChanged] == timelineKeyForTurn(m.timeline.Turns[firstChanged]) {
			firstChanged++
		}
		if firstChanged == len(cache.turnKeys) && firstChanged == len(m.timeline.Turns) {
			// A no-op timeline mutation can advance Version without changing any
			// committed render input.
			cache.timelineVersion = m.timeline.Version
			return cache.lines, cache.turnStarts
		}
		if len(cache.turnKeys) != len(m.timeline.Turns) && firstChanged > 0 {
			// The previous/latest turn changes role when a turn is added or
			// removed, so rebuild it to restore historical status/timestamps.
			firstChanged--
		}
	}

	prefixLines := 0
	if firstChanged < len(cache.turnStarts) {
		prefixLines = cache.turnStarts[firstChanged]
	} else if firstChanged == len(cache.turnStarts) {
		prefixLines = len(cache.lines)
	}
	lines = cache.lines[:prefixLines]
	turnStarts = cache.turnStarts[:firstChanged]
	turnKeys := cache.turnKeys[:firstChanged]
	last := len(m.timeline.Turns) - 1
	for i := firstChanged; i < len(m.timeline.Turns); i++ {
		turn := m.timeline.Turns[i]
		turnStarts = append(turnStarts, len(lines))
		runStatus := ""
		includeTimestamp := false
		if i != last {
			runStatus = m.renderTurnRunStatus(turn)
			includeTimestamp = m.showTimestamps
		}
		lines = m.appendTimelineTurnHistoryLines(lines, turn, width, runStatus, includeTimestamp)
		turnKeys = append(turnKeys, timelineKeyForTurn(turn))
	}

	cache.initialized = true
	cache.key = key
	cache.timelineVersion = m.timeline.Version
	cache.timelineFirst = timelineFirst
	cache.timelineLen = len(m.timeline.Turns)
	cache.turnKeys = turnKeys
	cache.lines = lines
	cache.turnStarts = turnStarts
	return lines, turnStarts
}

func (m Model) computeTimelineHistoryLines(width int) (lines []string, turnStarts []int) {
	turnStarts = make([]int, 0, len(m.timeline.Turns))
	last := len(m.timeline.Turns) - 1
	for i, turn := range m.timeline.Turns {
		turnStarts = append(turnStarts, len(lines))
		runStatus := ""
		includeTimestamp := false
		if i != last {
			runStatus = m.renderTurnRunStatus(turn)
			includeTimestamp = m.showTimestamps
		}
		lines = m.appendTimelineTurnHistoryLines(lines, turn, width, runStatus, includeTimestamp)
	}
	return lines, turnStarts
}

func (m Model) appendTimelineTurnHistoryLines(lines []string, turn Turn, width int, runStatus string, includeTimestamp bool) []string {
	turnIdx := -1
	for i := range m.timeline.Turns {
		if m.timeline.Turns[i].ID == turn.ID {
			turnIdx = i
			break
		}
	}
	sel, hasSel := m.selectedScrollEntry()
	selecting := hasSel && m.focus == FocusScrollback

	if body := strings.TrimSpace(turn.User.Body); body != "" {
		// Split into one timeline row per visual line. Storing a multi-line
		// string as a single slice element breaks height/scroll math: the
		// body window counts it as one row, then fitBodyWindow keeps the
		// bottom of the expanded text and drops earlier prompt lines.
		userLines := strings.Split(renderUserEcho(body, width), "\n")
		if selecting && sel.turnIdx == turnIdx && sel.blockIdx < 0 {
			userLines = highlightScrollSelection(userLines, true)
		}
		// Consistent inter-turn spacing: blank line after the user row.
		lines = append(lines, userLines...)
		lines = append(lines, "")
	}
	for bi, block := range turn.Blocks {
		blockLines := m.renderCachedTimelineBlockLines(block, width)
		if len(blockLines) == 0 {
			continue
		}
		if selecting && sel.turnIdx == turnIdx && sel.blockIdx == bi {
			blockLines = highlightScrollSelection(blockLines, true)
		}
		lines = append(lines, blockLines...)
		// One blank line between major blocks for hierarchy (not a log dump).
		if bi < len(turn.Blocks)-1 || runStatus != "" || includeTimestamp {
			lines = append(lines, "")
		}
	}
	if runStatus != "" {
		lines = append(lines, runStatus)
	}
	if includeTimestamp {
		if ts := renderTurnTimestamp(turn); ts != "" {
			lines = append(lines, ts)
		}
	}
	return append(lines, "")
}

func (m Model) renderLiveStreamLines(width int) []string {
	cache := m.timelineLinesCache
	live := m.agent.LiveStream
	if live == nil {
		if cache != nil {
			cache.live = liveStreamLinesCache{}
		}
		return nil
	}
	// Pending unfinished tail (no trailing newline yet): show a single dim
	// "writing" row — never character-by-character typewriter of the tail.
	hasPending := live.visibleLen < len(live.body)
	body := strings.TrimSuffix(live.visibleBody(), "\n")
	if body == "" && !hasPending {
		if cache != nil {
			cache.live = liveStreamLinesCache{}
		}
		return nil
	}
	// Include pending flag in cache key so the writing row appears/disappears.
	pendingFlag := 0
	if hasPending {
		pendingFlag = 1
	}
	key := liveStreamLinesKey{
		generation:   live.generation,
		kind:         live.kind,
		visibleLen:   live.visibleLen + pendingFlag, // distinguish pending state
		width:        width,
		showThinking: m.showThinking,
		verbose:      m.verbose,
		accent:       m.accent,
	}
	if cache != nil && cache.live.initialized && cache.live.key == key {
		return cache.live.lines
	}

	var lines []string
	switch live.kind {
	case BlockAssistant:
		if body != "" {
			contentWidth := max(1, width-2)
			// Stream stable closed lines as plain wrap (no full markdown per
			// delta — performance guardrail). Committed history uses goldmark.
			wrapped := ansi.Wordwrap(body, contentWidth, "")
			wrapped = ansi.Hardwrap(wrapped, contentWidth, true)
			lines = renderBlockMarkerLines("│", accentStyle, wrapped)
		}
		if hasPending {
			lines = append(lines, dimStyle.Render("│ …"))
		}
	case BlockReasoning:
		// Live thinking does NOT grow inside the transcript — multi-line
		// tail previews change height every delta and make scrollback jitter.
		// Preview lives in the fixed dock turn-status row (see
		// renderTurnStatusLine). showThinking still paints a full live body
		// when the user explicitly asks for it (Ctrl+T).
		if m.showThinking {
			lines = renderLiveReasoningLines(body, max(20, width-2), true, hasPending)
		} else {
			lines = nil
		}
	}
	if cache != nil {
		cache.live = liveStreamLinesCache{initialized: true, key: key, lines: lines}
	}
	return lines
}

// renderUserEcho renders a user prompt as a full-width card so user turns read
// as conversation blocks, not gray text on a black half-line. Claude Code paints
// userMessageBackground across the Box; we pad every visual row with the same
// Selection background so fitStyledLine never leaves an unstyled black tail.
func renderUserEcho(body string, width int) string {
	if width < 1 {
		width = 1
	}
	text := lipgloss.NewStyle().
		Foreground(DefaultTheme.Text).
		Background(DefaultTheme.Selection)
	you := lipgloss.NewStyle().
		Foreground(DefaultTheme.TextMuted).
		Background(DefaultTheme.Selection).
		Bold(true)
	prompt := lipgloss.NewStyle().
		Foreground(DefaultTheme.GrayBright).
		Background(DefaultTheme.Selection)

	const (
		youTag    = " you "
		promptTag = "❯ "
	)
	firstPrefixCols := lipgloss.Width(youTag) + lipgloss.Width(promptTag)
	contPad := strings.Repeat(" ", firstPrefixCols)

	parts := strings.Split(body, "\n")
	out := make([]string, 0, len(parts))
	for i, line := range parts {
		if i == 0 {
			msgWidth := max(1, width-firstPrefixCols)
			for j, ml := range wrapPlainLines(line, msgWidth) {
				var styled string
				if j == 0 {
					styled = you.Render(youTag) + prompt.Render(promptTag) + text.Render(ml)
				} else {
					styled = text.Render(contPad + ml)
				}
				out = append(out, stretchCardLine(styled, width))
			}
			continue
		}
		msgWidth := max(1, width-firstPrefixCols)
		for _, ml := range wrapPlainLines(line, msgWidth) {
			out = append(out, stretchCardLine(text.Render(contPad+ml), width))
		}
	}
	return strings.Join(out, "\n")
}

// wrapPlainLines word/hard-wraps unstyled text into visual rows of at most width.
func wrapPlainLines(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	if text == "" {
		return []string{""}
	}
	wrapped := ansi.Wordwrap(text, width, "")
	wrapped = ansi.Hardwrap(wrapped, width, true)
	return strings.Split(wrapped, "\n")
}

// stretchCardLine pads a styled row to width with Selection-background spaces so
// the user card is a solid bar, not gray glyphs on a black remainder.
func stretchCardLine(styled string, width int) string {
	if width < 1 {
		width = 1
	}
	w := lipgloss.Width(styled)
	if w > width {
		return ansi.Truncate(styled, width, "")
	}
	if w == width {
		return styled
	}
	pad := lipgloss.NewStyle().
		Background(DefaultTheme.Selection).
		Render(strings.Repeat(" ", width-w))
	return styled + pad
}

func isToolActivityBlock(block Block) bool {
	return block.Tool != nil && (block.Kind == BlockTool || block.Kind == BlockConfirmation)
}

// renderSubagentBlock renders a delegated subagent task as a single folded
// line: an accent title, a status-colored lifecycle, and a one-line preview of
// the body (usually the result/description). The full body is intentionally not
// expanded so a long subagent result does not push the main agent's own output
// out of view.
func renderSubagentBlock(block Block, width int) string {
	header := commandStyle.Render(block.Title)
	statusStyle := dimStyle
	switch strings.TrimSpace(block.Summary) {
	case "running":
		statusStyle = accentStyle
	case "completed":
		statusStyle = successStyle
	case "failed":
		statusStyle = errorStyle
	}
	if status := strings.TrimSpace(block.Summary); status != "" {
		header += " " + statusStyle.Render(status)
	}
	body := strings.TrimRight(block.Body, "\n")
	if body == "" {
		return header
	}
	firstLine, _, _ := strings.Cut(body, "\n")
	preview := strings.TrimSpace(firstLine)
	if preview == "" {
		return header
	}
	limit := width - lipgloss.Width(header) - 4
	if limit < 20 {
		limit = 20
	}
	return header + " " + dimStyle.Render(truncatePlain(preview, limit))
}

// agentBarView renders the bounded Agent switcher pinned above the composer: the
// current turn's child agents with status, the selected one highlighted when
// the bar holds focus, and a hint describing the focus key / nav keys.
func (m Model) currentLayout() LayoutState {
	dock := m.measureBottomDock()
	return CalculateLayout(m.width, m.height, dock.reservedHeight())
}

func (m Model) clampScroll(layout LayoutState) Model {
	maxScroll := m.maxScroll(layout)
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll == 0 {
		m.scrollPaused = false
		m.newOutputBelow = false
		m.newOutputCount = 0
	}
	return m
}

func (m Model) maxScroll(layout LayoutState) int {
	if layout.TimelineH < 1 {
		return 0
	}
	var lines []string
	if m.focus == FocusAgentWindow {
		lines = m.agentWindowLines(layout.TimelineW)
	} else if len(m.timeline.Turns) > 0 {
		lines = m.workspaceTimelineLines(layout.TimelineW)
	}
	maxScroll := len(lines) - layout.TimelineH
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

func (m Model) renderCachedTimelineBlockLines(block Block, width int) []string {
	// Panels can contain dynamic command-specific state and do not have a
	// stable structural identity. Blocks without IDs are likewise unsafe to
	// cache because a later block could otherwise reuse their map slot.
	if block.ID == "" || block.Panel != nil || m.timelineLinesCache == nil {
		return m.renderTimelineBlockLines(block, width)
	}

	key := timelineBlockLinesKey{
		id:              block.ID,
		kind:            block.Kind,
		title:           block.Title,
		summary:         block.Summary,
		body:            block.Body,
		tool:            block.Tool,
		createdAt:       block.CreatedAt,
		width:           width,
		showToolDetails: m.showToolDetails,
		showThinking:    m.showThinking,
		verbose:         m.verbose,
		accent:          m.accent,
		folded:          m.isEntryFolded(block.ID) || m.isEntryExpanded(block.ID),
	}
	cache := m.timelineLinesCache
	if cache.blockLines == nil {
		cache.blockLines = make(map[string]timelineBlockLinesEntry)
	}
	if entry, ok := cache.blockLines[block.ID]; ok && entry.key == key {
		return entry.lines
	}

	lines := m.renderTimelineBlockLines(block, width)
	cache.blockLines[block.ID] = timelineBlockLinesEntry{key: key, lines: lines}
	return lines
}
