package tui

import "strings"

// bottomDock holds the vertically stacked controls below the transcript.
// Grok agent stack order (bottom of screen upward):
//
//	scrollback → turn_status (busy only) → queued/suggestions →
//	permission|question|composer → shortcuts footer
//
// The measured form reserves layout height; renderBottomDock rebuilds
// width-sensitive components for the final timeline column.
type bottomDock struct {
	promptVisible bool
	composerH     int
	separator     string
	turnStatus    string
	permission    string
	question      string
	suggestions   string
	queued        string
	agentBar      string
	composer      string
}

func (d bottomDock) componentHeight() int {
	return d.composerH + renderedHeight(d.separator) + renderedHeight(d.turnStatus) +
		renderedHeight(d.permission) + renderedHeight(d.question) + renderedHeight(d.suggestions) +
		renderedHeight(d.queued) + renderedHeight(d.agentBar)
}

func (d bottomDock) reservedHeight() int { return d.componentHeight() }

func (d bottomDock) parts(footer string) []string {
	// Stack order matches Grok agent view: rule → turn status → prompt → shortcuts.
	parts := make([]string, 0, 10)
	// Separator only when something sits below the transcript (always true in practice).
	hasDock := d.turnStatus != "" || d.queued != "" || d.suggestions != "" ||
		d.agentBar != "" || d.permission != "" || d.question != "" || d.composer != "" || footer != ""
	if hasDock {
		// Width is applied later via truncate in render; use a generous rule here
		// and let composeBaseView / truncateStyled fit. Actual width-aware rule
		// is injected in renderBottomDock.
		if d.separator != "" {
			parts = append(parts, d.separator)
		}
	}
	for _, part := range []string{
		d.turnStatus,
		d.queued,
		d.suggestions,
		d.agentBar,
		d.permission,
		d.question,
		d.composer,
		footer,
	} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func (m Model) measureBottomDock() bottomDock {
	// Separator only while busy/waiting (animated). Idle sessions use the
	// composer's soft prompt_border rule alone — matching Grok's single chrome
	// edge above the prompt (avoids double ── lines).
	busyChrome := m.agent.Busy || m.agent.Pending != nil || m.question != nil
	sep := ""
	if busyChrome {
		sep = animDockSeparator(m.width, m.animFrame, true)
	}
	dock := bottomDock{
		promptVisible: m.promptVisible(),
		separator:     sep,
		queued:        m.queuedView(),
		turnStatus:    m.renderTurnStatusLine(m.width),
		permission: renderPermissionPanel(
			m.agent.Pending,
			m.agent.ConfirmChoice,
			m.agent.ConfirmEnteringReject,
			m.agent.ConfirmRejectReason,
			m.alwaysAvailable(),
			m.width,
			m.animFrame,
		),
	}
	if dock.promptVisible {
		dock.composerH = m.composerHeight()
		if m.composer.Suggestions != nil && m.composer.Suggestions.IsVisible() {
			dock.suggestions = m.renderSuggestions()
		}
	}
	if m.agent.Pending == nil {
		dock.question = renderQuestionPanel(m.question, m.questionCursor, m.questionSelected, m.width)
	}
	// Multi-agent switcher only when user is in agent focus (not primary chrome).
	dock.agentBar = m.agentBarView()
	return dock
}

func (m Model) renderBottomDock(measured bottomDock, layout LayoutState) bottomDock {
	dock := measured
	busyChrome := m.agent.Busy || m.agent.Pending != nil || m.question != nil
	if busyChrome {
		dock.separator = animDockSeparator(layout.TimelineW, m.animFrame, true)
	} else {
		dock.separator = ""
	}
	dock.turnStatus = m.renderTurnStatusLine(layout.TimelineW)
	dock.queued = m.queuedViewForWidth(layout.TimelineW)
	if measured.promptVisible && measured.suggestions != "" {
		dock.suggestions = m.renderSuggestionsForWidth(layout.TimelineW)
	} else {
		dock.suggestions = ""
	}
	dock.permission = renderPermissionPanel(
		m.agent.Pending,
		m.agent.ConfirmChoice,
		m.agent.ConfirmEnteringReject,
		m.agent.ConfirmRejectReason,
		m.alwaysAvailable(),
		layout.TimelineW,
		m.animFrame,
	)
	if m.agent.Pending == nil {
		dock.question = renderQuestionPanel(m.question, m.questionCursor, m.questionSelected, layout.TimelineW)
	} else {
		dock.question = ""
	}
	if measured.agentBar != "" {
		dock.agentBar = m.agentBarViewForWidth(layout.TimelineW)
	} else {
		dock.agentBar = ""
	}
	if measured.promptVisible {
		dock.composer = m.composerViewForWidth(layout.TimelineW)
	}
	dock.composerH = renderedHeight(dock.composer)

	optionalH := layout.ComposerH - renderedHeight(dock.composer) - renderedHeight(dock.agentBar) - renderedHeight(dock.turnStatus)
	if optionalH < 0 {
		optionalH = 0
	}
	if dock.suggestions != "" {
		if optionalH <= 0 {
			dock.suggestions = ""
		} else {
			dock.suggestions = m.renderSuggestionsForWidthHeight(layout.TimelineW, optionalH)
			optionalH -= renderedHeight(dock.suggestions)
		}
	}
	if dock.queued != "" {
		if optionalH <= 0 {
			dock.queued = ""
		} else {
			dock.queued = fitRenderedBlockHeight(dock.queued, optionalH, layout.TimelineW)
			optionalH -= renderedHeight(dock.queued)
		}
	}
	fixedH := renderedHeight(dock.turnStatus) + renderedHeight(dock.queued) + renderedHeight(dock.suggestions) +
		renderedHeight(dock.agentBar) + renderedHeight(dock.composer)
	if dock.permission != "" {
		dock.permission = fitRenderedBlockHeight(dock.permission, layout.ComposerH-fixedH-renderedHeight(dock.question), layout.TimelineW)
	}
	if dock.question != "" {
		availableH := layout.ComposerH - fixedH - renderedHeight(dock.permission)
		dock.question = renderQuestionPanelForHeight(m.question, m.questionCursor, m.questionSelected, layout.TimelineW, availableH)
		dock.question = fitRenderedBlockHeight(dock.question, availableH, layout.TimelineW)
	}
	return dock
}

// composeBaseView pins the bottom dock (prompt / permission / shortcuts) and
// gives the transcript a pure scroll window — Grok's "middle pane scrolls,
// prompt stays put". Never use "... +N more lines" on the main body.
func (m Model) composeBaseView(body string, dock bottomDock, footer string, layout LayoutState) string {
	dockKeys := []struct {
		key  string
		text string
	}{
		{"separator", dock.separator},
		{"turnStatus", dock.turnStatus},
		{"queued", dock.queued},
		{"suggestions", dock.suggestions},
		{"agent", dock.agentBar},
		{"permission", dock.permission},
		{"question", dock.question},
		{"composer", dock.composer},
		{"footer", footer},
	}
	dockH := 0
	for _, d := range dockKeys {
		if d.text != "" {
			dockH += renderedHeight(d.text)
		}
	}

	// Very short terminals: keep interaction surfaces by priority, clip body
	// with hard line cuts (no "+N more lines" ellipsis).
	if m.height < 1 {
		return ""
	}
	if dockH >= m.height {
		return m.composeBaseViewTight(body, dock, footer, layout)
	}

	bodyH := m.height - dockH
	if bodyH < 1 {
		bodyH = 1
	}
	// Prefer layout.TimelineH when it fits so scroll math, hit-testing, and
	// the painted track share one height (avoids a "broken" half-height bar).
	if layout.TimelineH > 0 && layout.TimelineH <= bodyH {
		bodyH = layout.TimelineH
	}
	// rawBody is the unpainted transcript; paintBody always starts from it so
	// emergency height shrinks never double-blit the scrollbar column.
	rawBody := body
	paintBody := func(h int) string {
		b := fitBodyWindow(rawBody, h)
		if m.scrollbarVisible(layout) {
			paintLayout := layout
			paintLayout.TimelineH = h
			b = blitScrollbar(b, m.scroll, m.maxScroll(paintLayout), h, layout.TimelineW)
		}
		return b
	}
	body = paintBody(bodyH)

	parts := []string{body}
	for _, d := range dockKeys {
		if d.text != "" {
			parts = append(parts, d.text)
		}
	}
	view := strings.Join(parts, "\n")
	for renderedHeight(view) > m.height && bodyH > 0 {
		bodyH--
		if bodyH == 0 {
			// Drop body entirely; keep dock only.
			parts = nil
			for _, d := range dockKeys {
				if d.text != "" {
					parts = append(parts, d.text)
				}
			}
			view = strings.Join(parts, "\n")
			break
		}
		body = paintBody(bodyH)
		parts = []string{body}
		for _, d := range dockKeys {
			if d.text != "" {
				parts = append(parts, d.text)
			}
		}
		view = strings.Join(parts, "\n")
	}
	// Still over? Trim dock from the top (separator first) — never ellipsis body.
	for renderedHeight(view) > m.height && len(parts) > 1 {
		parts = parts[1:]
		view = strings.Join(parts, "\n")
	}
	_ = layout
	return view
}

// fitBodyWindow hard-clips a scroll window to maxHeight lines (no ellipsis).
func fitBodyWindow(body string, maxHeight int) string {
	if maxHeight < 1 {
		return ""
	}
	lines := strings.Split(body, "\n")
	if len(lines) > maxHeight {
		// Prefer newest lines at the bottom of the window.
		lines = lines[len(lines)-maxHeight:]
	}
	for len(lines) < maxHeight {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// composeBaseViewTight keeps footer + primary control when the terminal is
// shorter than the full dock stack.
func (m Model) composeBaseViewTight(body string, dock bottomDock, footer string, layout LayoutState) string {
	remaining := m.height
	allocations := make(map[string]string, 10)
	allocate := func(key, value string, fit func(string, int, int) string) {
		if value == "" || remaining <= 0 {
			return
		}
		height := renderedHeight(value)
		if height <= remaining {
			allocations[key] = value
			remaining -= height
			return
		}
		allocations[key] = fit(value, remaining, layout.TimelineW)
		remaining = 0
	}
	// Body: hard clip, never "+N more lines".
	fitBody := func(value string, height, _ int) string {
		return fitBodyWindow(value, height)
	}
	// Dock panels may still use structural fit (permission options stay visible).
	fitBlock := func(value string, height, width int) string {
		return fitRenderedBlockHeight(value, height, width)
	}

	allocate("footer", footer, fitBlock)
	switch {
	case dock.permission != "":
		allocate("permission", dock.permission, fitBlock)
	case dock.question != "":
		allocate("question", dock.question, fitBlock)
	case dock.composer != "":
		allocate("composer", dock.composer, m.fitComposerBlockHeight)
	}
	allocate("turnStatus", dock.turnStatus, fitBlock)
	if dock.agentBar != "" {
		allocate("agent", dock.agentBar, fitBlock)
	}
	allocate("body", body, fitBody)
	allocate("suggestions", dock.suggestions, fitBlock)
	allocate("queued", dock.queued, fitBlock)
	if dock.separator != "" && remaining > 0 {
		allocations["separator"] = dock.separator
		remaining--
	}

	ordered := make([]string, 0, 11)
	for _, key := range []string{"body", "separator", "turnStatus", "queued", "suggestions", "agent", "permission", "question", "composer", "footer"} {
		if value := allocations[key]; value != "" {
			ordered = append(ordered, value)
		}
	}
	return strings.Join(ordered, "\n")
}

// fitComposerBlockHeight keeps the active ❯ input row whenever a short terminal
// cannot show the full prompt chrome. Prefers the input line, then info line.
func (m Model) fitComposerBlockHeight(value string, maxHeight int, width int) string {
	if value == "" || maxHeight < 1 {
		return ""
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= maxHeight {
		return value
	}

	// Prefer the last non-empty line cluster (input + optional info).
	inputRow := 0
	if m.promptAttachmentsLine() != "" {
		inputRow = 1
	}
	if inputRow >= len(lines) {
		inputRow = max(0, len(lines)-1)
	}

	selected := make([]bool, len(lines))
	selected[inputRow] = true
	used := 1
	// Keep trailing info line when present.
	if last := len(lines) - 1; last != inputRow && used < maxHeight {
		selected[last] = true
		used++
	}
	for index := range lines {
		if used >= maxHeight {
			break
		}
		if selected[index] {
			continue
		}
		selected[index] = true
		used++
	}

	out := make([]string, 0, maxHeight)
	for index, line := range lines {
		if selected[index] {
			out = append(out, truncateStyled(line, width))
		}
	}
	return strings.Join(out, "\n")
}

func (m Model) renderMainBody(layout LayoutState) string {
	if m.focus == FocusAgentWindow {
		return m.agentWindowView(layout.TimelineH, layout.TimelineW)
	}

	tp := m.prof.phase("timeline")
	body := m.renderWorkspaceTimeline(layout)
	tp.done()
	// Scrollbar is painted in composeBaseView after the final body height fit
	// so the track stays continuous (no mid-dock re-clip gaps).
	return body
}

// scrollbarVisible reports whether the transcript right-edge track should
// paint. Preference force-on always shows it; otherwise it appears only when
// the transcript overflows the viewport (so drag-to-scroll is available when
// needed without a permanent chrome tax on short sessions).
func (m Model) scrollbarVisible(layout LayoutState) bool {
	if m.focus == FocusAgentWindow {
		return false
	}
	if m.showScrollbar {
		return true
	}
	return m.maxScroll(layout) > 0
}

func (m Model) composeFloatingLayers(view string) string {
	needsHeightFit := false
	// Modal overlays replace the base view with one centered interaction.
	if m.overlay.active() {
		view = m.renderOverlay()
		needsHeightFit = true
	}
	// Toasts remain visible over the base view and every modal layer.
	if len(m.toasts) > 0 {
		view = blitTopRight(view, renderToasts(m.toasts, m.width), m.width)
		needsHeightFit = true
	}
	if needsHeightFit && renderedHeight(view) > m.height {
		view = fitRenderedBlockHeight(view, m.height, m.width)
	}
	return fitViewToTerminal(view, m.width)
}
