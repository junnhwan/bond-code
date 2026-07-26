package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/ask"
)

// mouseHitKind identifies which interactive surface a click/hover lands on.
// Grok Build treats these as real controls; BondCode mirrors that contract.
type mouseHitKind uint8

const (
	mouseHitNone mouseHitKind = iota
	mouseHitScrollback
	mouseHitComposer
	mouseHitPermissionOption
	mouseHitQuestionOption
	mouseHitWelcomeMenu
	mouseHitSuggestion
	mouseHitStop      // turn-status [stop] cancel button
	mouseHitScrollbar // transcript right-edge track / thumb
)

// mouseHit is the resolved target under a terminal cell.
type mouseHit struct {
	kind    mouseHitKind
	index   int    // option / menu / suggestion index
	command string // welcome menu slash command
}

// mouseHover tracks hover feedback for surfaces that re-render on motion.
type mouseHover struct {
	kind  mouseHitKind
	index int
}

// mouseBand is one vertical strip of the composed workspace view.
type mouseBand struct {
	name  string
	start int
	end   int // exclusive
	text  string
}

// handleMouseMsg processes wheel, motion (hover), drag, and left-click press.
func (m Model) handleMouseMsg(msg tea.MouseMsg) (Model, tea.Cmd) {
	// Runtime/config may have mouse tracking off — ignore stray events.
	if !m.mouseEnabled {
		return m, nil
	}
	// Wheel always scrolls the transcript (and cancels an in-progress drag).
	// Keep the prompt focused so scrolling does not grey the composer via
	// FocusScrollback / BlurredStyle — wheel is not a focus change.
	if tea.MouseEvent(msg).IsWheel() || msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		m.scrollbarDragging = false
		var cmd tea.Cmd
		if m.agent.Pending == nil && m.question == nil && !m.search.Active &&
			m.focus != FocusAgentWindow && m.focus != FocusAgentBar {
			m, cmd = m.withFocus(FocusComposer)
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m.scrollBy(3), cmd
		case tea.MouseButtonWheelDown:
			return m.scrollBy(-3), cmd
		}
		return m, cmd
	}

	// Release ends scrollbar drag.
	if msg.Action == tea.MouseActionRelease {
		if m.scrollbarDragging {
			m.scrollbarDragging = false
			return m, nil
		}
		return m, nil
	}

	// Active scrollbar drag: any motion (or held press) jumps the viewport.
	// Keep the composer focused for the whole drag so the prompt does not
	// grey out if a motion event briefly resolves as scrollback.
	if m.scrollbarDragging {
		if msg.Action == tea.MouseActionMotion || msg.Button == tea.MouseButtonLeft {
			var cmd tea.Cmd
			if m.agent.Pending == nil && m.question == nil && !m.search.Active &&
				m.focus != FocusAgentWindow && m.focus != FocusAgentBar {
				m, cmd = m.withFocus(FocusComposer)
			}
			return m.applyScrollbarDrag(msg.Y), cmd
		}
	}

	// Motion → hover highlight only.
	if msg.Action == tea.MouseActionMotion {
		return m.applyMouseHoverWithTick(m.resolveMouseHit(msg.X, msg.Y))
	}
	// Some terminals deliver hover as ButtonNone without Motion.
	if msg.Button == tea.MouseButtonNone && msg.Action != tea.MouseActionPress {
		return m.applyMouseHoverWithTick(m.resolveMouseHit(msg.X, msg.Y))
	}

	// Left press only for activation.
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	return m.activateMouseHit(m.resolveMouseHit(msg.X, msg.Y))
}

