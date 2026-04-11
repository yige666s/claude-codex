package bash

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"claude-codex/internal/harness/permissions"
)

// PathOperationType describes the kind of filesystem operation a command performs.
type PathOperationType string

const (
	PathOpRead   PathOperationType = "read"
	PathOpWrite  PathOperationType = "write"
	PathOpCreate PathOperationType = "create"
)

// supportedPathCommands is the set of commands for which path extraction is supported.
var supportedPathCommands = map[string]PathOperationType{
	"cd":        PathOpRead,
	"ls":        PathOpRead,
	"find":      PathOpRead,
	"mkdir":     PathOpCreate,
	"touch":     PathOpCreate,
	"rm":        PathOpWrite,
	"rmdir":     PathOpWrite,
	"mv":        PathOpWrite,
	"cp":        PathOpWrite,
	"cat":       PathOpRead,
	"head":      PathOpRead,
	"tail":      PathOpRead,
	"sort":      PathOpRead,
	"uniq":      PathOpRead,
	"wc":        PathOpRead,
	"cut":       PathOpRead,
	"paste":     PathOpRead,
	"column":    PathOpRead,
	"tr":        PathOpRead,
	"file":      PathOpRead,
	"stat":      PathOpRead,
	"diff":      PathOpRead,
	"awk":       PathOpRead,
	"strings":   PathOpRead,
	"hexdump":   PathOpRead,
	"od":        PathOpRead,
	"base64":    PathOpRead,
	"nl":        PathOpRead,
	"grep":      PathOpRead,
	"rg":        PathOpRead,
	"sed":       PathOpWrite,
	"git":       PathOpRead,
	"jq":        PathOpRead,
	"sha256sum": PathOpRead,
	"sha1sum":   PathOpRead,
	"md5sum":    PathOpRead,
}

// dangerousRemovalPaths lists paths that are dangerous to remove.
var dangerousRemovalPaths = []string{
	"/", "/usr", "/etc", "/var", "/bin", "/sbin", "/lib", "/lib64",
	"/home", "/root", "/boot", "/dev", "/proc", "/sys",
}

// processSubstitutionRE matches process substitutions >(...) <(...).
var processSubstitutionRE = regexp.MustCompile(`[><]\(`)

// CheckPathConstraints validates path usage in a command.
// cwd is the current working directory used for resolving relative paths.
func CheckPathConstraints(command, cwd string) permissions.PermissionResult {
	// Block process substitutions >(...) <(...)
	if processSubstitutionRE.MatchString(command) {
		return permissions.Ask("command uses process substitution which requires manual review")
	}

	// Check output redirections
	if r := validateOutputRedirections(command, cwd); r.Behavior != "" && r.Behavior != permissions.BehaviorPassthrough {
		return r
	}

	// Parse and validate path commands in subcommands
	for _, subcmd := range splitSubcommands(command) {
		if r := validateSinglePathCommand(subcmd, cwd); r.Behavior != "" && r.Behavior != permissions.BehaviorPassthrough {
			return r
		}
	}

	return permissions.PermissionResult{Behavior: permissions.BehaviorPassthrough}
}

// validateOutputRedirections checks that output redirections do not target dangerous paths.
func validateOutputRedirections(command, cwd string) permissions.PermissionResult {
	parsed := ParseCommand(command)
	for _, redirection := range parsed.OutputRedirections() {
		target := strings.TrimSpace(redirection.Target)
		if target == "" || target == "/dev/null" {
			continue
		}
		abs := resolvePath(target, cwd)
		if isDangerousPath(abs, PathOpCreate) {
			return permissions.PermissionResult{
				Behavior:    permissions.BehaviorAsk,
				Message:     "output redirection to path requires permission: " + target,
				BlockedPath: abs,
			}
		}
	}
	return permissions.PermissionResult{}
}

