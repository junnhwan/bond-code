package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Which-key (Phase 4.1). With the leader layer now holding ~12 bindings, this
// gives them a discoverable popup: after Ctrl+X, if no follow-up key arrives
// within whichKeyDelay, a bottom-centered panel lists every available leader
// action. Any key dismisses it and routes normally. It mirrors the
// spacemacs/emacs which-key idea without the framework weight.

const whichKeyDelay = 400 * time.Millisecond

// whichKeyShowMsg fires from the scheduleWhichKey timer; the handler only honors
// it when the user is still idling in the leader-pending state.
type whichKeyShowMsg struct{}

// scheduleWhichKey arms the popup timer. The Cmd sleeps on its goroutine; if the
// user pressed a leader key in the meantime, leaderPending is already false and
// the msg is ignored on arrival.
func scheduleWhichKey() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(whichKeyDelay)
		return whichKeyShowMsg{}
	}
}

// leaderBinding is one row of the which-key panel.
type leaderBinding struct {
	key  string
	desc string
}

// leaderBindings is the single source of truth for what Ctrl+X … does. It is
// display-only — the actual dispatch lives in handleLeaderKey — but keeping the
// labels here means the popup and the dispatch stay in the same file and review
// together.
func leaderBindings() []leaderBinding {
	return []leaderBinding{
		{"d", "diff viewer"},
		{"t", "toggle thinking"},
		{"l", "session manager"},
		{"p", "stash / pop draft"},
		{"e", "edit draft in editor"},
		{"1-9", "quick-switch session"},
		{"n", "new session"},
		{"c", "compact context"},
		{"s", "status"},
		{"g", "session timeline"},
		{"b", "toggle rail"},
		{"q", "quit / cancel"},
	}
}

// renderWhichKey builds the popup body: a titled grid of key → description rows,
// two columns wide so it stays compact at the bottom of the screen.
func (m Model) renderWhichKey() string {
	bindings := leaderBindings()
	// Two-column grid: split the list in half.
	half := (len(bindings) + 1) / 2
	left := bindings[:half]
	right := bindings[half:]
	rowW := 26
	renderCol := func(rows []leaderBinding) []string {
		out := make([]string, 0, len(rows))
		for _, b := range rows {
			key := accentStyle.Render(b.key)
			desc := dimStyle.Render(truncatePlain(b.desc, rowW-6))
			line := " " + key + strings.Repeat(" ", max(1, 6-len(b.key))) + desc
			out = append(out, line)
		}
		return out
	}
	leftLines := renderCol(left)
	rightLines := renderCol(right)
	rows := max(len(leftLines), len(rightLines))
	var b strings.Builder
	b.WriteString(overlayTitleStyle().Render(" Leader keys "))
	b.WriteString("\n")
	for i := 0; i < rows; i++ {
		l := ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		}
		// pad left column to rowW so the right column aligns
		lPad := rowW - lipgloss.Width(l)
		if lPad < 1 {
			lPad = 1
		}
		b.WriteString(l + strings.Repeat(" ", lPad) + r)
		if i < rows-1 {
			b.WriteString("\n")
		}
	}
	body := b.String()
	box := overlayBoxStyle().Render(body)
	return box
}

// blitScrollbar overlays a 1-column scrollbar on the right edge of the timeline
// block. The thumb is proportional to how much of the transcript is visible
// (slight overflow → tall thumb + short travel; long history → shorter thumb).
// scroll=0 (newest) parks the thumb at the bottom; scroll=maxScroll (oldest)
// parks it at the top.
//
// Always emits exactly `height` rows with a continuous track (pads short
// blocks, clips tall ones). Content is truncated and right-padded to width-1
// so the bar never overwrites text and never "breaks" mid-column when lines
// have uneven ANSI widths.
func blitScrollbar(block string, scroll, maxScroll, height, width int) string {
	if height <= 0 || width <= 1 {
		return block
	}
	src := strings.Split(block, "\n")
	// Normalize to exactly height rows so the track is continuous top→bottom
	// (composeBaseView may pad/clip after the initial body render).
	for len(src) < height {
		src = append(src, "")
	}
	if len(src) > height {
		src = src[len(src)-height:]
	}
	thumbTop, thumbSize := scrollbarMetrics(scroll, maxScroll, height)
	track := lipgloss.NewStyle().Foreground(DefaultTheme.Border).Render("│")
	thumb := lipgloss.NewStyle().Foreground(DefaultTheme.Accent).Render("█")
	contentW := width - 1
	out := make([]string, height)
	for i := 0; i < height; i++ {
		kept := ansi.Truncate(src[i], contentW, "")
		if pad := contentW - lipgloss.Width(kept); pad > 0 {
			kept += strings.Repeat(" ", pad)
		}
		cell := track
		if i >= thumbTop && i < thumbTop+thumbSize {
			cell = thumb
		}
		out[i] = kept + cell
	}
	return strings.Join(out, "\n")
}

