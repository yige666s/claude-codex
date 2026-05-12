package brief

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const (
	ToolName       = "SendUserMessage"
	FileToolName   = "SendUserFile"
	LegacyToolName = "Brief"
)

type Tool struct {
	name       string
	workingDir string
	uploader   Uploader
}

type input struct {
	Message     string   `json:"message"`
	Attachments []string `json:"attachments,omitempty"`
	Status      string   `json:"status"`
}

type fileInput struct {
	Files []string `json:"files"`
}

type output struct {
	Message     string       `json:"message"`
	Attachments []attachment `json:"attachments,omitempty"`
	SentAt      string       `json:"sentAt,omitempty"`
}

type fileOutput struct {
	Files  []attachment `json:"files"`
	SentAt string       `json:"sentAt,omitempty"`
}

type attachment struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	IsImage  bool   `json:"isImage"`
	FileUUID string `json:"file_uuid,omitempty"`
}

type FileTool struct {
	workingDir string
	uploader   Uploader
}

func NewTool(workingDir string) toolkit.Tool {
	return &Tool{name: ToolName, workingDir: workingDir}
}

func NewToolWithUploader(workingDir string, uploader Uploader) toolkit.Tool {
	return &Tool{name: ToolName, workingDir: workingDir, uploader: uploader}
}

func NewLegacyTool(workingDir string) toolkit.Tool {
	return &Tool{name: LegacyToolName, workingDir: workingDir}
}

func NewLegacyToolWithUploader(workingDir string, uploader Uploader) toolkit.Tool {
	return &Tool{name: LegacyToolName, workingDir: workingDir, uploader: uploader}
}

func NewFileTool(workingDir string) toolkit.Tool {
	return &FileTool{workingDir: workingDir}
}

func NewFileToolWithUploader(workingDir string, uploader Uploader) toolkit.Tool {
	return &FileTool{workingDir: workingDir, uploader: uploader}
}

func (t *Tool) Name() string {
	return t.name
}

func (t *Tool) Description() string {
	return "Send a markdown message to the user, optionally with file attachments."
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "message": {
      "type": "string",
      "description": "The message for the user. Supports markdown formatting."
    },
    "attachments": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Optional file paths, absolute or relative to the working directory, to attach."
    },
    "status": {
      "type": "string",
      "enum": ["normal", "proactive"],
      "description": "Use normal when replying to the user, proactive when surfacing unsolicited status."
    }
  },
  "required": ["message", "status"]
}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *Tool) IsConcurrencySafe() bool {
	return true
}

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, fmt.Errorf("%s: invalid input: %w", t.name, err)
	}
	in.Message = strings.TrimSpace(in.Message)
	if in.Message == "" {
		return toolkit.Result{}, fmt.Errorf("%s: message is required", t.name)
	}
	switch in.Status {
	case "normal", "proactive":
	default:
		return toolkit.Result{}, fmt.Errorf("%s: status must be normal or proactive", t.name)
	}

	attachments, err := resolveAttachments(ctx, t.effectiveWorkingDir(), t.uploader, in.Attachments)
	if err != nil {
		return toolkit.Result{}, err
	}
	out := output{
		Message:     in.Message,
		Attachments: attachments,
		SentAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(out)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}

func (t *Tool) effectiveWorkingDir() string {
	if strings.TrimSpace(t.workingDir) != "" {
		return t.workingDir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func (t *FileTool) Name() string {
	return FileToolName
}

func (t *FileTool) Description() string {
	return "Send one or more files to the user."
}

func (t *FileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "files": {
      "type": "array",
      "items": {"type": "string"},
      "description": "File paths, absolute or relative to the working directory, to send to the user."
    }
  },
  "required": ["files"]
}`)
}

func (t *FileTool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *FileTool) IsConcurrencySafe() bool {
	return true
}

func (t *FileTool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in fileInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, fmt.Errorf("%s: invalid input: %w", FileToolName, err)
	}
	if len(in.Files) == 0 {
		return toolkit.Result{}, fmt.Errorf("%s: files is required", FileToolName)
	}
	files, err := resolveAttachments(ctx, t.effectiveWorkingDir(), t.uploader, in.Files)
	if err != nil {
		return toolkit.Result{}, err
	}
	out := fileOutput{
		Files:  files,
		SentAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(out)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}

func (t *FileTool) effectiveWorkingDir() string {
	if strings.TrimSpace(t.workingDir) != "" {
		return t.workingDir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func resolveAttachments(ctx context.Context, workingDir string, uploader Uploader, rawPaths []string) ([]attachment, error) {
	if len(rawPaths) == 0 {
		return nil, nil
	}
	resolved := make([]attachment, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		fullPath, err := resolvePath(workingDir, rawPath)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("attachment %q does not exist. Current working directory: %s", rawPath, workingDir)
			}
			if os.IsPermission(err) {
				return nil, fmt.Errorf("attachment %q is not accessible (permission denied)", rawPath)
			}
			return nil, fmt.Errorf("attachment %q: %w", rawPath, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("attachment %q is not a regular file", rawPath)
		}
		item := attachment{
			Path:    fullPath,
			Size:    info.Size(),
			IsImage: isImagePath(fullPath),
		}
		if uploader != nil {
			if uuid, err := uploader.UploadAttachment(ctx, item.Path, item.Size); err == nil {
				item.FileUUID = uuid
			}
		}
		resolved = append(resolved, item)
	}
	return resolved, nil
}

func resolvePath(workingDir string, rawPath string) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("attachment path is required")
	}
	if strings.HasPrefix(rawPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if rawPath == "~" {
			rawPath = home
		} else if strings.HasPrefix(rawPath, "~/") {
			rawPath = filepath.Join(home, strings.TrimPrefix(rawPath, "~/"))
		}
	}
	if filepath.IsAbs(rawPath) {
		return filepath.Clean(rawPath), nil
	}
	return filepath.Join(workingDir, rawPath), nil
}

func isImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".heic", ".heif":
		return true
	default:
		return false
	}
}
