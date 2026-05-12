package brief

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toolkit "claude-codex/internal/harness/tools"
)

func executeBriefTool(t *testing.T, tool toolkit.Tool, payload string) output {
	t.Helper()
	result, err := tool.Execute(context.Background(), json.RawMessage(payload))
	if err != nil {
		t.Fatalf("Execute(%s) error = %v", payload, err)
	}
	var out output
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output %q: %v", result.Output, err)
	}
	return out
}

func TestBriefToolDeliversMessageWithoutAttachments(t *testing.T) {
	tool := NewTool(t.TempDir())

	out := executeBriefTool(t, tool, `{"message":"Done **now**.","status":"normal"}`)
	if out.Message != "Done **now**." {
		t.Fatalf("unexpected message: %#v", out)
	}
	if out.SentAt == "" {
		t.Fatalf("expected sentAt timestamp, got %#v", out)
	}
	if len(out.Attachments) != 0 {
		t.Fatalf("expected no attachments, got %#v", out.Attachments)
	}
}

func TestBriefToolResolvesAttachmentMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "screens", "result.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("png bytes"), 0o644); err != nil {
		t.Fatalf("write attachment: %v", err)
	}
	tool := NewTool(root)

	out := executeBriefTool(t, tool, `{"message":"See attachment.","status":"proactive","attachments":["screens/result.png"]}`)
	if out.Message != "See attachment." || len(out.Attachments) != 1 {
		t.Fatalf("unexpected output: %#v", out)
	}
	attachment := out.Attachments[0]
	if attachment.Path != path {
		t.Fatalf("expected absolute path %q, got %q", path, attachment.Path)
	}
	if attachment.Size != int64(len("png bytes")) {
		t.Fatalf("expected size %d, got %d", len("png bytes"), attachment.Size)
	}
	if !attachment.IsImage {
		t.Fatalf("expected png attachment to be marked as image")
	}
}

func TestBriefToolAddsUploadedAttachmentUUID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "screens", "result.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("png bytes"), 0o644); err != nil {
		t.Fatalf("write attachment: %v", err)
	}
	uploader := &stubUploader{uuid: "file-uuid-123"}
	tool := NewToolWithUploader(root, uploader)

	out := executeBriefTool(t, tool, `{"message":"See attachment.","status":"proactive","attachments":["screens/result.png"]}`)
	if len(out.Attachments) != 1 || out.Attachments[0].FileUUID != "file-uuid-123" {
		t.Fatalf("expected uploaded uuid in attachment output, got %#v", out.Attachments)
	}
	if uploader.path != path || uploader.size != int64(len("png bytes")) {
		t.Fatalf("unexpected upload call: path=%q size=%d", uploader.path, uploader.size)
	}
}

func TestOAuthUploaderPostsMultipartFile(t *testing.T) {
	var seenAuth string
	var seenFilename string
	var seenContentType string
	var seenBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/file_upload" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		seenAuth = r.Header.Get("Authorization")
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("MultipartReader: %v", err)
		}
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		seenFilename = part.FileName()
		seenContentType = part.Header.Get("Content-Type")
		seenBody, err = io.ReadAll(part)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"file_uuid":"uploaded-uuid"}`))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "result.png")
	if err := os.WriteFile(path, []byte("png bytes"), 0o644); err != nil {
		t.Fatalf("write attachment: %v", err)
	}
	uploader := NewOAuthUploader(&stubAuth{token: "access-token", baseURL: server.URL}, server.Client())

	uuid, err := uploader.UploadAttachment(context.Background(), path, int64(len("png bytes")))
	if err != nil {
		t.Fatalf("UploadAttachment() error = %v", err)
	}
	if uuid != "uploaded-uuid" {
		t.Fatalf("uuid = %q", uuid)
	}
	if seenAuth != "Bearer access-token" {
		t.Fatalf("Authorization = %q", seenAuth)
	}
	if seenFilename != "result.png" || seenContentType != "image/png" || !bytes.Equal(seenBody, []byte("png bytes")) {
		t.Fatalf("unexpected multipart file filename=%q contentType=%q body=%q", seenFilename, seenContentType, seenBody)
	}
}

func TestOAuthUploaderSkipsFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	if err := os.WriteFile(path, []byte("too large"), 0o644); err != nil {
		t.Fatalf("write attachment: %v", err)
	}
	uploader := NewOAuthUploader(&stubAuth{token: "access-token", baseURL: "https://example.invalid"}, nil)

	uuid, err := uploader.UploadAttachment(context.Background(), path, MaxUploadBytes+1)
	if err != nil {
		t.Fatalf("UploadAttachment() error = %v", err)
	}
	if uuid != "" {
		t.Fatalf("expected upload to be skipped, got uuid %q", uuid)
	}
}

func TestUploadEnabledFromEnv(t *testing.T) {
	t.Setenv("CLAUDE_CODE_BRIEF_UPLOAD", "true")
	if !UploadEnabledFromEnv() {
		t.Fatal("expected upload to be enabled")
	}
	t.Setenv("CLAUDE_CODE_BRIEF_UPLOAD", "false")
	if UploadEnabledFromEnv() {
		t.Fatal("expected upload to be disabled")
	}
}

func TestBriefToolRejectsInvalidAttachments(t *testing.T) {
	root := t.TempDir()
	tool := NewTool(root)

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"message":"bad","status":"normal","attachments":["missing.log"]}`))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing attachment error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), json.RawMessage(`{"message":"bad","status":"normal","attachments":["."]}`))
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected directory attachment error, got %v", err)
	}
}

type stubUploader struct {
	uuid string
	path string
	size int64
}

func (s *stubUploader) UploadAttachment(_ context.Context, path string, size int64) (string, error) {
	s.path = path
	s.size = size
	return s.uuid, nil
}

type stubAuth struct {
	token   string
	baseURL string
}

func (s *stubAuth) GetAccessToken(context.Context) (string, error) {
	return s.token, nil
}

func (s *stubAuth) BaseAPIURL() string {
	return s.baseURL
}

func TestLegacyBriefToolName(t *testing.T) {
	tool := NewLegacyTool(t.TempDir())
	if tool.Name() != LegacyToolName {
		t.Fatalf("expected legacy name %q, got %q", LegacyToolName, tool.Name())
	}
}
