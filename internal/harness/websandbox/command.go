package websandbox

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func ParseAction(scope Scope, command string) (Action, error) {
	command = normalizeLegacySkillCommand(scope, strings.TrimSpace(command))
	if command == "" {
		return Action{}, fmt.Errorf("web sandbox denied empty command")
	}
	if err := validateCommandSyntax(command); err != nil {
		return Action{}, err
	}
	tokens, err := splitCommandTokens(command)
	if err != nil {
		return Action{}, err
	}
	if len(tokens) == 0 {
		return Action{}, fmt.Errorf("web sandbox denied empty command")
	}
	envs := make(map[string]string)
	for len(tokens) > 0 && isEnvAssignment(tokens[0]) {
		name, value, _ := strings.Cut(tokens[0], "=")
		envs[name] = value
		tokens = tokens[1:]
	}
	if len(tokens) == 0 {
		return Action{}, fmt.Errorf("web sandbox denied command %q: missing executable", command)
	}

	switch tokens[0] {
	case "python", "python3", "bash", "sh":
		return parseScriptExecution(scope, command, envs, tokens)
	case "ls":
		return parseListScripts(scope, command, envs, tokens)
	case "find":
		return parseFindScripts(scope, command, envs, tokens)
	default:
		return Action{}, fmt.Errorf("web sandbox denied command %q: only direct script execution in scripts/ is allowed", command)
	}
}

func parseScriptExecution(scope Scope, raw string, envs map[string]string, tokens []string) (Action, error) {
	if len(tokens) < 2 {
		return Action{}, fmt.Errorf("web sandbox denied command %q: missing script path", raw)
	}
	if strings.HasPrefix(tokens[1], "-") {
		return Action{}, fmt.Errorf("web sandbox denied command %q: interpreter flags are not allowed", raw)
	}
	scriptPath, err := resolveScriptPath(scope.RootDir, tokens[1])
	if err != nil {
		return Action{}, err
	}
	args := []string{mapToContainerPath(scope.RootDir, scriptPath)}
	args = append(args, tokens[2:]...)
	return Action{
		RawCommand: raw,
		Type:       ActionExecuteScript,
		Env:        envs,
		Binary:     tokens[0],
		Args:       args,
	}, nil
}

func parseListScripts(scope Scope, raw string, envs map[string]string, tokens []string) (Action, error) {
	if !scope.SkillScoped {
		return Action{}, fmt.Errorf("web sandbox denied command %q: script discovery requires skill scope", raw)
	}
	args := make([]string, 0, len(tokens)-1)
	for _, token := range tokens[1:] {
		if strings.HasPrefix(token, "-") {
			switch token {
			case "-l", "-a", "-la", "-al":
			default:
				return Action{}, fmt.Errorf("web sandbox denied command %q: unsupported ls flag %q", raw, token)
			}
			args = append(args, token)
			continue
		}
		resolved, err := resolveScriptsPath(scope.RootDir, token)
		if err != nil {
			return Action{}, err
		}
		args = append(args, mapToContainerPath(scope.RootDir, resolved))
	}
	if len(args) == 0 {
		args = append(args, "scripts")
	}
	return Action{RawCommand: raw, Type: ActionListScripts, Env: envs, Binary: "ls", Args: args}, nil
}

