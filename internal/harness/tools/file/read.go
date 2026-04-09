package file

import (
	"context"
	"encoding/json"
	"os"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type ReadTool struct {
	rootDir string
}

type readInput struct {
	Path string `json:"path"`
}

func NewReadTool(rootDir string) *ReadTool {
	return &ReadTool{rootDir: rootDir}
}

func (t *ReadTool) Name() string {
	return "file_read"
}

func (t *ReadTool) Description() string {
	return "Read a UTF-8 text file from the project root."
}

func (t *ReadTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}

func (t *ReadTool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *ReadTool) IsConcurrencySafe() bool {
	return true // reads are safe to run concurrently
}

func (t *ReadTool) Execute(_ context.Context, input json.RawMessage) (toolkit.Result, error) {
	var payload readInput
	if err := json.Unmarshal(input, &payload); err != nil {
		return toolkit.Result{}, err
	}

	path, err := toolkit.ResolvePath(t.rootDir, payload.Path)
	if err != nil {
		return toolkit.Result{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return toolkit.Result{}, err
	}

	return toolkit.Result{Output: string(data)}, nil
}
