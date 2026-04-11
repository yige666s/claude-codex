package computeruse

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	ComputerUseMCPServerName = "computer-use"
	CLIHostBundleID          = "com.anthropic.claude-code.cli-no-window"
)

var terminalBundleIDFallback = map[string]string{
	"iTerm.app":      "com.googlecode.iterm2",
	"Apple_Terminal": "com.apple.Terminal",
	"ghostty":        "com.mitchellh.ghostty",
	"kitty":          "net.kovidgoyal.kitty",
	"WarpTerminal":   "dev.warp.Warp-Stable",
	"vscode":         "com.microsoft.VSCode",
}

var alwaysKeepBundleIDs = map[string]struct{}{
	"com.apple.Safari":            {},
	"com.google.Chrome":           {},
	"com.microsoft.edgemac":       {},
	"org.mozilla.firefox":         {},
	"com.tinyspeck.slackmacgap":   {},
	"com.microsoft.VSCode":        {},
	"com.apple.Terminal":          {},
	"com.googlecode.iterm2":       {},
	"com.apple.finder":            {},
	"com.apple.systempreferences": {},
}

var noisyNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`Helper(?:$|\s\()|Agent(?:$|\s\()|Service(?:$|\s\()|Uninstaller(?:$|\s\()|Updater(?:$|\s\()|^\.`),
}

var appNameAllowed = regexp.MustCompile(`^[\p{L}\p{M}\p{N}_ .&'()+-]+$`)

type InstalledApp struct {
	BundleID    string
	DisplayName string
	Path        string
}

type Capabilities struct {
	ScreenshotFiltering string
	Platform            string
}

func GetTerminalBundleID() string {
	if value := os.Getenv("__CFBundleIdentifier"); strings.TrimSpace(value) != "" {
		return value
	}
	return terminalBundleIDFallback[os.Getenv("TERM_PROGRAM")]
}

func IsComputerUseMCPServer(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(name, "_", "-")))
	return normalized == ComputerUseMCPServerName
}

func CLICapabilities() Capabilities {
	return Capabilities{
		ScreenshotFiltering: "native",
		Platform:            "darwin",
	}
}

func FilterAppsForDescription(installed []InstalledApp, homeDir string) []string {
	var trusted []string
	var rest []string
	for _, app := range installed {
		if _, ok := alwaysKeepBundleIDs[app.BundleID]; ok {
			trusted = append(trusted, app.DisplayName)
			continue
		}
		if isUserFacingPath(app.Path, homeDir) && !isNoisyName(app.DisplayName) {
			rest = append(rest, app.DisplayName)
		}
	}
	return mergeNames(sanitizeNames(trusted, false), sanitizeNames(rest, true))
}

func ChicagoEnabled(userType string, monorepoRoot string, allowAnt string, subscription string, flagEnabled bool) bool {
	if userType == "ant" && monorepoRoot != "" && !isTruthy(allowAnt) {
		return false
	}
	if userType != "ant" && subscription != "max" && subscription != "pro" {
		return false
	}
	return flagEnabled
}

func isUserFacingPath(path string, homeDir string) bool {
	for _, root := range []string{"/Applications/", "/System/Applications/"} {
		if strings.HasPrefix(path, root) {
			return true
		}
	}
	if homeDir != "" {
		prefix := strings.TrimRight(homeDir, "/") + "/Applications/"
		return strings.HasPrefix(path, prefix)
	}
	return false
}

func isNoisyName(name string) bool {
	for _, pattern := range noisyNamePatterns {
		if pattern.MatchString(name) {
			return true
		}
	}
	return false
}

func sanitizeNames(raw []string, applyCharFilter bool) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, name := range raw {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > 40 || seen[name] {
			continue
		}
		if applyCharFilter && !appNameAllowed.MatchString(name) {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	if len(out) > 50 {
		extra := len(out) - 50
		out = append(out[:50], "… and "+strconv.Itoa(extra)+" more")
	}
	return out
}

func mergeNames(primary []string, secondary []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(primary)+len(secondary))
	for _, name := range append(primary, secondary...) {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