func (m Model) applyMouseHover(hit mouseHit) Model {
	switch hit.kind {
	case mouseHitComposer:
		m.hover = mouseHover{kind: mouseHitComposer}
	case mouseHitStop:
		m.hover = mouseHover{kind: mouseHitStop}
	case mouseHitWelcomeMenu:
		m.hover = mouseHover{kind: mouseHitWelcomeMenu, index: hit.index}
		m.welcomeMenuActive = hit.index
	case mouseHitPermissionOption:
		m.hover = mouseHover{kind: mouseHitPermissionOption, index: hit.index}
		// Live highlight: move ConfirmChoice under the pointer.
		if m.agent.Pending != nil && !m.agent.ConfirmEnteringReject {
			m.agent.ConfirmChoice = permissionChoiceFromIndex(hit.index, isHighRisk(m.agent.Pending), m.alwaysAvailable())
		}
	case mouseHitQuestionOption:
		m.hover = mouseHover{kind: mouseHitQuestionOption, index: hit.index}
		if m.question != nil && hit.index >= 0 && hit.index < len(m.question.Options) {
			m.questionCursor = hit.index
		}
	case mouseHitSuggestion:
		m.hover = mouseHover{kind: mouseHitSuggestion, index: hit.index}
		if m.composer.Suggestions != nil {
			m.composer.Suggestions.selected = hit.index
		}
	default:
		m.hover = mouseHover{}
	}
	return m
}

// applyMouseHoverWithTick updates hover and may start (not multiply) an
// animation tick chain for hover pulse / flash surfaces.
func (m Model) applyMouseHoverWithTick(hit mouseHit) (Model, tea.Cmd) {
	m = m.applyMouseHover(hit)
	return m.ensureAnimTick()
}

func (m Model) activateMouseHit(hit mouseHit) (Model, tea.Cmd) {
	// Every successful hit gets a short flash so the click "lands".
	armFlash := func(next Model, cmd tea.Cmd) (Model, tea.Cmd) {
		if hit.kind == mouseHitNone {
			return next, cmd
		}
		next.flash = newUIFlash(hit.kind)
		var tick tea.Cmd
		next, tick = next.ensureAnimTick()
		return next, tea.Batch(cmd, tick)
	}

	switch hit.kind {
	case mouseHitComposer:
		m, cmd := m.withFocus(FocusComposer)
		m.hover = mouseHover{kind: mouseHitComposer}
		return armFlash(m, cmd)

	case mouseHitScrollback:
		// Keep the composer focused for mouse clicks on the transcript.
		// Focusing scrollback greys the prompt (BlurredStyle) and feels like
		// "scrolling killed the input box". Keyboard Tab still enters
		// scrollback navigation mode.
		var cmd tea.Cmd
		if m.agent.Pending == nil && m.question == nil && !m.search.Active &&
			m.focus != FocusAgentWindow && m.focus != FocusAgentBar {
			m, cmd = m.withFocus(FocusComposer)
		}
		m.hover = mouseHover{}
		return armFlash(m, cmd)

	case mouseHitWelcomeMenu:
		m.welcomeMenuActive = hit.index
		m.hover = mouseHover{kind: mouseHitWelcomeMenu, index: hit.index}
		cmdName := strings.TrimSpace(hit.command)
		if cmdName == "" {
			return armFlash(m, nil)
		}
		next, cmd := m.runCommand(m.cfg.Context, cmdName)
		return armFlash(next, cmd)

	case mouseHitPermissionOption:
		next, cmd := m.clickPermissionOption(hit.index)
		return armFlash(next, cmd)

	case mouseHitQuestionOption:
		next, cmd := m.clickQuestionOption(hit.index)
		return armFlash(next, cmd)

	case mouseHitSuggestion:
		next, cmd := m.clickSuggestion(hit.index)
		return armFlash(next, cmd)

	case mouseHitStop:
		if m.agent.Busy {
			m = m.cancelRunningAgent()
			m = m.pushToast("stopped", toastWarn)
		}
		next, tick := m.ensureAnimTick()
		return armFlash(next, tick)

	case mouseHitScrollbar:
		// Start drag; jump only when the click is on the track (outside the
		// current thumb) — matching Grok: thumb press holds position, track
		// press jumps. Never steal focus into scrollback.
		var cmd tea.Cmd
		if m.agent.Pending == nil && m.question == nil && !m.search.Active {
			m, cmd = m.withFocus(FocusComposer)
		}
		m.scrollbarDragging = true
		layout := m.currentLayout()
		maxScroll := m.maxScroll(layout)
		thumbTop, thumbSize := scrollbarMetrics(m.scroll, maxScroll, layout.TimelineH)
		onThumb := hit.index >= thumbTop && hit.index < thumbTop+thumbSize
		if !onThumb {
			m = m.scrollToScrollbarRelY(hit.index)
		}
		m.hover = mouseHover{kind: mouseHitScrollbar}
		return armFlash(m, cmd)
	}
	return m, nil
}

