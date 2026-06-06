package agentruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const (
	ArtifactToolName       = "Artifact"
	DefaultMaxArtifactSize = 64 << 20
)

type ArtifactTool struct {
	writer   ArtifactWriter
	rootDir  string
	maxBytes int
}

type artifactContentTypeWriter struct {
	base    ArtifactWriter
	allowed []string
}

type artifactToolInput struct {
	Filename      string `json:"filename"`
	ContentType   string `json:"content_type,omitempty"`
	Content       string `json:"content,omitempty"`
	ContentBase64 string `json:"content_base64,omitempty"`
	FilePath      string `json:"file_path,omitempty"`
}

type artifactToolOutput struct {
	ID                   string `json:"id"`
	Kind                 string `json:"kind"`
	Filename             string `json:"filename"`
	ContentType          string `json:"content_type"`
	SizeBytes            int64  `json:"size_bytes"`
	DownloadPath         string `json:"download_path"`
	AssistantInstruction string `json:"assistant_instruction,omitempty"`
}

func NewArtifactTool(writer ArtifactWriter, rootDir ...string) toolkit.Tool {
	tool := &ArtifactTool{writer: writer, maxBytes: DefaultMaxArtifactSize}
	if len(rootDir) > 0 {
		tool.rootDir = rootDir[0]
	}
	return tool
}

func NewArtifactToolWithLimit(writer ArtifactWriter, rootDir string, maxBytes int64) toolkit.Tool {
	tool := &ArtifactTool{writer: writer, rootDir: rootDir, maxBytes: int(maxBytes)}
	if tool.maxBytes <= 0 {
		tool.maxBytes = int(DefaultMaxAssetBytes)
	}
	return tool
}

func (t *ArtifactTool) Name() string {
	return ArtifactToolName
}

func (t *ArtifactTool) Description() string {
	return "Create a generated artifact for the current user session. Use this for outputs produced by the agent or a skill, such as images, reports, slides, CSV files, or other generated files. Do not use it for user-uploaded input files."
}