// scrollbarMetrics returns the thumb's top row and height on a track of
// `height` rows. Proportional sizing: visibleFraction ≈ height/(height+maxScroll).
// A minimum grab size keeps the thumb usable even with long history.
func scrollbarMetrics(scroll, maxScroll, height int) (thumbTop, thumbSize int) {
	if height <= 0 {
		return 0, 0
	}
	if maxScroll <= 0 {
		// Nothing to scroll: fill the track so it still reads as a bar.
		return 0, height
	}
	total := height + maxScroll
	thumbSize = (height * height) / total
	minThumb := scrollbarMinThumb(height)
	if thumbSize < minThumb {
		thumbSize = minThumb
	}
	if thumbSize > height {
		thumbSize = height
	}
	travel := height - thumbSize
	if travel <= 0 {
		return 0, height
	}
	// scroll=0 (newest/bottom) → thumb at bottom; scroll=max → top.
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	thumbTop = travel * (maxScroll - scroll) / maxScroll
	return thumbTop, thumbSize
}

// scrollbarMinThumb is the smallest grab handle that still feels clickable in
// a terminal. Short viewports get a smaller floor so the track isn't all thumb.
func scrollbarMinThumb(height int) int {
	if height <= 1 {
		return 1
	}
	if height < 8 {
		return max(2, height/3)
	}
	return 3
}

// scrollbarThumbPos is the top row of the thumb (kept for call sites / tests
// that only need a single anchor position).
func scrollbarThumbPos(scroll, maxScroll, height int) int {
	top, _ := scrollbarMetrics(scroll, maxScroll, height)
	return top
}

// scrollFromScrollbarY maps a body-relative track Y (0 = top) to a scroll
// offset. The click/drag point is treated as the thumb center so a tall
// proportional thumb only needs a short travel when overflow is small.
func scrollFromScrollbarY(relY, height, maxScroll int) int {
	if maxScroll <= 0 || height <= 1 {
		return 0
	}
	_, thumbSize := scrollbarMetrics(0, maxScroll, height)
	travel := height - thumbSize
	if travel <= 0 {
		return 0
	}
	// Center the thumb on the pointer.
	thumbTop := relY - thumbSize/2
	if thumbTop < 0 {
		thumbTop = 0
	}
	if thumbTop > travel {
		thumbTop = travel
	}
	return maxScroll * (travel - thumbTop) / travel
}

// blitBottomCenter overlays block at the bottom-center of view, preserving the
// rest of the view. Mirrors blitTopRight's cell-splice approach but anchored to
// the bottom and horizontally centered.
func blitBottomCenter(view string, block string, width, height int) string {
	blockLines := strings.Split(block, "\n")
	if len(blockLines) == 0 || strings.TrimSpace(block) == "" {
		return view
	}
	blockH := len(blockLines)
	blockW := 0
	for _, l := range blockLines {
		if w := lipgloss.Width(l); w > blockW {
			blockW = w
		}
	}
	if blockW >= width {
		blockW = width
	}
	left := (width - blockW) / 2
	if left < 0 {
		left = 0
	}
	bottom := height - blockH
	if bottom < 0 {
		bottom = 0
	}
	viewLines := strings.Split(view, "\n")
	for i, bl := range blockLines {
		vy := bottom + i
		if vy < 0 || vy >= len(viewLines) {
			break
		}
		// Keep the left portion of the view line, pad to `left`, then blit the
		// block line. The block's own background covers the underlying cells.
		viewLine := viewLines[vy]
		kept := ansi.Truncate(viewLine, left, "")
		pad := left - lipgloss.Width(kept)
		if pad < 0 {
			pad = 0
		}
		viewLines[vy] = kept + strings.Repeat(" ", pad) + bl
	}
	return strings.Join(viewLines, "\n")
}
