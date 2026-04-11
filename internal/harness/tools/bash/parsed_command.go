package bash

import (
	"strings"
)

type OutputRedirection struct {
	Target   string
	Operator string
}

type ParsedCommand struct {
	originalCommand string
	pipeSegments    []string
	redirections    []OutputRedirection
	analysis        CommandAnalysis
}

func ParseCommand(command string) *ParsedCommand {
	pipeSegments, _ := splitTopLevelSegments(command, func(separator string) bool {
		return separator == "|" || separator == "|&"
	})

	return &ParsedCommand{
		originalCommand: command,
		pipeSegments:    pipeSegments,
		redirections:    extractOutputRedirections(command),
		analysis:        AnalyzeCommand(command),
	}
}

func (p *ParsedCommand) String() string {
	if p == nil {
		return ""
	}
	return p.originalCommand
}

func (p *ParsedCommand) PipeSegments() []string {
	if p == nil {
		return nil
	}
	out := make([]string, len(p.pipeSegments))
	copy(out, p.pipeSegments)
	return out
}

func (p *ParsedCommand) WithoutOutputRedirections() string {
	if p == nil {
		return ""
	}

	tokens := splitCommandTokens(p.originalCommand)
	if len(tokens) == 0 {
		return p.originalCommand
	}

	var kept []string
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		operator, target, fused := parseOutputRedirectionToken(token)
		if operator == "" {
			kept = append(kept, token)
			continue
		}
		if fused {
			if target == "" || strings.HasPrefix(target, "&") {
				kept = append(kept, token)
			}
			continue
		}
		if i+1 < len(tokens) && tokens[i+1] != "" && !strings.HasPrefix(tokens[i+1], "&") {
			i++
			continue
		}
		kept = append(kept, token)
	}

	return strings.Join(kept, " ")
}

func (p *ParsedCommand) OutputRedirections() []OutputRedirection {
	if p == nil {
		return nil
	}
	out := make([]OutputRedirection, len(p.redirections))
	copy(out, p.redirections)
	return out
}

func (p *ParsedCommand) Analysis() CommandAnalysis {
	if p == nil {
		return CommandAnalysis{}
	}
	return p.analysis
}

func extractOutputRedirections(command string) []OutputRedirection {
	tokens := splitCommandTokens(command)
	var out []OutputRedirection
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		operator, target, fused := parseOutputRedirectionToken(token)
		if operator == "" {
			continue
		}
		if !fused {
			if i+1 >= len(tokens) {
				continue
			}
			target = tokens[i+1]
			i++
		}
		target = trimShellWordQuotes(target)
		if target == "" || strings.HasPrefix(target, "&") {
			continue
		}
		out = append(out, OutputRedirection{
			Target:   target,
			Operator: operator,
		})
	}
	return out
}

func parseOutputRedirectionToken(token string) (operator, target string, fused bool) {
	for _, candidate := range []string{"&>>", "&>", ">>", ">|", ">"} {
		if token == candidate || token == "1"+candidate || token == "2"+candidate {
			return candidate, "", false
		}
		if strings.HasPrefix(token, candidate) {
			return candidate, token[len(candidate):], true
		}
		if strings.HasPrefix(token, "1"+candidate) {
			return candidate, token[len(candidate)+1:], true
		}
		if strings.HasPrefix(token, "2"+candidate) {
			return candidate, token[len(candidate)+1:], true
		}
	}
	return "", "", false
}

func trimShellWordQuotes(value string) string {
	if len(value) >= 2 {
		if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
			return value[1 : len(value)-1]
		}
	}
	return value
}