func parseFindScripts(scope Scope, raw string, envs map[string]string, tokens []string) (Action, error) {
	if !scope.SkillScoped {
		return Action{}, fmt.Errorf("web sandbox denied command %q: script discovery requires skill scope", raw)
	}
	if len(tokens) < 2 {
		return Action{}, fmt.Errorf("web sandbox denied command %q: missing find path", raw)
	}
	root, err := resolveScriptsPath(scope.RootDir, tokens[1])
	if err != nil {
		return Action{}, err
	}
	args := []string{mapToContainerPath(scope.RootDir, root)}
	rest := tokens[2:]
	for i := 0; i < len(rest); i++ {
		token := rest[i]
		switch token {
		case "-maxdepth":
			if i+1 >= len(rest) {
				return Action{}, fmt.Errorf("web sandbox denied command %q: missing -maxdepth value", raw)
			}
			if _, err := strconv.Atoi(rest[i+1]); err != nil {
				return Action{}, fmt.Errorf("web sandbox denied command %q: invalid -maxdepth value", raw)
			}
			args = append(args, token, rest[i+1])
			i++
		case "-type":
			if i+1 >= len(rest) || (rest[i+1] != "f" && rest[i+1] != "d") {
				return Action{}, fmt.Errorf("web sandbox denied command %q: invalid -type value", raw)
			}
			args = append(args, token, rest[i+1])
			i++
		case "-name":
			if i+1 >= len(rest) {
				return Action{}, fmt.Errorf("web sandbox denied command %q: missing -name value", raw)
			}
			args = append(args, token, rest[i+1])
			i++
		default:
			return Action{}, fmt.Errorf("web sandbox denied command %q: unsupported find token %q", raw, token)
		}
	}
	return Action{RawCommand: raw, Type: ActionFindScripts, Env: envs, Binary: "find", Args: args}, nil
}

func validateCommandSyntax(command string) error {
	for _, fragment := range []string{"|", ";", "&&", "||", ">", "<", "$(", "`", "\n"} {
		if strings.Contains(command, fragment) {
			return fmt.Errorf("web sandbox denied command %q: shell operator %q is not allowed", command, fragment)
		}
	}
	return nil
}

func normalizeLegacySkillCommand(scope Scope, command string) string {
	if !scope.SkillScoped || !strings.Contains(command, "&&") {
		return command
	}
	left, right, ok := strings.Cut(command, "&&")
	if !ok {
		return command
	}
	leftTokens, err := splitCommandTokens(strings.TrimSpace(left))
	if err != nil || len(leftTokens) != 2 || leftTokens[0] != "cd" {
		return command
	}
	target := filepath.Clean(leftTokens[1])
	if target != filepath.Clean(scope.RootDir) && target != filepath.Clean(containerSkillRoot) {
		return command
	}
	return strings.TrimSpace(right)
}

func splitCommandTokens(command string) ([]string, error) {
	var (
		tokens   []string
		current  strings.Builder
		quote    rune
		escaping bool
	)
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}
	for _, r := range command {
		switch {
		case escaping:
			current.WriteRune(r)
			escaping = false
		case r == '\\':
			escaping = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	if escaping || quote != 0 {
		return nil, fmt.Errorf("web sandbox denied command %q: unterminated shell quoting", command)
	}
	flush()
	return tokens, nil
}

func isEnvAssignment(token string) bool {
	name, _, ok := strings.Cut(token, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '_' {
				return false
			}
			continue
		}
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func resolveScriptPath(rootDir, value string) (string, error) {
	return resolvePathWithin(filepath.Join(rootDir, "scripts"), rootDir, value, "scripts/")
}

func resolveScriptsPath(rootDir, value string) (string, error) {
	return resolvePathWithin(filepath.Join(rootDir, "scripts"), rootDir, value, "scripts/")
}

func resolvePathWithin(allowedRoot, skillRoot, value, label string) (string, error) {
	if strings.TrimSpace(skillRoot) == "" {
		return "", fmt.Errorf("web sandbox denied path %q: missing skill root", value)
	}
	resolved := value
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(skillRoot, value)
	}
	resolved = filepath.Clean(resolved)
	allowedRoot = filepath.Clean(allowedRoot)
	rel, err := filepath.Rel(allowedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("web sandbox denied path %q: only %s is accessible", value, label)
	}
	return resolved, nil
}

func mapToContainerPath(skillRoot, hostPath string) string {
	rel, err := filepath.Rel(filepath.Clean(skillRoot), filepath.Clean(hostPath))
	if err != nil {
		return hostPath
	}
	return filepath.ToSlash(filepath.Join(containerSkillRoot, rel))
}
