package search

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type GrepTool struct {
	rootDir string
}

type grepInput struct {
	Path            string `json:"path,omitempty"`
	Pattern         string `json:"pattern"`
	Glob            string `json:"glob,omitempty"`
	OutputMode      string `json:"output_mode,omitempty"`
	Before          int    `json:"-B,omitempty"`
	After           int    `json:"-A,omitempty"`
	Context         int    `json:"context,omitempty"`
	ContextAlias    int    `json:"-C,omitempty"`
	ShowLineNumbers *bool  `json:"-n,omitempty"`
	CaseInsensitive bool   `json:"-i,omitempty"`
	Type            string `json:"type,omitempty"`
	HeadLimit       *int   `json:"head_limit,omitempty"`
	Offset          int    `json:"offset,omitempty"`
	Multiline       bool   `json:"multiline,omitempty"`
	MaxMatches      int    `json:"max_matches,omitempty"`
}

func NewGrepTool(rootDir string) *GrepTool {
	return &GrepTool{rootDir: rootDir}
}

func (t *GrepTool) Name() string {
	return "Grep"
}

func (t *GrepTool) Description() string {
	return "Search file contents under the project root, using rg when available."
}

func (t *GrepTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"The regular expression pattern to search for in file contents"},"path":{"type":"string","description":"File or directory to search in. Defaults to the current working directory."},"glob":{"type":"string","description":"Glob pattern to filter files, e.g. *.go or *.{ts,tsx}"},"output_mode":{"type":"string","enum":["content","files_with_matches","count"],"description":"Output mode. Defaults to files_with_matches."},"-B":{"type":"integer","description":"Lines before each match for content mode."},"-A":{"type":"integer","description":"Lines after each match for content mode."},"-C":{"type":"integer","description":"Context lines before and after each match for content mode."},"context":{"type":"integer","description":"Context lines before and after each match for content mode."},"-n":{"type":"boolean","description":"Show line numbers in content mode. Defaults to true."},"-i":{"type":"boolean","description":"Case insensitive search."},"type":{"type":"string","description":"File type to search, passed to rg --type when rg is available."},"head_limit":{"type":"integer","description":"Limit output entries. Defaults to 250; 0 means unlimited."},"offset":{"type":"integer","description":"Skip first N output entries before applying head_limit."},"multiline":{"type":"boolean","description":"Enable multiline regex mode when rg is available."},"max_matches":{"type":"integer","description":"Legacy alias for head_limit."}},"required":["pattern"]}`)
}

func (t *GrepTool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *GrepTool) IsConcurrencySafe() bool {
	return true // grep is read-only and safe to run concurrently
}

func (t *GrepTool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var input grepInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return toolkit.Result{}, err
	}

	searchRoot := t.rootDir
	if input.Path != "" {
		resolved, err := toolkit.ResolvePath(t.rootDir, input.Path)
		if err != nil {
			return toolkit.Result{}, err
		}
		searchRoot = resolved
	}

	outputMode := input.OutputMode
	if outputMode == "" {
		outputMode = "files_with_matches"
	}
	switch outputMode {
	case "content", "files_with_matches", "count":
	default:
		return toolkit.Result{}, fmt.Errorf("unsupported output_mode %q", outputMode)
	}
	limit := 250
	if input.MaxMatches > 0 {
		limit = input.MaxMatches
	}
	if input.HeadLimit != nil {
		limit = *input.HeadLimit
		if limit < 0 {
			limit = 0
		}
	}
	if input.Offset < 0 {
		input.Offset = 0
	}

	if output, ok, err := runRipgrep(ctx, searchRoot, input, outputMode, limit); ok {
		if err != nil {
			return toolkit.Result{}, err
		}
		return toolkit.Result{Output: output}, nil
	}

	return grepFallback(searchRoot, input, outputMode, limit)
}

func runRipgrep(ctx context.Context, searchRoot string, input grepInput, outputMode string, limit int) (string, bool, error) {
	path, err := exec.LookPath("rg")
	if err != nil {
		return "", false, nil
	}

	args := []string{"--hidden", "--color", "never", "--max-columns", "500"}
	if input.Multiline {
		args = append(args, "-U", "--multiline-dotall")
	}
	if input.CaseInsensitive {
		args = append(args, "-i")
	}
	switch outputMode {
	case "files_with_matches":
		args = append(args, "-l")
	case "count":
		args = append(args, "-c")
	case "content":
		if input.ShowLineNumbers == nil || *input.ShowLineNumbers {
			args = append(args, "-n")
		}
		contextLines := input.Context
		if input.ContextAlias > 0 {
			contextLines = input.ContextAlias
		}
		if contextLines > 0 {
			args = append(args, "-C", fmt.Sprintf("%d", contextLines))
		} else {
			if input.Before > 0 {
				args = append(args, "-B", fmt.Sprintf("%d", input.Before))
			}
			if input.After > 0 {
				args = append(args, "-A", fmt.Sprintf("%d", input.After))
			}
		}
	}
	for _, pattern := range splitGlobPatterns(input.Glob) {
		args = append(args, "--glob", pattern)
	}
	if input.Type != "" {
		args = append(args, "--type", input.Type)
	}
	args = append(args, input.Pattern, searchRoot)

	command := exec.CommandContext(ctx, path, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		exitErr := new(exec.ExitError)
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", true, nil
		}
		return "", true, fmt.Errorf("rg failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	output := relativizeRipgrepOutput(strings.TrimSpace(stdout.String()), searchRoot)
	return applyHeadOffset(output, input.Offset, limit), true, nil
}

func grepFallback(searchRoot string, input grepInput, outputMode string, limit int) (toolkit.Result, error) {
	regexPattern := input.Pattern
	if input.CaseInsensitive {
		regexPattern = "(?i)" + regexPattern
	}
	compiled, err := regexp.Compile(regexPattern)
	if err != nil {
		compiled = regexp.MustCompile(regexp.QuoteMeta(input.Pattern))
	}
	globMatchers := make([]*regexp.Regexp, 0)
	for _, pattern := range splitGlobPatterns(input.Glob) {
		matcher, err := compileGlobPattern(pattern)
		if err != nil {
			return toolkit.Result{}, err
		}
		globMatchers = append(globMatchers, matcher)
	}

	results := make([]string, 0, 16)
	fileCounts := map[string]int{}
	err = filepath.WalkDir(searchRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		relative, err := filepath.Rel(searchRoot, path)
		if err != nil {
			return err
		}
		normalized := filepath.ToSlash(relative)
		if input.Type != "" && !matchesType(normalized, input.Type) {
			return nil
		}
		if len(globMatchers) > 0 && !matchesAnyGlob(globMatchers, normalized) {
			return nil
		}

		scanner := bufio.NewScanner(file)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Text()
			if compiled.MatchString(line) {
				switch outputMode {
				case "files_with_matches":
					results = append(results, normalized)
					return nil
				case "count":
					fileCounts[normalized]++
				default:
					results = append(results, formatGrepContentLine(normalized, lineNumber, line, input.ShowLineNumbers))
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}

		return nil
	})
	if err != nil && err != fs.SkipAll {
		return toolkit.Result{}, err
	}
	if outputMode == "count" {
		for file, count := range fileCounts {
			results = append(results, fmt.Sprintf("%s:%d", file, count))
		}
		sort.Strings(results)
	}

	return toolkit.Result{Output: applyHeadOffset(strings.Join(results, "\n"), input.Offset, limit)}, nil
}

func splitGlobPatterns(glob string) []string {
	fields := strings.Fields(glob)
	patterns := make([]string, 0, len(fields))
	for _, field := range fields {
		for _, part := range strings.Split(field, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				patterns = append(patterns, trimmed)
			}
		}
	}
	return patterns
}

func matchesAnyGlob(matchers []*regexp.Regexp, path string) bool {
	for _, matcher := range matchers {
		if matcher.MatchString(path) || matcher.MatchString(filepath.Base(path)) {
			return true
		}
	}
	return false
}

func matchesType(path, typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "go":
		return strings.HasSuffix(path, ".go")
	case "js", "javascript":
		return strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".jsx")
	case "ts", "typescript":
		return strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")
	case "py", "python":
		return strings.HasSuffix(path, ".py")
	case "rust", "rs":
		return strings.HasSuffix(path, ".rs")
	case "java":
		return strings.HasSuffix(path, ".java")
	default:
		return true
	}
}

func formatGrepContentLine(path string, lineNumber int, line string, showLineNumbers *bool) string {
	if showLineNumbers == nil || *showLineNumbers {
		return fmt.Sprintf("%s:%d:%s", path, lineNumber, line)
	}
	return fmt.Sprintf("%s:%s", path, line)
}

func applyHeadOffset(output string, offset, limit int) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}
	lines := strings.Split(output, "\n")
	if offset > len(lines) {
		return ""
	}
	lines = lines[offset:]
	if limit > 0 && len(lines) > limit {
		lines = lines[:limit]
	}
	return strings.Join(lines, "\n")
}

func relativizeRipgrepOutput(output, searchRoot string) string {
	if output == "" {
		return ""
	}
	prefix := filepath.ToSlash(strings.TrimRight(searchRoot, string(filepath.Separator))) + "/"
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		normalized := filepath.ToSlash(line)
		if strings.HasPrefix(normalized, prefix) {
			lines[i] = strings.TrimPrefix(normalized, prefix)
		}
	}
	return strings.Join(lines, "\n")
}
