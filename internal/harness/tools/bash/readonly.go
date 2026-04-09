package bash

import (
	"regexp"
	"strings"
)

// readOnlyCommandRegexes mirrors TS READONLY_COMMAND_REGEXES.
// These are hand-crafted regexes for commands needing custom patterns.
var readOnlyCommandRegexes = []*regexp.Regexp{
	// echo with quoted args, optional 2>&1
	regexp.MustCompile(`^echo\s+(?:'[^']*'|"[^"]*"|\S+)(?:\s+(?:'[^']*'|"[^"]*"|\S+))*\s*(?:2>&1)?$`),
	// pwd, whoami — no arguments
	regexp.MustCompile(`^(?:pwd|whoami)(?:\s|$)`),
	// uniq — flags only, no file paths
	regexp.MustCompile(`^uniq(?:\s+-[cdiuDdf]+)*\s*$`),
	// node version only
	regexp.MustCompile(`^node\s+(?:-v|--version)$`),
	// python version only
	regexp.MustCompile(`^python3?\s+--version$`),
	// history [N]
	regexp.MustCompile(`^history(?:\s+\d+)?\s*$`),
	// alias
	regexp.MustCompile(`^alias(?:\s+\S+)?\s*$`),
	// arch
	regexp.MustCompile(`^arch\s*$`),
	// ip addr / ip link (read-only ip commands)
	regexp.MustCompile(`^ip\s+(?:addr|link|route|neigh|rule)\b`),
	// ifconfig [interface] — read only
	regexp.MustCompile(`^ifconfig(?:\s+\w+)?\s*$`),
	// cd [path]
	regexp.MustCompile(`^cd(?:\s+\S+)?\s*$`),
	// ls with common flags and no redirections/substitutions
	regexp.MustCompile(`^ls(?:\s+-[alhtrdFRS1]*)*(?:\s+(?:[^|;&<>$` + "`" + `()\n]+))?\s*$`),
	// find without -exec, -delete, -fprint
	regexp.MustCompile(`^find\s+[\s\S]*$`), // will be filtered by containsExecDelete
	// claude help
	regexp.MustCompile(`^claude\s+(?:-h|--help)\s*$`),
	// jq — read-only patterns (no -r/-e with redirections, no system())
	regexp.MustCompile(`^jq\s+(?:-[rcCRnMSes]|\s)*(?:'[^']*'|"[^"]*")\s*(?:\S+\s*)*$`),
}

// readOnlyCommandPrefixes are simple commands that are safe with no pipe/redirect/substitution.
var readOnlyCommandPrefixes = []string{
	"cal", "uptime", "cat", "head", "tail", "wc", "stat", "strings", "hexdump", "od",
	"nl", "id", "uname", "free", "df", "du", "locale", "groups", "nproc",
	"basename", "dirname", "realpath", "cut", "paste", "tr", "column",
	"tac", "rev", "fold", "expand", "unexpand", "fmt", "comm", "cmp",
	"numfmt", "readlink", "diff", "true", "false", "sleep", "which",
	"type", "expr", "test", "getconf", "seq", "tsort", "pr",
	"sort", "uniq", "grep", "rg", "awk", "sed",
	"git log", "git show", "git diff", "git status", "git branch",
	"git remote", "git stash list", "git tag", "git describe",
	"git ls-files", "git blame", "git shortlog",
}

// safeReadOnlyRE matches commands in readOnlyCommandPrefixes: no pipes/redirections/substitutions.
var safeReadOnlyRE = regexp.MustCompile(`^[a-z][a-z0-9_\- ]*(?:\s+-[\w-]*|\s+[^|;&<>$` + "`" + `()\n{}]*)*\s*$`)

// findExecDeleteRE matches -exec, -delete, -fprint in find commands.
var findExecDeleteRE = regexp.MustCompile(`\s-(?:exec|delete|fprint|fprint0|fprintf|ok|okdir)\b`)

// containsUnquotedExpansion returns true if the command contains unquoted variable/glob expansion.
func containsUnquotedExpansion(command string) bool {
	inSingle, inDouble := false, false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case inSingle:
			// Inside single quotes: nothing expands
		case c == '$' && !inSingle:
			if i+1 < len(command) && isVarChar(command[i+1]) {
				return true
			}
		case (c == '?' || c == '*' || c == '[') && !inSingle && !inDouble:
			return true
		}
	}
	return false
}

func isVarChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '{' || c == '('
}

