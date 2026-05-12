package brief

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toolkit "claude-codex/internal/harness/tools"
)

func executeSendUserFileTool(t *testing.T, tool toolkit.Tool, payload string) fileOutput {
	t.Helper()
	result, err := tool.Execute(context.Background(), json.RawMessage(payload))
	if err != nil {
		t.Fatalf("Execute(%s) error = %v", payload, err)
	}
	var out fileOutput
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output %q: %v", result.Output, err)
	}
	return out
}

func TestSendUserFileToolResolvesFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "logs", "run.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	tool := NewFileTool(root)

	out := executeSendUserFileTool(t, tool, `{"files":["logs/run.txt"]}`)
	if len(out.Files) != 1 {
		t.Fatalf("expected one file, got %#v", out)
	}
	file := out.Files[0]
	if file.Path != path || file.Size != 5 || file.IsImage {
		t.Fatalf("unexpected file metadata: %#v", file)
	}
	if out.SentAt == "" {
		t.Fatalf("expected sentAt timestamp, got %#v", out)
	}
}

func TestSendUserFileToolAddsUploadedUUID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "screens", "result.webp")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("image"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	uploader := &stubUploader{uuid: "file-uploaded"}
	tool := NewFileToolWithUploader(root, uploader)

	out := executeSendUserFileTool(t, tool, `{"files":["screens/result.webp"]}`)
	if len(out.Files) != 1 || out.Files[0].FileUUID != "file-uploaded" {
		t.Fatalf("expected uploaded uuid in file output, got %#v", out.Files)
	}
	if uploader.path != path || uploader.size != int64(len("image")) {
		t.Fatalf("unexpected upload call: path=%q size=%d", uploader.path, uploader.size)
	}
	if !out.Files[0].IsImage {
		t.Fatalf("expected webp file to be marked as image")
	}
}

func TestSendUserFileToolRejectsInvalidFiles(t *testing.T) {
	root := t.TempDir()
	tool := NewFileTool(root)

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"files":[]}`))
	if err == nil || !strings.Contains(err.Error(), "files is required") {
		t.Fatalf("expected required files error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), json.RawMessage(`{"files":["missing.txt"]}`))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing file error, got %v", err)
	}
}