// applyScrollbarDrag updates scroll from an absolute terminal Y while the
// user is dragging the right-edge track.
func (m Model) applyScrollbarDrag(absY int) Model {
	_, bodyTop, bodyH, ok := m.transcriptBodyBand()
	if !ok || bodyH < 1 {
		return m
	}
	return m.scrollToScrollbarRelY(absY - bodyTop)
}

// scrollToScrollbarRelY maps a body-relative track row to scroll offset and
// pauses auto-follow when not at the bottom (scroll > 0).
func (m Model) scrollToScrollbarRelY(relY int) Model {
	layout := m.currentLayout()
	maxScroll := m.maxScroll(layout)
	m.scroll = scrollFromScrollbarY(relY, layout.TimelineH, maxScroll)
	m = m.clampScroll(layout)
	if m.scroll > 0 {
		m.scrollPaused = true
	} else {
		m.scrollPaused = false
		m = m.clearNewOutputBelow()
	}
	return m
}

// scrollbarHit reports whether terminal column x is inside the interactive
// scrollbar gutter for a timeline of width w. The painted track is 1 cell;
// the hit zone is 2 cells (or 1 on very narrow layouts) so drag-to-scroll
// does not require pixel-perfect aim on the right edge.
func scrollbarHit(x, width int) bool {
	if width <= 1 {
		return false
	}
	gutter := 2
	if width < 8 {
		gutter = 1
	}
	return x >= width-gutter
}

// transcriptBodyBand returns the body (transcript) vertical band for hit
// testing and scrollbar drag, using the same stack geometry as resolveMouseHit.
func (m Model) transcriptBodyBand() (layout LayoutState, start, height int, ok bool) {
	if m.width < 1 || m.height < 1 {
		return LayoutState{}, 0, 0, false
	}
	dock := m.measureBottomDock()
	layout = CalculateLayout(m.width, m.height, dock.reservedHeight())
	if layout.TimelineH < 1 {
		return layout, 0, 0, false
	}
	return layout, 0, layout.TimelineH, true
}

func permissionChoiceFromIndex(idx int, high, alwaysAvail bool) confirmChoice {
	if high {
		if idx == 0 {
			return choiceOnce
		}
		return choiceReject
	}
	switch idx {
	case 0:
		return choiceOnce
	case 1:
		if alwaysAvail {
			return choiceAlways
		}
		return choiceOnce
	default:
		return choiceReject
	}
}

func (m Model) clickPermissionOption(idx int) (Model, tea.Cmd) {
	if m.agent.Pending == nil || m.agent.ConfirmEnteringReject {
		return m, nil
	}
	high := isHighRisk(m.agent.Pending)
	alwaysAvail := m.alwaysAvailable()
	if !high && !alwaysAvail && idx == 1 {
		return m, nil // dimmed Always
	}
	if high && (idx < 0 || idx > 1) {
		return m, nil
	}
	if !high && (idx < 0 || idx > 2) {
		return m, nil
	}
	choice := permissionChoiceFromIndex(idx, high, alwaysAvail)
	m.agent.ConfirmChoice = choice
	// One click confirms — matches "click has an effect" expectation.
	return m.respondToConfirmation(choice, "")
}

func (m Model) clickQuestionOption(idx int) (Model, tea.Cmd) {
	if m.question == nil || idx < 0 || idx >= len(m.question.Options) {
		return m, nil
	}
	m.questionCursor = idx
	if m.question.Multi {
		if m.questionSelected == nil {
			m.questionSelected = map[int]bool{}
		}
		m.questionSelected[idx] = !m.questionSelected[idx]
		return m, nil
	}
	m = m.confirmQuestion()
	return m, m.waitForAgent()
}

