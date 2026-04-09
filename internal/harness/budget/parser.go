package budget

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	// Shorthand patterns: +500k, +2m, +1b (case insensitive)
	shorthandStartRE = regexp.MustCompile(`(?i)^\s*\+(\d+(?:\.\d+)?)\s*(k|m|b)\b`)
	shorthandEndRE   = regexp.MustCompile(`(?i)\s\+(\d+(?:\.\d+)?)\s*(k|m|b)\s*[.!?]?\s*$`)

	// Verbose pattern: use 2M tokens, spend 500k tokens (case insensitive)
	verboseRE = regexp.MustCompile(`(?i)\b(?:use|spend)\s+(\d+(?:\.\d+)?)\s*(k|m|b)\s*tokens?\b`)
)

var multipliers = map[string]int{
	"k": 1_000,
	"m": 1_000_000,
	"b": 1_000_000_000,
}

// BudgetPosition represents a position in text where a budget was found.
type BudgetPosition struct {
	Start int
	End   int
}

// ParseTokenBudget extracts a token budget from text.
// Returns the budget in tokens, or 0 if no budget found.
func ParseTokenBudget(text string) int {
	// Try shorthand at start
	if match := shorthandStartRE.FindStringSubmatch(text); match != nil {
		return parseBudgetMatch(match[1], match[2])
	}

	// Try shorthand at end
	if match := shorthandEndRE.FindStringSubmatch(text); match != nil {
		return parseBudgetMatch(match[1], match[2])
	}

	// Try verbose pattern
	if match := verboseRE.FindStringSubmatch(text); match != nil {
		return parseBudgetMatch(match[1], match[2])
	}

	return 0
}

// FindTokenBudgetPositions finds all positions in text where budgets are specified.
func FindTokenBudgetPositions(text string) []BudgetPosition {
	var positions []BudgetPosition

	// Check shorthand at start
	if match := shorthandStartRE.FindStringSubmatchIndex(text); match != nil {
		offset := match[0] + len(text[match[0]:match[1]]) - len(strings.TrimLeft(text[match[0]:match[1]], " \t"))
		positions = append(positions, BudgetPosition{
			Start: offset,
			End:   match[1],
		})
	}

	// Check shorthand at end
	if match := shorthandEndRE.FindStringSubmatchIndex(text); match != nil {
		// +1 to skip leading whitespace captured by regex
		endStart := match[0] + 1
		// Avoid double-counting when input is just "+500k"
		alreadyCovered := false
		for _, p := range positions {
			if endStart >= p.Start && endStart < p.End {
				alreadyCovered = true
				break
			}
		}
		if !alreadyCovered {
			positions = append(positions, BudgetPosition{
				Start: endStart,
				End:   match[1],
			})
		}
	}

	// Find all verbose patterns
	matches := verboseRE.FindAllStringSubmatchIndex(text, -1)
	for _, match := range matches {
		positions = append(positions, BudgetPosition{
			Start: match[0],
			End:   match[1],
		})
	}

	return positions
}

// parseBudgetMatch parses a budget value and suffix into tokens.
func parseBudgetMatch(value, suffix string) int {
	val, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}

	multiplier := multipliers[strings.ToLower(suffix)]
	return int(val * float64(multiplier))
}