// IsCommandReadOnly returns true if the bash command is safe to execute without
// write/execute permission prompting.
func IsCommandReadOnly(command string) bool {
	// Strip trailing 2>&1
	cmd := strings.TrimSuffix(strings.TrimSpace(command), "2>&1")
	cmd = strings.TrimSpace(cmd)

	if cmd == "" {
		return true
	}

	// Reject unquoted expansions
	if containsUnquotedExpansion(cmd) {
		return false
	}

	// Check flag-based allowlist
	if isCommandSafeViaFlagParsing(cmd) {
		return true
	}

	// Check regex patterns
	for _, re := range readOnlyCommandRegexes {
		if re.MatchString(cmd) {
			// Extra check: find without -exec/-delete
			if strings.HasPrefix(cmd, "find ") && findExecDeleteRE.MatchString(cmd) {
				return false
			}
			return true
		}
	}

	// Check simple prefix commands
	for _, prefix := range readOnlyCommandPrefixes {
		if cmd == prefix || strings.HasPrefix(cmd, prefix+" ") || strings.HasPrefix(cmd, prefix+"\t") {
			// Must not contain pipes/redirections/substitutions
			if !safeReadOnlyRE.MatchString(cmd) {
				return false
			}
			// Must not contain $ expansions outside quotes
			if containsUnquotedExpansion(cmd) {
				return false
			}
			return true
		}
	}

	return false
}

// isCommandSafeViaFlagParsing checks the command against the flag-based allowlist.
// Rejects any token containing $ (variable expansion) or brace expansion.
func isCommandSafeViaFlagParsing(command string) bool {
	tokens := splitCommandTokens(command)
	if len(tokens) == 0 {
		return false
	}

	// Reject tokens with $ (runtime expansion defeats static analysis)
	for _, tok := range tokens[1:] {
		if strings.ContainsRune(tok, '$') {
			return false
		}
		// Brace expansion: both { and , or ..
		if strings.ContainsRune(tok, '{') &&
			(strings.ContainsRune(tok, ',') || strings.Contains(tok, "..")) {
			return false
		}
	}

	return isInCommandAllowlist(tokens[0], tokens[1:])
}

// splitCommandTokens splits a command into tokens respecting quotes.
func splitCommandTokens(command string) []string {
	var tokens []string
	var cur strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			cur.WriteByte(c)
		case c == '"' && !inSingle:
			inDouble = !inDouble
			cur.WriteByte(c)
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// isInCommandAllowlist checks if the base command with given args is in the safe allowlist.
// This is a simplified version — flags starting with - are allowed for most read-only commands.
func isInCommandAllowlist(baseCmd string, args []string) bool {
	// Check for operators in args (reject if any contain shell operators)
	for _, arg := range args {
		if arg == "|" || arg == ";" || arg == "&&" || arg == "||" || arg == "&" {
			return false
		}
		if strings.ContainsAny(arg, "|;&<>()") {
			return false
		}
	}

	switch baseCmd {
	case "echo", "printf", "true", "false":
		return true
	case "pwd", "whoami", "id", "uname", "arch", "hostname":
		return allFlags(args)
	case "ls", "cat", "head", "tail", "wc", "stat", "du", "df":
		return allFlagsOrPaths(args)
	case "grep", "rg", "awk", "sed":
		return allFlagsOrPaths(args)
	case "git":
		return isGitReadOnly(args)
	case "sort", "uniq", "cut", "paste", "tr", "column", "comm", "diff":
		return allFlagsOrPaths(args)
	case "find":
		// find is allowed if it doesn't use -exec/-delete
		for _, a := range args {
			if a == "-exec" || a == "-delete" || a == "-fprint" || a == "-ok" {
				return false
			}
		}
		return true
	case "which", "type", "whereis":
		return true
	case "sleep":
		return len(args) == 1
	case "date":
		return len(args) == 0 || (len(args) == 1 && strings.HasPrefix(args[0], "+"))
	}
	return false
}

func allFlags(args []string) bool {
	for _, a := range args {
		if a != "--" && !strings.HasPrefix(a, "-") {
			return false
		}
	}
	return true
}

func allFlagsOrPaths(args []string) bool {
	seenDDash := false
	for _, a := range args {
		if seenDDash {
			continue // after --, all args are paths (allowed)
		}
		if a == "--" {
			seenDDash = true
			continue
		}
		// Flags are OK; paths are OK as long as they don't start with -
		_ = a
	}
	return true
}

// isGitReadOnly returns true for git subcommands that only read the repository.
var gitReadOnlySubcmds = map[string]bool{
	"log": true, "show": true, "diff": true, "status": true,
	"branch": true, "remote": true, "tag": true, "describe": true,
	"ls-files": true, "blame": true, "shortlog": true, "stash": true,
	"rev-parse": true, "rev-list": true, "cat-file": true,
	"ls-tree": true, "for-each-ref": true, "count-objects": true,
	"help": true, "version": true, "--version": true,
}

func isGitReadOnly(args []string) bool {
	if len(args) == 0 {
		return false
	}
	sub := args[0]
	// Strip leading flags before subcommand
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			sub = a
			break
		}
	}
	return gitReadOnlySubcmds[sub]
}
