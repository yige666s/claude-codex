package memdir

import (
	"fmt"
	"math"
	"time"
)

// MemoryAgeDays returns days elapsed since mtime
// Floor-rounded — 0 for today, 1 for yesterday, 2+ for older
// Negative inputs (future mtime, clock skew) clamp to 0
func MemoryAgeDays(mtimeMs int64) int {
	elapsed := time.Now().UnixMilli() - mtimeMs
	days := int(math.Floor(float64(elapsed) / 86400000.0))
	if days < 0 {
		return 0
	}
	return days
}

// MemoryAge returns human-readable age string
// Models are poor at date arithmetic — a raw ISO timestamp doesn't trigger
// staleness reasoning the way "47 days ago" does
func MemoryAge(mtimeMs int64) string {
	d := MemoryAgeDays(mtimeMs)
	if d == 0 {
		return "today"
	}
	if d == 1 {
		return "yesterday"
	}
	return fmt.Sprintf("%d days ago", d)
}

// MemoryFreshnessText returns plain-text staleness caveat for memories >1 day old
// Returns empty string for fresh (today/yesterday) memories
func MemoryFreshnessText(mtimeMs int64) string {
	d := MemoryAgeDays(mtimeMs)
	if d <= 1 {
		return ""
	}
	return fmt.Sprintf(
		"This memory is %d days old. "+
			"Memories are point-in-time observations, not live state — "+
			"claims about code behavior or file:line citations may be outdated. "+
			"Verify against current code before asserting as fact.",
		d,
	)
}

// MemoryFreshnessNote returns per-memory staleness note wrapped in <system-reminder> tags
// Returns empty string for memories ≤ 1 day old
func MemoryFreshnessNote(mtimeMs int64) string {
	text := MemoryFreshnessText(mtimeMs)
	if text == "" {
		return ""
	}
	return fmt.Sprintf("<system-reminder>%s</system-reminder>\n", text)
}
