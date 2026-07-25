package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

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
