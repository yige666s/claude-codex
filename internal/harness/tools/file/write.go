package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"claude-codex/internal/harness/memdir"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
	"claude-codex/internal/public/fsutil"
)

type WriteTool struct {
	rootDir string
}

type writeInput struct {
	FilePath string `json:"file_path"`
	Path     string `json:"path,omitempty"`
	Content  string `json:"content"`
}

func NewWriteTool(rootDir string) *WriteTool {
	return &WriteTool{rootDir: rootDir}
}

func (t *WriteTool) Name() string {
	return WriteToolName
}

func (t *WriteTool) Description() string {
	return "Write a UTF-8 text file under the project root using atomic replacement."
}

func (t *WriteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string","description":"The absolute path to the file to write"},"content":{"type":"string","description":"The content to write to the file"}},"required":["file_path","content"]}`)
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

	path, err := toolkit.ResolvePath(t.rootDir, payload.filePath())
	if err != nil {
		return toolkit.Result{}, err
	}

	if err := memdir.CheckTeamMemSecrets(path, payload.Content, t.rootDir); err != nil {
		return toolkit.Result{}, err
	}

	var original *string
	if data, err := os.ReadFile(path); err == nil {
		value := string(data)
		original = &value
	} else if !os.IsNotExist(err) {
		return toolkit.Result{}, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return toolkit.Result{}, err
	}

	if err := fsutil.WriteFileAtomic(path, []byte(payload.Content), 0o644); err != nil {
		return toolkit.Result{}, err
	}

	outputType := "create"
	patch := []patchHunk{}
	if original != nil {
		outputType = "update"
		patch = structuredPatch(*original, payload.Content)
	}

	output, err := encodeOutput(writeOutput{
		Type:            outputType,
		FilePath:        path,
		Content:         payload.Content,
		StructuredPatch: patch,
		OriginalFile:    original,
	})
	if err != nil {
		return toolkit.Result{}, err
	}

	return toolkit.Result{Output: output}, nil
}

func (in writeInput) filePath() string {
	if in.FilePath != "" {
		return in.FilePath
	}
	return in.Path
}
