package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-codex/internal/harness/memdir"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
	"claude-codex/internal/public/fsutil"
)

type EditTool struct {
	rootDir string
}

type editInput struct {
	FilePath   string `json:"file_path"`
	Path       string `json:"path,omitempty"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func NewEditTool(rootDir string) *EditTool {
	return &EditTool{rootDir: rootDir}
}

func (t *EditTool) Name() string {
	return EditToolName
}

func (t *EditTool) Description() string {
	return "Replace text in a file under the project root."
}

func (t *EditTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string","description":"The absolute path to the file to modify"},"old_string":{"type":"string","description":"The text to replace"},"new_string":{"type":"string","description":"The text to replace it with; must be different from old_string"},"replace_all":{"type":"boolean","description":"Replace all occurrences of old_string"}},"required":["file_path","old_string","new_string"]}`)
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
	if payload.OldString == payload.NewString {
		return toolkit.Result{}, fmt.Errorf("no changes to make: old_string and new_string are exactly the same")
	}

	path, err := toolkit.ResolvePath(t.rootDir, payload.filePath())
	if err != nil {
		return toolkit.Result{}, err
	}

	if err := memdir.CheckTeamMemSecrets(path, payload.NewString, t.rootDir); err != nil {
		return toolkit.Result{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && payload.OldString == "" {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return toolkit.Result{}, err
			}
			if err := fsutil.WriteFileAtomic(path, []byte(payload.NewString), 0o644); err != nil {
				return toolkit.Result{}, err
			}
			output, err := encodeOutput(editOutput{
				FilePath:        path,
				OldString:       payload.OldString,
				NewString:       payload.NewString,
				OriginalFile:    "",
				StructuredPatch: []patchHunk{},
				UserModified:    false,
				ReplaceAll:      payload.ReplaceAll,
			})
			if err != nil {
				return toolkit.Result{}, err
			}
			return toolkit.Result{Output: output}, nil
		}
		return toolkit.Result{}, err
	}

	content := string(data)
	if payload.OldString == "" {
		return toolkit.Result{}, fmt.Errorf("old_string is required when editing an existing file")
	}
	count := strings.Count(content, payload.OldString)
	if count == 0 {
		return toolkit.Result{}, fmt.Errorf("target text not found in %s", path)
	}
	if count > 1 && !payload.ReplaceAll {
		return toolkit.Result{}, fmt.Errorf("old_string appears %d times in %s; set replace_all to true or provide a more specific old_string", count, path)
	}

	updated := strings.Replace(content, payload.OldString, payload.NewString, 1)
	if payload.ReplaceAll {
		updated = strings.ReplaceAll(content, payload.OldString, payload.NewString)
	}

	if err := fsutil.WriteFileAtomic(path, []byte(updated), 0o644); err != nil {
		return toolkit.Result{}, err
	}

	output, err := encodeOutput(editOutput{
		FilePath:        path,
		OldString:       payload.OldString,
		NewString:       payload.NewString,
		OriginalFile:    content,
		StructuredPatch: structuredPatch(content, updated),
		UserModified:    false,
		ReplaceAll:      payload.ReplaceAll,
	})
	if err != nil {
		return toolkit.Result{}, err
	}

	return toolkit.Result{Output: output}, nil
}

func (in editInput) filePath() string {
	if in.FilePath != "" {
		return in.FilePath
	}
	return in.Path
}
