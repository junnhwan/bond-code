package memory

import (
	"fmt"
	"math"
	"time"
)

// AgeDays returns whole days since mtime (0 = today). Negative/future clamps to 0.
func AgeDays(mtimeMs int64) int {
	if mtimeMs <= 0 {
		return 0
	}
	d := math.Floor(float64(time.Now().UnixMilli()-mtimeMs) / 86_400_000)
	if d < 0 {
		return 0
	}
	return int(d)
}

// AgeText is a human-readable age. Models reason better about "47 days ago"
// than raw ISO timestamps (CC memoryAge.ts).
func AgeText(mtimeMs int64) string {
	d := AgeDays(mtimeMs)
	switch d {
	case 0:
		return "today"
	case 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", d)
	}
}

// FreshnessText returns a staleness caveat for memories older than 1 day.
// Empty for today/yesterday (noise otherwise).
func FreshnessText(mtimeMs int64) string {
	d := AgeDays(mtimeMs)
	if d <= 1 {
		return ""
	}
	return fmt.Sprintf(
		"This memory is %d days old. Memories are point-in-time observations, not live state — "+
			"claims about code behavior or file:line citations may be outdated. "+
			"Verify against current code before asserting as fact.",
		d,
	)
}
