package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ding/claude-code/claude-go/internal/public/fsutil"
	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type WriteTool struct {
	rootDir string
}

type writeInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func NewWriteTool(rootDir string) *WriteTool {
	return &WriteTool{rootDir: rootDir}
}

func (t *WriteTool) Name() string {
	return "file_write"
}

func (t *WriteTool) Description() string {
	return "Write a UTF-8 text file under the project root using atomic replacement."
}

func (t *WriteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}

func (t *WriteTool) Permission() permissions.Level {
	return permissions.LevelWrite
}

func (t *WriteTool) IsConcurrencySafe() bool {
	return false // file writes are not safe to run concurrently
}

func (t *WriteTool) Execute(_ context.Context, input json.RawMessage) (toolkit.Result, error) {
	var payload writeInput
	if err := json.Unmarshal(input, &payload); err != nil {
		return toolkit.Result{}, err
	}

	path, err := toolkit.ResolvePath(t.rootDir, payload.Path)
	if err != nil {
		return toolkit.Result{}, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return toolkit.Result{}, err
	}

	if err := fsutil.WriteFileAtomic(path, []byte(payload.Content), 0o644); err != nil {
		return toolkit.Result{}, err
	}

	return toolkit.Result{Output: "wrote " + path}, nil
}