func (t *ArtifactTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"filename": {
				"type": "string",
				"description": "Name of the generated artifact, including extension. Example: chart.png"
			},
			"content_type": {
				"type": "string",
				"description": "MIME type. If omitted, it is inferred from the filename extension when possible."
			},
			"content": {
				"type": "string",
				"description": "UTF-8 text content for text artifacts. Use content_base64 for binary files."
			},
			"content_base64": {
				"type": "string",
				"description": "Base64-encoded bytes for binary artifacts such as images or documents."
			},
			"file_path": {
				"type": "string",
				"description": "Path to a generated file under the current workspace. Use this when a skill script already wrote the output file."
			}
		},
		"required": ["filename"]
	}`)
}

func (t *ArtifactTool) Permission() permissions.Level {
	return permissions.LevelWrite
}

func (t *ArtifactTool) IsConcurrencySafe() bool {
	return true
}

func (t *ArtifactTool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	if t.writer == nil {
		return toolkit.Result{}, fmt.Errorf("artifact writer is not configured")
	}
	var input artifactToolInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return toolkit.Result{}, err
	}
	filename := filepath.Base(strings.TrimSpace(input.Filename))
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		return toolkit.Result{}, fmt.Errorf("filename is required")
	}
	data, err := t.artifactBytes(input)
	if err != nil {
		return toolkit.Result{}, err
	}
	maxBytes := t.maxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxArtifactSize
	}
	if len(data) > maxBytes {
		return toolkit.Result{}, fmt.Errorf("artifact exceeds max size of %d bytes", maxBytes)
	}
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(filename))
	}
	contentType = normalizeArtifactToolContentType(filename, contentType)
	started := time.Now()
	artifact, err := t.writer.Write(ctx, filename, contentType, data)
	duration := time.Since(started)
	if err != nil {
		emitArtifactMetric(ctx, filename, contentType, len(data), duration, "", err)
		return toolkit.Result{}, err
	}
	emitArtifactMetric(ctx, filename, contentType, len(data), duration, artifact.ID, nil)
	output, err := json.Marshal(artifactToolOutput{
		ID:                   artifact.ID,
		Kind:                 artifact.Kind,
		Filename:             artifact.Filename,
		ContentType:          artifact.ContentType,
		SizeBytes:            artifact.SizeBytes,
		DownloadPath:         "/v1/artifacts/" + artifact.ID,
		AssistantInstruction: "Use this metadata as internal context only. Tell the user the generated artifact is ready in the Artifacts panel. Do not paste the artifact body/content into chat. Do not expose raw JSON, artifact IDs, object paths, or download paths unless the user explicitly asks for technical details.",
	})
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(output)}, nil
}

func emitArtifactMetric(ctx context.Context, filename, contentType string, sizeBytes int, duration time.Duration, artifactID string, runErr error) {
	data := map[string]any{
		"filename":     filename,
		"content_type": contentType,
		"size_bytes":   sizeBytes,
		"duration_ms":  duration.Milliseconds(),
		"success":      runErr == nil,
	}
	if artifactID != "" {
		data["artifact_id"] = artifactID
	}
	if runErr != nil {
		data["error"] = runErr.Error()
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	content := fmt.Sprintf("Artifact %s completed in %d ms", filename, duration.Milliseconds())
	if runErr != nil {
		content = fmt.Sprintf("Artifact %s failed after %d ms", filename, duration.Milliseconds())
	}
	emitJobEventFromContext(ctx, Event{Type: "artifact_metric", Role: "tool", Content: content, Data: raw})
}

func normalizeArtifactToolContentType(filename, contentType string) string {
	contentType = normalizedContentType(contentType)
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown":
		if contentType == "" || contentType == "text/plain" {
			return "text/markdown"
		}
	}
	return contentType
}

func (t *ArtifactTool) artifactBytes(input artifactToolInput) ([]byte, error) {
	hasText := input.Content != ""
	hasBase64 := strings.TrimSpace(input.ContentBase64) != ""
	hasFile := strings.TrimSpace(input.FilePath) != ""
	provided := 0
	for _, ok := range []bool{hasText, hasBase64, hasFile} {
		if ok {
			provided++
		}
	}
	if provided != 1 {
		return nil, fmt.Errorf("provide exactly one of content, content_base64, or file_path")
	}
	switch {
	case hasBase64:
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(input.ContentBase64))
		if err != nil {
			return nil, fmt.Errorf("invalid content_base64: %w", err)
		}
		return data, nil
	case hasText:
		return []byte(input.Content), nil
	case hasFile:
		if strings.TrimSpace(t.rootDir) == "" {
			return nil, fmt.Errorf("file_path is unavailable without a workspace root")
		}
		path, err := toolkit.ResolvePath(t.rootDir, input.FilePath)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("file_path must point to a file")
		}
		if t.maxBytes > 0 && info.Size() > int64(t.maxBytes) {
			return nil, fmt.Errorf("artifact exceeds max size of %d bytes", t.maxBytes)
		}
		return os.ReadFile(path)
	default:
		return nil, fmt.Errorf("content, content_base64, or file_path is required")
	}
}

func NewArtifactContentTypeWriter(base ArtifactWriter, allowed []string) ArtifactWriter {
	allowed = cleanStringSlice(allowed)
	if base == nil || len(allowed) == 0 {
		return base
	}
	return artifactContentTypeWriter{base: base, allowed: allowed}
}

func (w artifactContentTypeWriter) Write(ctx context.Context, filename, contentType string, data []byte) (*Artifact, error) {
	if !artifactContentTypeAllowed(contentType, w.allowed) {
		return nil, fmt.Errorf("artifact content type %q is not allowed for this skill", contentType)
	}
	return w.base.Write(ctx, filename, contentType, data)
}

func artifactContentTypeAllowed(contentType string, allowed []string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	for _, pattern := range allowed {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		switch {
		case pattern == "":
			continue
		case pattern == "*":
			return true
		case strings.HasSuffix(pattern, "/*"):
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(contentType, prefix) {
				return true
			}
		case contentType == pattern:
			return true
		}
	}
	return false
}