func (m Model) clickSuggestion(idx int) (Model, tea.Cmd) {
	if m.composer.Suggestions == nil || !m.composer.Suggestions.IsVisible() {
		return m, nil
	}
	filter := m.getCommandFilter()
	visible := m.composer.Suggestions.GetVisible(filter)
	if idx < 0 || idx >= len(visible) {
		return m, nil
	}
	m.composer.Suggestions.selected = idx
	selected := m.composer.Suggestions.GetSelected(filter)
	if selected == "" {
		return m, nil
	}
	m = m.completeSelectedSuggestion(filter, selected)
	return m, nil
}

// resolveMouseHit maps terminal (x,y) to an interactive surface using the same
// stack order as composeBaseView.
func (m Model) resolveMouseHit(x, y int) mouseHit {
	if m.width < 1 || m.height < 1 || y < 0 || y >= m.height {
		return mouseHit{}
	}
	if m.history.visible && m.agent.Pending == nil && m.question == nil {
		return mouseHit{}
	}
	if m.overlay.active() {
		return mouseHit{}
	}

	dock := m.measureBottomDock()
	layout := CalculateLayout(m.width, m.height, dock.reservedHeight())
	dock = m.renderBottomDock(dock, layout)
	footer := m.shortcutsBarLine(layout.TimelineW)
	if footer == "" {
		footer = " "
	}

	parts := []struct {
		name string
		text string
		h    int
	}{
		{"body", "", layout.TimelineH},
		{"separator", dock.separator, renderedHeight(dock.separator)},
		{"turnStatus", dock.turnStatus, renderedHeight(dock.turnStatus)},
		{"queued", dock.queued, renderedHeight(dock.queued)},
		{"suggestions", dock.suggestions, renderedHeight(dock.suggestions)},
		{"agent", dock.agentBar, renderedHeight(dock.agentBar)},
		{"permission", dock.permission, renderedHeight(dock.permission)},
		{"question", dock.question, renderedHeight(dock.question)},
		{"composer", dock.composer, renderedHeight(dock.composer)},
		{"footer", footer, renderedHeight(footer)},
	}

	bands := make([]mouseBand, 0, len(parts))
	yCursor := 0
	for _, p := range parts {
		if p.h <= 0 {
			continue
		}
		bands = append(bands, mouseBand{name: p.name, start: yCursor, end: yCursor + p.h, text: p.text})
		yCursor += p.h
	}
	if yCursor > m.height {
		bands = refitMouseBandsFromBottom(bands, m.height, layout.TimelineH)
	}

	var target mouseBand
	found := false
	for _, b := range bands {
		if y >= b.start && y < b.end {
			target = b
			found = true
			break
		}
	}
	if !found {
		return mouseHit{}
	}

	relY := y - target.start
	switch target.name {
	case "body":
		// Scrollbar gutter: last N columns (wider than the 1-cell paint so a
		// near-miss does not fall through to scrollback focus and grey the
		// composer). Visual track stays 1 column; hit target is forgiving.
		if m.scrollbarVisible(layout) && scrollbarHit(x, layout.TimelineW) {
			return mouseHit{kind: mouseHitScrollbar, index: relY}
		}
		if len(m.timeline.Turns) == 0 {
			if hit, ok := m.welcomeMenuHitAt(layout.TimelineW, layout.TimelineH, x, relY); ok {
				return hit
			}
		}
		return mouseHit{kind: mouseHitScrollback}
	case "composer":
		return mouseHit{kind: mouseHitComposer}
	case "permission":
		if idx, ok := permissionOptionIndexAt(target.text, relY, isHighRisk(m.agent.Pending), m.alwaysAvailable()); ok {
			return mouseHit{kind: mouseHitPermissionOption, index: idx}
		}
	case "question":
		if idx, ok := questionOptionIndexAt(target.text, relY, m.question); ok {
			return mouseHit{kind: mouseHitQuestionOption, index: idx}
		}
	case "suggestions":
		if relY >= 0 && relY < renderedHeight(target.text) {
			return mouseHit{kind: mouseHitSuggestion, index: relY}
		}
	case "turnStatus":
		// Right-side [stop] hit target (last 8 cells).
		if m.agent.Busy && strings.Contains(target.text, "[stop]") {
			stopW := 6
			if x >= m.width-stopW-1 {
				return mouseHit{kind: mouseHitStop}
			}
		}
	}
	return mouseHit{}
}

