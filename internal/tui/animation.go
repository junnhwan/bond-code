package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func mathCos(x float64) float64 { return math.Cos(x) }

// Lightweight TUI animation helpers. Driven by spinner.Tick → animFrame.
// Only dock / live overlay / toast / flash may read animFrame — never put it
// into committed timeline cache keys (streaming performance guardrails).

const (
	// flashDuration is how long a click "pop" stays visible.
	flashDuration = 180 * time.Millisecond
	// animWavePeriod is frames per full accent wave cycle (~braille spinner len).
	animWavePeriod = 8
)

// brailleSpinner is a smooth Grok-like activity glyph sequence.
var brailleSpinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// accentWaveBars are left-edge bars that "travel" while thinking/running.
var accentWaveBars = []string{"▏", "▎", "▍", "▌", "▋", "▊", "▋", "▌", "▍", "▎"}

// uiFlash is a short-lived click feedback pulse on an interactive surface.
type uiFlash struct {
	kind  mouseHitKind
	until time.Time
}

func (f uiFlash) active() bool {
	return !f.until.IsZero() && time.Now().Before(f.until)
}

func newUIFlash(kind mouseHitKind) uiFlash {
	if kind == mouseHitNone {
		return uiFlash{}
	}
	return uiFlash{kind: kind, until: time.Now().Add(flashDuration)}
}

// animSpinnerFrame returns a braille spinner glyph for the frame counter.
func animSpinnerFrame(frame int) string {
	if len(brailleSpinner) == 0 {
		return "·"
	}
	if frame < 0 {
		frame = 0
	}
	return brailleSpinner[frame%len(brailleSpinner)]
}

// animAccentBar returns a traveling left accent bar glyph.
func animAccentBar(frame int) string {
	if len(accentWaveBars) == 0 {
		return "▏"
	}
	if frame < 0 {
		frame = 0
	}
	return accentWaveBars[frame%len(accentWaveBars)]
}

// animActivityDots appends 1–3 dots that cycle for "thinking…" motion.
func animActivityDots(frame int) string {
	n := (frame % 3) + 1
	return strings.Repeat(".", n)
}

// animPulseOn is true on alternating half-periods — soft blink for wait state.
func animPulseOn(frame int) bool {
	return (frame/2)%2 == 0
}

// animAccentStyle pulses brand accent brightness for live/waiting chrome.
func animAccentStyle(frame int) lipgloss.Style {
	if animPulseOn(frame) {
		return accentStyle
	}
	return lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted).Bold(true)
}

// animDimRamp returns a style that fades from full → muted as remaining/total
// approaches 0 (toast fade-out). remaining and total are durations.
func animDimRamp(remaining, total time.Duration) lipgloss.Style {
	if total <= 0 || remaining >= total {
		return lipgloss.NewStyle().Foreground(DefaultTheme.Text)
	}
	// Last 40% of life: switch to muted; last 15%: dim.
	ratio := float64(remaining) / float64(total)
	switch {
	case ratio < 0.15:
		return lipgloss.NewStyle().Foreground(DefaultTheme.Dim)
	case ratio < 0.40:
		return lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted)
	default:
		return lipgloss.NewStyle().Foreground(DefaultTheme.Text)
	}
}

