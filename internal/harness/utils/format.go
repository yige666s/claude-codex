package utils

import (
	"fmt"
	"math"
	"strings"
)

func FormatFileSize(sizeInBytes int64) string {
	kb := float64(sizeInBytes) / 1024
	if kb < 1 {
		return fmt.Sprintf("%d bytes", sizeInBytes)
	}
	if kb < 1024 {
		return trimTrailingZero(fmt.Sprintf("%.1fKB", kb))
	}
	mb := kb / 1024
	if mb < 1024 {
		return trimTrailingZero(fmt.Sprintf("%.1fMB", mb))
	}
	gb := mb / 1024
	return trimTrailingZero(fmt.Sprintf("%.1fGB", gb))
}

func FormatSecondsShort(ms float64) string {
	return fmt.Sprintf("%.1fs", ms/1000)
}

type DurationFormatOptions struct {
	HideTrailingZeros   bool
	MostSignificantOnly bool
}

func FormatDuration(ms int64, options DurationFormatOptions) string {
	if ms < 60000 {
		if ms == 0 {
			return "0s"
		}
		if ms < 1000 {
			return fmt.Sprintf("%.1fs", float64(ms)/1000)
		}
		return fmt.Sprintf("%ds", ms/1000)
	}

	days := ms / 86400000
	hours := (ms % 86400000) / 3600000
	minutes := (ms % 3600000) / 60000
	seconds := int64(math.Round(float64(ms%60000) / 1000))

	if seconds == 60 {
		seconds = 0
		minutes++
	}
	if minutes == 60 {
		minutes = 0
		hours++
	}
	if hours == 24 {
		hours = 0
		days++
	}

	if options.MostSignificantOnly {
		switch {
		case days > 0:
			return fmt.Sprintf("%dd", days)
		case hours > 0:
			return fmt.Sprintf("%dh", hours)
		case minutes > 0:
			return fmt.Sprintf("%dm", minutes)
		default:
			return fmt.Sprintf("%ds", seconds)
		}
	}

	if days > 0 {
		if options.HideTrailingZeros && hours == 0 && minutes == 0 {
			return fmt.Sprintf("%dd", days)
		}
		if options.HideTrailingZeros && minutes == 0 {
			return fmt.Sprintf("%dd %dh", days, hours)
		}
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		if options.HideTrailingZeros && minutes == 0 && seconds == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		if options.HideTrailingZeros && seconds == 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if options.HideTrailingZeros && seconds == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}

func trimTrailingZero(value string) string {
	value = strings.Replace(value, ".0KB", "KB", 1)
	value = strings.Replace(value, ".0MB", "MB", 1)
	value = strings.Replace(value, ".0GB", "GB", 1)
	return value
}