func refitMouseBandsFromBottom(bands []mouseBand, height, bodyH int) []mouseBand {
	byName := make(map[string]mouseBand, len(bands))
	for _, b := range bands {
		byName[b.name] = b
	}
	priority := []string{"footer", "permission", "question", "composer", "turnStatus", "agent", "body", "suggestions", "queued", "separator"}
	remaining := height
	kept := make(map[string]int, len(priority))
	for _, name := range priority {
		b, ok := byName[name]
		if !ok {
			continue
		}
		h := b.end - b.start
		if name == "body" {
			h = bodyH
		}
		if h <= 0 || remaining <= 0 {
			continue
		}
		if h > remaining {
			h = remaining
		}
		kept[name] = h
		remaining -= h
	}
	order := []string{"body", "separator", "turnStatus", "queued", "suggestions", "agent", "permission", "question", "composer", "footer"}
	out := make([]mouseBand, 0, len(order))
	y := 0
	for _, name := range order {
		h, ok := kept[name]
		if !ok || h <= 0 {
			continue
		}
		text := ""
		if b, ok := byName[name]; ok {
			text = b.text
		}
		out = append(out, mouseBand{name: name, start: y, end: y + h, text: text})
		y += h
	}
	return out
}

func (m Model) welcomeMenuHitAt(width, height, x, relY int) (mouseHit, bool) {
	left, right := welcomeMenuColumnBounds(width)
	// Only the painted menu column is interactive — padding left/right of the
	// centered bar must not steal clicks (full-row hits feel broken).
	if x < left || x >= right {
		return mouseHit{}, false
	}
	rows := welcomeMenuRowYs(WelcomeChromeInput{
		Width:   width,
		Height:  height,
		Project: m.cfg.Status.ProjectRoot,
		Branch:  m.cfg.Status.GitBranch,
		Version: "v1.0.0",
		Model:   m.cfg.Status.Model,
	})
	items := welcomeMenuItems()
	for i, row := range rows {
		if relY == row && i < len(items) {
			return mouseHit{kind: mouseHitWelcomeMenu, index: i, command: items[i].Command}, true
		}
	}
	return mouseHit{}, false
}

// permissionOptionIndexAt maps a relative Y inside the permission panel to an
// option index. Options sit just above the trailing hint line.
func permissionOptionIndexAt(panel string, relY int, high, alwaysAvail bool) (int, bool) {
	if panel == "" || relY < 0 {
		return 0, false
	}
	nOpts := 3
	if high {
		nOpts = 2
	}
	h := renderedHeight(panel)
	hintLine := h - 1
	firstOpt := hintLine - nOpts
	if firstOpt < 0 {
		return 0, false
	}
	if relY < firstOpt || relY >= hintLine {
		return 0, false
	}
	idx := relY - firstOpt
	if idx < 0 || idx >= nOpts {
		return 0, false
	}
	if !high && !alwaysAvail && idx == 1 {
		return 0, false
	}
	return idx, true
}

// questionOptionIndexAt maps relative Y to a question option index.
func questionOptionIndexAt(panel string, relY int, q *ask.Question) (int, bool) {
	if panel == "" || q == nil || relY < 1 {
		return 0, false
	}
	lines := strings.Split(panel, "\n")
	if relY >= len(lines) {
		return 0, false
	}
	optIdx := 0
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			break
		}
		// Trailing hint.
		if strings.Contains(line, "select") && (strings.Contains(line, "Enter") || strings.Contains(line, "enter")) {
			break
		}
		if i == relY {
			if optIdx >= len(q.Options) {
				return 0, false
			}
			return optIdx, true
		}
		optIdx++
	}
	return 0, false
}