// FormatTurnStatusRowAnimated is FormatTurnStatusRow with a traveling accent
// bar + braille spinner (Grok busy row language).
func FormatTurnStatusRowAnimated(frame int, activity, elapsed string, width int) string {
	if width < 1 {
		width = 1
	}
	activity = strings.TrimSpace(activity)
	if activity == "" {
		activity = "working"
	}
	// Soft trailing dots on activity so the row feels alive even when the
	// detail string is static ("thinking" → "thinking...").
	if !strings.Contains(activity, ".") {
		activity = activity + animActivityDots(frame)
	}
	bar := animAccentStyle(frame).Render(animAccentBar(frame))
	spin := animAccentStyle(frame).Render(animSpinnerFrame(frame))
	left := strings.TrimSpace(bar + " " + spin + " " + busyStyle.Render(activity))
	right := dimStyle.Render(strings.TrimSpace(elapsed))
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return truncateStyled(left, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// animDockSeparator paints a subtle traveling highlight on the rule between
// scrollback and the prompt stack while the agent is busy.
func animDockSeparator(width, frame int, busy bool) string {
	if width < 1 {
		width = 1
	}
	if !busy {
		return dimStyle.Render(strings.Repeat("\u2500", width))
	}
	// One brighter cell travels along the rule.
	pos := frame % width
	var b strings.Builder
	b.Grow(width * 8)
	for i := 0; i < width; i++ {
		if i == pos || i == (pos+1)%width {
			b.WriteString(accentStyle.Render("\u2500"))
		} else {
			b.WriteString(dimStyle.Render("\u2500"))
		}
	}
	return b.String()
}

// animPermissionBar returns the left takeover bar, pulsing when active.
func animPermissionBar(frame int) string {
	style := confirmStyle
	if animPulseOn(frame) {
		style = accentStyle
	}
	return style.Render("\u258e ") // ▎
}

// needsAnimationTick reports whether the spinner should keep firing so chrome
// animations (busy, hover pulse, flash, toast fade, welcome shimmer) continue.
func (m Model) needsAnimationTick() bool {
	if m.agent.Busy || m.agent.Pending != nil || m.question != nil {
		return true
	}
	if len(m.toasts) > 0 {
		return true
	}
	if m.flash.active() {
		return true
	}
	if m.hover.kind != mouseHitNone {
		return true
	}
	// Empty welcome: keep logo shimmer alive (Grok ~12fps sheen).
	if len(m.timeline.Turns) == 0 {
		return true
	}
	return false
}

// blendHex mixes two #RRGGBB colors by t in [0,1] toward hi.
func blendHex(lo, hi lipgloss.Color, t float64) lipgloss.Color {
	if t <= 0 {
		return lo
	}
	if t >= 1 {
		return hi
	}
	lr, lg, lb := parseHexRGB(string(lo))
	hr, hg, hb := parseHexRGB(string(hi))
	r := uint8(float64(lr) + (float64(hr)-float64(lr))*t)
	g := uint8(float64(lg) + (float64(hg)-float64(lg))*t)
	b := uint8(float64(lb) + (float64(hb)-float64(lb))*t)
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
}

func parseHexRGB(s string) (r, g, b uint8) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return 108, 108, 108
	}
	var rv, gv, bv int
	_, _ = fmt.Sscanf(s, "%02x%02x%02x", &rv, &gv, &bv)
	return uint8(rv), uint8(gv), uint8(bv)
}

// shimmerWord styles an entire word as one span (keeps "BOND" contiguous for
// search/select while still sweeping brightness with frame).
func shimmerWord(word string, row, rows, frame int) string {
	if word == "" {
		return word
	}
	// Sample sheen at the word's midpoint column.
	mid := len([]rune(word)) / 2
	t := shimmerT(mid, len([]rune(word)), row, rows, frame)
	color := blendHex(DefaultTheme.Dim, DefaultTheme.Text, t)
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(word)
}

func shimmerT(col, cols, row, rows, frame int) float64 {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	const period = 48
	const band = 0.38
	p := float64(frame%period) / period
	q := p / 0.32
	if q > 1 {
		q = 1
	}
	bandPos := -band + q*(1+2*band)
	pulse := 0.06 * (0.5 - 0.5*mathCos(float64(frame)*0.05))
	diag := (float64(col) + float64(rows-1-row)) / float64(cols+rows)
	d := diag - bandPos
	if d < 0 {
		d = -d
	}
	shine := 0.0
	if d < band {
		shine = 0.5 * (1 + mathCos(3.14159265*d/band))
	}
	t := pulse + 0.33*shine
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return t
}

// shimmerLine recolors a plain logo line with a diagonal sheen (Grok logo
// language). frame advances on spinner ticks (~10–12 Hz effective).
func shimmerLine(plain string, row, rows, frame int) string {
	if plain == "" {
		return plain
	}
	runes := []rune(plain)
	cols := len(runes)
	if cols == 0 || rows < 1 {
		return plain
	}
	var b strings.Builder
	b.Grow(len(plain) * 12)
	var run strings.Builder
	var runT float64
	var runHas bool
	flush := func() {
		if !runHas || run.Len() == 0 {
			return
		}
		color := blendHex(DefaultTheme.Dim, DefaultTheme.Text, runT)
		b.WriteString(lipgloss.NewStyle().Foreground(color).Bold(true).Render(run.String()))
		run.Reset()
		runHas = false
	}
	for col, ch := range runes {
		if ch == ' ' {
			flush()
			b.WriteRune(ch)
			continue
		}
		t := shimmerT(col, cols, row, rows, frame)
		// Quantize so adjacent glyphs share spans (cheaper + keeps runs).
		tq := float64(int(t*20)) / 20
		if runHas && tq != runT {
			flush()
		}
		if !runHas {
			runT = tq
			runHas = true
		}
		run.WriteRune(ch)
	}
	flush()
	return b.String()
}

// ensureAnimTick starts a spinner.Tick chain when chrome needs frames and no
// chain is already in flight. Callers that already schedule m.spinner.Tick
// (Init, startAgent, runAgent) must set animTickArmed themselves.
//
// Never call m.spinner.Tick on every mouse motion while Busy — concurrent
// timers amplify animFrame and make the thinking spinner race (bug).
func (m Model) ensureAnimTick() (Model, tea.Cmd) {
	if !m.needsAnimationTick() {
		m.animTickArmed = false
		return m, nil
	}
	if m.animTickArmed {
		return m, nil
	}
	m.animTickArmed = true
	return m, m.spinner.Tick
}
