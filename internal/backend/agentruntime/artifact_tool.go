package agentruntime

import (
	"archive/zip"
	"bytes"
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

type workspaceFileToolOutput struct {
	Kind                 string `json:"kind"`
	Filename             string `json:"filename"`
	ContentType          string `json:"content_type"`
	SizeBytes            int64  `json:"size_bytes"`
	FilePath             string `json:"file_path"`
	AssistantInstruction string `json:"assistant_instruction"`
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
	return "Create a final, user-facing artifact for the current user session, such as an image, report, slide deck, CSV, PDF, DOCX, or other deliverable the user should download or view in the Artifacts panel. Do not use this for intermediate scripts, scratch files, helper inputs, temporary logs, or user-uploaded input files; create those in the workspace with Bash or the appropriate file tool, then call Artifact only for the final deliverable file. For DOCX, PPTX, and XLSX deliverables, first generate a real file in the workspace and pass it with file_path; do not inline Office document bytes with content or content_base64."
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
				"description": "Base64-encoded bytes for binary artifacts such as images. Do not use this for DOCX, PPTX, or XLSX; generate those as workspace files and use file_path."
			},
			"file_path": {
				"type": "string",
				"description": "Path to a generated file under the current workspace. Required for DOCX, PPTX, and XLSX deliverables, and recommended whenever a skill script already wrote the output file."
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
	if len(data) == 0 {
		return toolkit.Result{}, fmt.Errorf("artifact content is empty")
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
	convertedTextDocx := false
	if shouldConvertTextToDocx(filename, contentType, input) {
		if looksLikeDocxGenerationRequest(string(data)) {
			return toolkit.Result{}, fmt.Errorf("docx artifact content looks like a generation request rather than final document body; load or compose the actual source text first, generate a real .docx workspace file, then call Artifact with file_path")
		}
		converted, err := simpleDocxBytesFromText(string(data))
		if err != nil {
			return toolkit.Result{}, err
		}
		data = converted
		contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		convertedTextDocx = true
	}
	if shouldSaveAsWorkspaceIntermediate(filename, contentType) && strings.TrimSpace(input.FilePath) == "" {
		return t.saveWorkspaceIntermediate(filename, contentType, data)
	}
	if !convertedTextDocx && officeArtifactRequiresFilePath(contentType) && strings.TrimSpace(input.FilePath) == "" {
		return toolkit.Result{}, fmt.Errorf("artifact content type %q must be submitted with file_path; generate the Office document as a real workspace file with Bash or a helper script, then call Artifact with file_path instead of content or content_base64", contentType)
	}
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

func (t *ArtifactTool) saveWorkspaceIntermediate(filename, contentType string, data []byte) (toolkit.Result, error) {
	if strings.TrimSpace(t.rootDir) == "" {
		return toolkit.Result{}, fmt.Errorf("intermediate workspace files require a workspace root; create scripts with Bash instead of Artifact")
	}
	path, err := toolkit.ResolvePath(t.rootDir, filename)
	if err != nil {
		return toolkit.Result{}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return toolkit.Result{}, err
	}
	output, err := json.Marshal(workspaceFileToolOutput{
		Kind:                 "workspace_file",
		Filename:             filename,
		ContentType:          contentType,
		SizeBytes:            int64(len(data)),
		FilePath:             filename,
		AssistantInstruction: "This is an intermediate workspace file, not a user artifact. Run or inspect it with Bash as needed. When the final user-facing deliverable is generated, call Artifact again with file_path pointing to that final file.",
	})
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(output)}, nil
}

func officeArtifactRequiresFilePath(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return true
	default:
		return false
	}
}

func shouldConvertTextToDocx(filename, _ string, input artifactToolInput) bool {
	if strings.ToLower(filepath.Ext(filename)) != ".docx" {
		return false
	}
	return input.Content != ""
}

func looksLikeDocxGenerationRequest(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	if strings.HasPrefix(clean, "/documents") || strings.HasPrefix(clean, "/docx") {
		return true
	}
	hasDocTarget := strings.Contains(clean, ".docx") ||
		strings.Contains(clean, " docx") ||
		strings.Contains(clean, "word") ||
		strings.Contains(clean, "文件名") ||
		strings.Contains(clean, "输出文件")
	if !hasDocTarget {
		return false
	}
	requestMarkers := []string{
		"请将", "请把", "请生成", "请创建", "请输出", "生成为", "渲染为", "转换为", "输出文件名",
		"please", "generate", "create", "convert", "render", "save as", "filename",
	}
	for _, marker := range requestMarkers {
		if strings.Contains(clean, marker) {
			return true
		}
	}
	return strings.Contains(clean, "/v1/artifacts/")
}

func simpleDocxBytesFromText(text string) ([]byte, error) {
	title, paragraphs := splitSimpleDocxText(text)
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	files := map[string]string{
		"[Content_Types].xml":          simpleDocxContentTypesXML(),
		"_rels/.rels":                  simpleDocxRelsXML(),
		"word/_rels/document.xml.rels": simpleDocxDocumentRelsXML(),
		"word/styles.xml":              simpleDocxStylesXML(),
		"word/document.xml":            simpleDocxDocumentXML(title, paragraphs),
	}
	for name, content := range files {
		writer, err := archive.Create(name)
		if err != nil {
			_ = archive.Close()
			return nil, err
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			_ = archive.Close()
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func splitSimpleDocxText(text string) (string, []string) {
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"))
	lines := strings.Split(text, "\n")
	title := "Word Document"
	bodyStart := 0
	for i, line := range lines {
		clean := cleanSimpleDocxLine(line)
		if clean == "" {
			continue
		}
		title = clean
		bodyStart = i + 1
		break
	}
	paragraphs := make([]string, 0)
	for _, line := range lines[bodyStart:] {
		clean := cleanSimpleDocxLine(line)
		if clean != "" && clean != title {
			paragraphs = append(paragraphs, clean)
		}
	}
	if len(paragraphs) == 0 && title != "" {
		paragraphs = append(paragraphs, title)
	}
	return title, paragraphs
}

func cleanSimpleDocxLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "#")
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "*_`- ")
	return strings.TrimSpace(line)
}

func simpleDocxContentTypesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
</Types>`
}

func simpleDocxRelsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`
}

func simpleDocxDocumentRelsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`
}

func simpleDocxStylesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal">
    <w:name w:val="Normal"/>
    <w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:eastAsia="Microsoft YaHei"/><w:sz w:val="22"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Title">
    <w:name w:val="Title"/>
    <w:basedOn w:val="Normal"/>
    <w:pPr><w:spacing w:after="240"/></w:pPr>
    <w:rPr><w:b/><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:eastAsia="Microsoft YaHei"/><w:sz w:val="34"/></w:rPr>
  </w:style>
</w:styles>`
}

func simpleDocxDocumentXML(title string, paragraphs []string) string {
	var body strings.Builder
	body.WriteString(simpleDocxParagraph(title, "Title"))
	for _, paragraph := range paragraphs {
		body.WriteString(simpleDocxParagraph(paragraph, ""))
	}
	body.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>`)
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() + `</w:body></w:document>`
}

func simpleDocxParagraph(text, style string) string {
	styleXML := ""
	if style != "" {
		styleXML = `<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`
	}
	return `<w:p>` + styleXML + `<w:r><w:t xml:space="preserve">` + escapeSimpleXML(text) + `</w:t></w:r></w:p>`
}

func escapeSimpleXML(text string) string {
	var buf bytes.Buffer
	for _, r := range text {
		switch r {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '"':
			buf.WriteString("&quot;")
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

func shouldSaveAsWorkspaceIntermediate(filename, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return false
	}
	switch ext {
	case ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".rb", ".go":
		return true
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "text/x-python", "application/x-python-code", "application/javascript", "text/javascript":
		return true
	default:
		return false
	}
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
		return nil, fmt.Errorf("artifact content type %q is not allowed for this skill; Artifact is only for final user-facing deliverables allowed by the skill policy. For intermediate scripts, scratch files, helper inputs, or logs, create files in the workspace with Bash and call Artifact only for the final deliverable.", contentType)
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