// validateSinglePathCommand validates path arguments for a single subcommand.
func validateSinglePathCommand(command, cwd string) permissions.PermissionResult {
	stripped := stripSafeWrappersForPath(command)
	tokens := splitCommandTokens(stripped)
	if len(tokens) == 0 {
		return permissions.PermissionResult{}
	}

	baseCmd := tokens[0]
	opType, supported := supportedPathCommands[baseCmd]
	if !supported {
		return permissions.PermissionResult{}
	}

	args := filterOutFlags(tokens[1:])
	paths := extractCommandPaths(baseCmd, args, cwd)

	for _, p := range paths {
		abs := resolvePath(p, cwd)
		if isDangerousPath(abs, opType) {
			return permissions.PermissionResult{
				Behavior:    permissions.BehaviorAsk,
				Message:     "command targets restricted path: " + p,
				BlockedPath: abs,
			}
		}
		// For rm/rmdir: extra check for dangerous removal
		if baseCmd == "rm" || baseCmd == "rmdir" {
			if isDangerousRemovalPath(abs) {
				return permissions.PermissionResult{
					Behavior:    permissions.BehaviorAsk,
					Message:     "removal of system path requires manual confirmation: " + p,
					BlockedPath: abs,
				}
			}
		}
	}

	return permissions.PermissionResult{}
}

// extractCommandPaths extracts the file path arguments from a command's args.
func extractCommandPaths(cmd string, args []string, cwd string) []string {
	switch cmd {
	case "cd":
		if len(args) == 0 {
			return []string{userHomeDir()}
		}
		return []string{strings.Join(args, " ")}
	case "ls":
		if len(args) == 0 {
			return []string{"."}
		}
		return args
	case "grep", "rg":
		// First non-flag arg is the pattern; remaining are paths
		if len(args) > 1 {
			return args[1:]
		}
		return nil
	case "sed":
		// Skip -e/-f script args; remaining are files
		var paths []string
		skipNext := false
		for _, a := range args {
			if skipNext {
				skipNext = false
				continue
			}
			if a == "-e" || a == "-f" || a == "--expression" || a == "--file" {
				skipNext = true
				continue
			}
			paths = append(paths, a)
		}
		return paths
	case "jq":
		// Skip filter (first non-flag arg) and flag args; remaining are files
		var paths []string
		sawFilter := false
		skipNext := false
		for _, a := range args {
			if skipNext {
				skipNext = false
				continue
			}
			if strings.HasPrefix(a, "-") {
				if a == "-e" || a == "-f" || a == "--arg" || a == "--argjson" {
					skipNext = true
				}
				continue
			}
			if !sawFilter {
				sawFilter = true
				continue
			}
			paths = append(paths, a)
		}
		return paths
	default:
		return args
	}
}

// filterOutFlags filters out flag arguments (starting with -) respecting -- end-of-options.
func filterOutFlags(args []string) []string {
	var out []string
	seenDDash := false
	for _, a := range args {
		if seenDDash {
			out = append(out, a)
			continue
		}
		if a == "--" {
			seenDDash = true
			continue
		}
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

// resolvePath resolves a possibly-relative path against cwd.
func resolvePath(p, cwd string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home := userHomeDir()
		p = strings.Replace(p, "~", home, 1)
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(cwd, p))
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "~"
	}
	return home
}

// isDangerousPath returns true if the path should require permission based on operation type.
func isDangerousPath(abs string, op PathOperationType) bool {
	if op == PathOpRead {
		return false // read operations don't require path-based restrictions
	}
	// Write/create operations are dangerous in system dirs
	for _, d := range dangerousRemovalPaths {
		if abs == d || strings.HasPrefix(abs, d+"/") {
			return true
		}
	}
	return false
}

// isDangerousRemovalPath returns true if the path is a system root that should not be removed.
func isDangerousRemovalPath(abs string) bool {
	for _, d := range dangerousRemovalPaths {
		if abs == d {
			return true
		}
	}
	return false
}

// stripSafeWrappersForPath strips timeout/time/nice/nohup wrappers for path analysis.
func stripSafeWrappersForPath(command string) string {
	wrappers := []string{"timeout ", "time ", "nice ", "nohup ", "stdbuf "}
	prev := ""
	for prev != command {
		prev = command
		for _, w := range wrappers {
			if strings.HasPrefix(command, w) {
				command = strings.TrimPrefix(command, w)
				// Skip duration arg for timeout
				if w == "timeout " {
					if idx := strings.Index(command, " "); idx >= 0 {
						command = strings.TrimSpace(command[idx:])
					}
				}
				command = strings.TrimSpace(command)
			}
		}
	}
	return command
}
