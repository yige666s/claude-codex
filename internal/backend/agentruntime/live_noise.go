package agentruntime

import (
	"strings"
	"unicode"
)

func liveCompactTranscriptNoiseText(text string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func liveTranscriptNoiseContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func liveTranscriptNoiseIsExtendedFiller(compact, filler string) bool {
	if compact == "" || filler == "" {
		return false
	}
	if compact == filler {
		return true
	}
	if strings.HasPrefix(compact, filler) && liveTranscriptNoiseAllRunes(compact[len(filler):], []rune(filler)[len([]rune(filler))-1]) {
		return true
	}
	if len(compact) > len(filler) && strings.ReplaceAll(compact, filler, "") == "" {
		return true
	}
	return false
}

func liveTranscriptNoiseAllRunes(text string, want rune) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r != want {
			return false
		}
	}
	return true
}
