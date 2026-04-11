package powershell

import "strings"

type PrefixResult struct {
	CommandPrefix string
}

var depthRules = map[string]int{
	"git":     2,
	"npm":     2,
	"pnpm":    2,
	"yarn":    2,
	"npx":     2,
	"docker":  2,
	"kubectl": 2,
	"go":      2,
	"cargo":   2,
	"gh":      2,
	"gcloud":  2,
	"aws":     2,
	"az":      2,
}

func GetCommandPrefixStatic(command string) *PrefixResult {
	parsed := ParseCommand(command)
	if !parsed.Valid {
		return nil
	}
	commands := GetAllCommands(parsed)
	for _, cmd := range commands {
		if cmd.ElementType != PipelineElementCommand {
			continue
		}
		prefix := extractPrefixFromElement(cmd)
		return &PrefixResult{CommandPrefix: prefix}
	}
	return &PrefixResult{CommandPrefix: ""}
}

func GetCompoundCommandPrefixesStatic(command string, excludeSubcommand func(ParsedCommandElement) bool) []string {
	parsed := ParseCommand(command)
	if !parsed.Valid {
		return nil
	}
	commands := GetAllCommands(parsed)
	if len(commands) <= 1 {
		if len(commands) == 0 {
			return nil
		}
		if excludeSubcommand != nil && excludeSubcommand(commands[0]) {
			return nil
		}
		prefix := extractPrefixFromElement(commands[0])
		if prefix == "" {
			return nil
		}
		return []string{prefix}
	}

	grouped := map[string][]string{}
	for _, cmd := range commands {
		if excludeSubcommand != nil && excludeSubcommand(cmd) {
			continue
		}
		prefix := extractPrefixFromElement(cmd)
		if prefix == "" {
			continue
		}
		root := strings.ToLower(strings.Fields(prefix)[0])
		grouped[root] = append(grouped[root], prefix)
	}

	var result []string
	for root, group := range grouped {
		lcp := wordAlignedLCP(group)
		if len(strings.Fields(lcp)) <= 1 && depthRules[root] > 0 {
			continue
		}
		if lcp != "" {
			result = append(result, lcp)
		}
	}
	return result
}

func extractPrefixFromElement(cmd ParsedCommandElement) string {
	if cmd.Name == "" || cmd.NameType == "application" || NeverSuggestCommand(cmd.Name) {
		return ""
	}
	if cmd.NameType == "cmdlet" {
		return cmd.Name
	}
	for _, tokenType := range cmd.ElementTypes {
		if tokenType == CommandElementSubExpression || tokenType == CommandElementScriptBlock {
			return ""
		}
	}

	root := cmd.Name
	if depthRules[strings.ToLower(root)] == 0 {
		return root
	}

	words := []string{root}
	args := cmd.Args
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		if strings.ContainsAny(arg, `/\`) {
			return ""
		}
		words = append(words, arg)
		if len(words) >= depthRules[strings.ToLower(root)] {
			break
		}
	}
	if len(words) <= 1 {
		return ""
	}
	return strings.Join(words, " ")
}

func wordAlignedLCP(values []string) string {
	if len(values) == 0 {
		return ""
	}
	if len(values) == 1 {
		return values[0]
	}
	base := strings.Fields(values[0])
	matchCount := len(base)
	for _, value := range values[1:] {
		words := strings.Fields(value)
		count := 0
		for count < matchCount && count < len(words) && strings.EqualFold(base[count], words[count]) {
			count++
		}
		matchCount = count
	}
	return strings.Join(base[:matchCount], " ")
}
