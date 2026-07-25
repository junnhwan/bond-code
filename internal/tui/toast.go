package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Toast notifications are transient non-blocking feedback rendered as a floating
// panel in the top-right corner. Unlike timeline entries, toasts convey
// ephemeral signals ("copied to clipboard", "model switched") without polluting
// the conversation transcript. They auto-dismiss after toastTTL.

// toastVariant controls the color and prefix glyph of a toast.
type toastVariant int

const (
	toastInfo toastVariant = iota
	toastSuccess
	toastWarn
	toastError
)

// toastTTL is how long a toast stays on screen before auto-dismissing. Long
// enough to read a short message, short enough not to clutter the corner.
const toastTTL = 2500 * time.Millisecond

// toast is one transient notification. expireAt is checked on the existing tick
// path (spinner / agentTick) so toasts need no timer of their own.
type toast struct {
	message  string
	variant  toastVariant
	expireAt time.Time
}

func newToast(message string, variant toastVariant) toast {
	return toast{
		message:  strings.TrimSpace(message),
		variant:  variant,
		expireAt: time.Now().Add(toastTTL),
	}
}

// pushToast appends a notification. Empty messages are dropped. The stack is
// capped so a burst cannot flood the corner and obscure the header.
func (m Model) pushToast(message string, variant toastVariant) Model {
	t := newToast(message, variant)
	if t.message == "" {
		return m
	}
	m.toasts = append(m.toasts, t)
	const maxToasts = 4
	if len(m.toasts) > maxToasts {
		m.toasts = m.toasts[len(m.toasts)-maxToasts:]
	}
	return m
}

// tickToasts evicts expired notifications. Called from the existing spinner /
// agentTick path so no extra timer goroutine is needed.
func (m Model) tickToasts() Model {
	if len(m.toasts) == 0 {
		return m
	}
	now := time.Now()
	kept := m.toasts[:0]
	for _, t := range m.toasts {
		if now.Before(t.expireAt) {
			kept = append(kept, t)
		}
	}
	m.toasts = kept
	return m
}

func (t toast) style() lipgloss.Style {
	base := lipgloss.NewStyle().
		Padding(0, 1).
		Background(DefaultTheme.BackgroundPanel)
	switch t.variant {
	case toastSuccess:
		return base.Foreground(DefaultTheme.Success)
	case toastWarn:
		return base.Foreground(DefaultTheme.Warning)
	case toastError:
		return base.Foreground(DefaultTheme.Error)
	default:
		return base.Foreground(DefaultTheme.Text)
	}
}

func (t toast) glyph() string {
	switch t.variant {
	case toastSuccess:
		return "✓"
	case toastWarn:
		return "⚠"
	case toastError:
		return "✗"
	default:
		return "·"
	}
}

// renderToasts lays out the toast stack as a right-aligned multi-line block.
// Each line is a solid panel (own background) so the underlying view is cleanly
// covered where the toast sits. Near expiry the text dims (fade-out).
func renderToasts(toasts []toast, width int) string {
	if len(toasts) == 0 {
		return ""
	}
	if width < 14 {
		width = 14
	}
	maxW := width - 2
	now := time.Now()
	var lines []string
	for _, t := range toasts {
		msg := truncatePlain(t.message, max(8, maxW-4))
		remaining := t.expireAt.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		// Near expiry, soft-fade by switching to muted/dim glyphs+text.
		style := t.style()
		ratio := 1.0
		if toastTTL > 0 {
			ratio = float64(remaining) / float64(toastTTL)
		}
		switch {
		case ratio < 0.15:
			style = style.Foreground(DefaultTheme.Dim)
		case ratio < 0.40:
			style = style.Foreground(DefaultTheme.TextMuted)
		}
		lines = append(lines, style.Render(t.glyph()+" "+msg))
	}
	return strings.Join(lines, "\n")
}

// blitTopRight overlays block (possibly multi-line) over the top-right corner
// of view, preserving the rest of the view's content. Each block line replaces
// the trailing cells of the corresponding view line; the leading cells keep the
// original (ANSI-styled) content via ansi.Truncate. This gives a true floating
// effect without a compositing buffer.
func blitTopRight(view string, block string, width int) string {
	blockLines := strings.Split(block, "\n")
	if len(blockLines) == 0 || strings.TrimSpace(block) == "" {
		return view
	}
	viewLines := strings.Split(view, "\n")
	for i, bl := range blockLines {
		if i >= len(viewLines) {
			break
		}
		blockW := lipgloss.Width(bl)
		if blockW <= 0 || blockW >= width {
			continue
		}
		kept := width - blockW
		viewLine := viewLines[i]
		truncated := ansi.Truncate(viewLine, kept, "")
		pad := kept - lipgloss.Width(truncated)
		if pad < 0 {
			pad = 0
		}
		viewLines[i] = truncated + strings.Repeat(" ", pad) + bl
	}
	return strings.Join(viewLines, "\n")
}
