package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/harness/memdir"
	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
	"github.com/ding/claude-code/claude-go/internal/public/fsutil"
)

type EditTool struct {
	rootDir string
}

type editInput struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func NewEditTool(rootDir string) *EditTool {
	return &EditTool{rootDir: rootDir}
}

func (t *EditTool) Name() string {
	return "file_edit"
}

func (t *EditTool) Description() string {
	return "Replace text in a file under the project root."
}

func (t *EditTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["path","old_string","new_string"]}`)
}

func (t *EditTool) Permission() permissions.Level {
	return permissions.LevelWrite
}

func (t *EditTool) IsConcurrencySafe() bool {
	return false // file edits are not safe to run concurrently
}

func (t *EditTool) Execute(_ context.Context, input json.RawMessage) (toolkit.Result, error) {
	var payload editInput
	if err := json.Unmarshal(input, &payload); err != nil {
		return toolkit.Result{}, err
	}
	if payload.OldString == "" {
		return toolkit.Result{}, fmt.Errorf("old_string is required")
	}

	path, err := toolkit.ResolvePath(t.rootDir, payload.Path)
	if err != nil {
		return toolkit.Result{}, err
	}

	if err := memdir.CheckTeamMemSecrets(path, payload.NewString, t.rootDir); err != nil {
		return toolkit.Result{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return toolkit.Result{}, err
	}

	content := string(data)
	if !strings.Contains(content, payload.OldString) {
		return toolkit.Result{}, fmt.Errorf("target text not found in %s", path)
	}

	updated := strings.Replace(content, payload.OldString, payload.NewString, 1)
	if payload.ReplaceAll {
		updated = strings.ReplaceAll(content, payload.OldString, payload.NewString)
	}

	if err := fsutil.WriteFileAtomic(path, []byte(updated), 0o644); err != nil {
		return toolkit.Result{}, err
	}

	diff := fmt.Sprintf(
		"edited %s\n```diff\n--- %s\n+++ %s\n-%s\n+%s\n```",
		path,
		filepath.Base(path),
		filepath.Base(path),
		payload.OldString,
		payload.NewString,
	)

	return toolkit.Result{Output: diff}, nil
}
