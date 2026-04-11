package ultraplan

import (
	"regexp"
	"strings"
	"unicode"
)

type TriggerPosition struct {
	Word  string
	Start int
	End   int
}

var openToClose = map[rune]rune{
	'`':  '`',
	'"':  '"',
	'<':  '>',
	'{':  '}',
	'[':  ']',
	'(':  ')',
	'\'': '\'',
}

func FindUltraplanTriggerPositions(text string) []TriggerPosition {
	return findKeywordTriggerPositions(text, "ultraplan")
}

func FindUltrareviewTriggerPositions(text string) []TriggerPosition {
	return findKeywordTriggerPositions(text, "ultrareview")
}

func HasUltraplanKeyword(text string) bool {
	return len(FindUltraplanTriggerPositions(text)) > 0
}

func HasUltrareviewKeyword(text string) bool {
	return len(FindUltrareviewTriggerPositions(text)) > 0
}

func ReplaceUltraplanKeyword(text string) string {
	positions := FindUltraplanTriggerPositions(text)
	if len(positions) == 0 {
		return text
	}
	trigger := positions[0]
	before := text[:trigger.Start]
	after := text[trigger.End:]
	if strings.TrimSpace(before+after) == "" {
		return ""
	}
	return before + trigger.Word[len("ultra"):] + after
}

func findKeywordTriggerPositions(text string, keyword string) []TriggerPosition {
	if text == "" || strings.HasPrefix(text, "/") {
		return nil
	}
	pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(keyword) + `\b`)
	if !pattern.MatchString(text) {
		return nil
	}

	ranges := quotedRanges(text)
	positions := []TriggerPosition{}
	for _, match := range pattern.FindAllStringIndex(text, -1) {
		start, end := match[0], match[1]
		if withinRanges(start, ranges) {
			continue
		}
		before := charAt(text, start-1)
		after := charAt(text, end)
		if before == '/' || before == '\\' || before == '-' {
			continue
		}
		if after == '/' || after == '\\' || after == '-' || after == '?' {
			continue
		}
		if after == '.' && isWord(charAt(text, end+1)) {
			continue
		}
		positions = append(positions, TriggerPosition{
			Word:  text[start:end],
			Start: start,
			End:   end,
		})
	}
	return positions
}

func quotedRanges(text string) [][2]int {
	var ranges [][2]int
	var open rune
	openIndex := -1
	for index, ch := range text {
		if open != 0 {
			if open == '[' && ch == '[' {
				openIndex = index
				continue
			}
			if ch != openToClose[open] {
				continue
			}
			if open == '\'' && isWord(charAt(text, index+1)) {
				continue
			}
			ranges = append(ranges, [2]int{openIndex, index + len(string(ch))})
			open = 0
			openIndex = -1
			continue
		}
		switch {
		case ch == '<' && isTagLikeStart(text, index):
			open = ch
			openIndex = index
		case ch == '\'' && !isWord(charAt(text, index-1)):
			open = ch
			openIndex = index
		case ch != '<' && ch != '\'':
			if _, ok := openToClose[ch]; ok {
				open = ch
				openIndex = index
			}
		}
	}
	return ranges
}

func withinRanges(offset int, ranges [][2]int) bool {
	for _, r := range ranges {
		if offset >= r[0] && offset < r[1] {
			return true
		}
	}
	return false
}

func charAt(text string, index int) rune {
	if index < 0 || index >= len(text) {
		return 0
	}
	return rune(text[index])
}

func isWord(ch rune) bool {
	return ch != 0 && (unicode.IsLetter(ch) || unicode.IsNumber(ch) || ch == '_')
}

func isTagLikeStart(text string, index int) bool {
	next := charAt(text, index+1)
	return unicode.IsLetter(next) || next == '/'
}
