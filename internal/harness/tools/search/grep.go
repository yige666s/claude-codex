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
	"strings"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type GrepTool struct {
	rootDir string
}

type grepInput struct {
	Path       string `json:"path,omitempty"`
	Pattern    string `json:"pattern"`
	MaxMatches int    `json:"max_matches,omitempty"`
}

func NewGrepTool(rootDir string) *GrepTool {
	return &GrepTool{rootDir: rootDir}
}

func (t *GrepTool) Name() string {
	return "grep"
}

func (t *GrepTool) Description() string {
	return "Search file contents under the project root, using rg when available."
}

func (t *GrepTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"pattern":{"type":"string"},"max_matches":{"type":"integer"}},"required":["pattern"]}`)
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

	maxMatches := input.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 100
	}

	if output, ok, err := runRipgrep(ctx, searchRoot, input.Pattern, maxMatches); ok {
		if err != nil {
			return toolkit.Result{}, err
		}
		return toolkit.Result{Output: output}, nil
	}

	return grepFallback(searchRoot, input.Pattern, maxMatches)
}

func runRipgrep(ctx context.Context, searchRoot, pattern string, maxMatches int) (string, bool, error) {
	path, err := exec.LookPath("rg")
	if err != nil {
		return "", false, nil
	}

	command := exec.CommandContext(ctx, path, "-n", "--color", "never", "-m", fmt.Sprintf("%d", maxMatches), pattern, searchRoot)
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

	return strings.TrimSpace(stdout.String()), true, nil
}

func grepFallback(searchRoot, pattern string, maxMatches int) (toolkit.Result, error) {
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		compiled = regexp.MustCompile(regexp.QuoteMeta(pattern))
	}

	results := make([]string, 0, 16)
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

		scanner := bufio.NewScanner(file)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Text()
			if compiled.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(relative), lineNumber, line))
				if len(results) >= maxMatches {
					return fs.SkipAll
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

	return toolkit.Result{Output: strings.Join(results, "\n")}, nil
}
