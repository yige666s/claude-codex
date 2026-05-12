package search

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type GlobTool struct {
	rootDir string
}

type globInput struct {
	Path       string `json:"path,omitempty"`
	Pattern    string `json:"pattern"`
	MaxResults int    `json:"max_results,omitempty"`
}

func NewGlobTool(rootDir string) *GlobTool {
	return &GlobTool{rootDir: rootDir}
}

func (t *GlobTool) Name() string {
	return "Glob"
}

func (t *GlobTool) Description() string {
	return "List files under the project root that match a glob pattern."
}

func (t *GlobTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"pattern":{"type":"string"},"max_results":{"type":"integer"}},"required":["pattern"]}`)
}

func (t *GlobTool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *GlobTool) IsConcurrencySafe() bool {
	return true // glob is read-only and safe to run concurrently
}

func (t *GlobTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var input globInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return toolkit.Result{}, err
	}

	baseDir := t.rootDir
	if input.Path != "" {
		resolved, err := toolkit.ResolvePath(t.rootDir, input.Path)
		if err != nil {
			return toolkit.Result{}, err
		}
		baseDir = resolved
	}

	matcher, err := compileGlobPattern(input.Pattern)
	if err != nil {
		return toolkit.Result{}, err
	}

	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = 200
	}

	type match struct {
		path    string
		modTime time.Time
	}
	matches := make([]match, 0, 16)
	err = filepath.WalkDir(baseDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		relative, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}

		normalized := filepath.ToSlash(relative)
		if matcher.MatchString(normalized) || matcher.MatchString(filepath.ToSlash(entry.Name())) {
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			matches = append(matches, match{path: normalized, modTime: info.ModTime()})
		}

		if len(matches) >= maxResults {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && err != fs.SkipAll {
		return toolkit.Result{}, err
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].modTime.Equal(matches[j].modTime) {
			return matches[i].path < matches[j].path
		}
		return matches[i].modTime.After(matches[j].modTime)
	})
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		paths = append(paths, match.path)
	}
	return toolkit.Result{Output: strings.Join(paths, "\n")}, nil
}

func compileGlobPattern(pattern string) (*regexp.Regexp, error) {
	normalized := filepath.ToSlash(strings.TrimSpace(pattern))
	var builder strings.Builder
	builder.WriteString("^")
	for i := 0; i < len(normalized); i++ {
		ch := normalized[i]
		switch ch {
		case '*':
			if i+1 < len(normalized) && normalized[i+1] == '*' {
				builder.WriteString(".*")
				i++
			} else {
				builder.WriteString(`[^/]*`)
			}
		case '?':
			builder.WriteString(`[^/]`)
		case '.', '+', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			builder.WriteByte('\\')
			builder.WriteByte(ch)
		default:
			builder.WriteByte(ch)
		}
	}
	builder.WriteString("$")
	return regexp.Compile(builder.String())
}
