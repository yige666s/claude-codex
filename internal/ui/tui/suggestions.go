package tui

import (
	"strings"
)

// CommandSuggestion represents a command suggestion with its match score
type CommandSuggestion struct {
	Command Command
	Score   int
}

// GenerateCommandSuggestions returns matching commands based on input
func GenerateCommandSuggestions(input string, registry CommandRegistry, maxResults int) []CommandSuggestion {
	if registry == nil {
		return nil
	}

	// Remove leading "/" if present
	query := strings.TrimPrefix(input, "/")
	query = strings.ToLower(query)

	// If query is empty, return all commands
	if query == "" {
		commands := registry.List()
		suggestions := make([]CommandSuggestion, 0, len(commands))
		for _, cmd := range commands {
			suggestions = append(suggestions, CommandSuggestion{
				Command: cmd,
				Score:   100,
			})
		}
		if len(suggestions) > maxResults {
			return suggestions[:maxResults]
		}
		return suggestions
	}

	// Score and filter commands
	var suggestions []CommandSuggestion
	for _, cmd := range registry.List() {
		score := scoreCommand(cmd, query)
		if score > 0 {
			suggestions = append(suggestions, CommandSuggestion{
				Command: cmd,
				Score:   score,
			})
		}
	}

	// Sort by score (highest first)
	for i := 0; i < len(suggestions); i++ {
		for j := i + 1; j < len(suggestions); j++ {
			if suggestions[j].Score > suggestions[i].Score {
				suggestions[i], suggestions[j] = suggestions[j], suggestions[i]
			}
		}
	}

	if len(suggestions) > maxResults {
		return suggestions[:maxResults]
	}
	return suggestions
}

// scoreCommand calculates a match score for a command against a query
func scoreCommand(cmd Command, query string) int {
	score := 0

	// Remove leading "/" from command name for comparison
	cmdName := strings.ToLower(strings.TrimPrefix(cmd.Name, "/"))

	// Exact match gets highest score
	if cmdName == query {
		return 1000
	}

	// Prefix match gets high score
	if strings.HasPrefix(cmdName, query) {
		score += 500
	}

	// Contains match gets medium score
	if strings.Contains(cmdName, query) {
		score += 200
	}

	// Check aliases
	for _, alias := range cmd.Aliases {
		aliasName := strings.ToLower(strings.TrimPrefix(alias, "/"))
		if aliasName == query {
			return 900
		}
		if strings.HasPrefix(aliasName, query) {
			score += 400
		}
		if strings.Contains(aliasName, query) {
			score += 150
		}
	}

	// Check description (lower weight)
	if strings.Contains(strings.ToLower(cmd.Description), query) {
		score += 50
	}

	// Fuzzy match bonus
	if fuzzyMatch(cmdName, query) {
		score += 100
	}

	return score
}

// fuzzyMatch checks if all characters in query appear in order in target
func fuzzyMatch(target, query string) bool {
	if query == "" {
		return true
	}
	if target == "" {
		return false
	}

	queryIdx := 0
	for i := 0; i < len(target) && queryIdx < len(query); i++ {
		if target[i] == query[queryIdx] {
			queryIdx++
		}
	}

	return queryIdx == len(query)
}

// GetBestCommandMatch returns the best matching command for autocomplete
func GetBestCommandMatch(input string, registry CommandRegistry) *Command {
	suggestions := GenerateCommandSuggestions(input, registry, 1)
	if len(suggestions) > 0 {
		cmd := suggestions[0].Command
		return &cmd
	}
	return nil
}
