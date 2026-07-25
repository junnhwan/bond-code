package tui

import (
	"strings"
	"unicode"
)

// titleSpinnerFrames are braille-ish spinner glyphs for the window title.
// Advanced slowly via spinner ticks so PowerShell/Windows Terminal tabs do not thrash.
var titleSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// titleSpinnerDivisor holds each title glyph for this many spinner ticks.
// At ~10–15 spinner fps this is roughly one title update every half-second.
const titleSpinnerDivisor = 8

// composeTerminalTitle builds the status-aware terminal/tab title.
//
//	idle:            BondCode · project
//	busy:            ⠋ activity · BondCode · project
//	action required: ⚠ Action Required · BondCode · project
//
// spinnerFrame is the raw tick counter; the glyph index is spinnerFrame/titleSpinnerDivisor
// so callers can increment every tick without changing the composed title each time.
func composeTerminalTitle(project, activity string, busy, actionRequired bool, spinnerFrame int) string {
	parts := make([]string, 0, 4)
	if actionRequired {
		parts = append(parts, "⚠ Action Required")
	} else if busy {
		frame := titleSpinnerFrames[0]
		if len(titleSpinnerFrames) > 0 {
			glyphIdx := spinnerFrame / titleSpinnerDivisor
			frame = titleSpinnerFrames[modNonNeg(glyphIdx, len(titleSpinnerFrames))]
		}
		label := strings.TrimSpace(activity)
		if label == "" {
			label = "working"
		}
		label = truncateRunes(label, 40)
		parts = append(parts, frame+" "+label)
	}
	parts = append(parts, "BondCode")
	project = strings.TrimSpace(project)
	if project != "" && project != "." {
		parts = append(parts, project)
	}
	return sanitizeTitle(strings.Join(parts, " · "))
}

func modNonNeg(i, n int) int {
	if n <= 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

func truncateRunes(s string, max int) string {
	if max < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func sanitizeTitle(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
