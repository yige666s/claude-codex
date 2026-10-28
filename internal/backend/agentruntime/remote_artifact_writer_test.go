package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type finalCollector struct {
	calls int
	files []RemoteSkillArtifact
}

func (c *finalCollector) CollectSkillArtifacts(_ context.Context, _ RemoteSkillRequest) ([]RemoteSkillArtifact, error) {
	c.calls++
	return c.files, nil
}
func TestRemoteArtifactFilePathReadsVerifiedTaskNotHost(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "generated-artifacts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "generated-artifacts", "report.txt"), []byte("host-private"), 0600); err != nil {
		t.Fatal(err)
	}
	collector := &finalCollector{files: []RemoteSkillArtifact{{Filename: "report.txt", Content: []byte("remote-task-result")}}}
	base := testArtifactWriter{}
	writer := remoteSkillArtifactWriter{base: base, collector: collector, request: RemoteSkillRequest{UserID: "alice", SkillName: "demo"}}
	tool := NewArtifactToolWithLimit(NewArtifactContentTypeWriter(writer, []string{"text/plain"}), root, 1<<20)
	input, _ := json.Marshal(map[string]string{"filename": "report.txt", "content_type": "text/plain", "file_path": "generated-artifacts/report.txt"})
	got, err := tool.Execute(context.Background(), input)
	if err != nil || !strings.Contains(got.Output, "report.txt") {
		t.Fatalf("remote tool: %+v %v", got, err)
	}
	if collector.calls != 1 {
		t.Fatalf("remote collector calls=%d", collector.calls)
	}
	for _, bad := range []string{"../report.txt", "generated-artifacts/../report.txt", "/etc/passwd", "other/report.txt", "generated-artifacts/missing.txt"} {
		input, _ = json.Marshal(map[string]string{"filename": "report.txt", "content_type": "text/plain", "file_path": bad})
		if _, err := tool.Execute(context.Background(), input); err == nil {
			t.Fatalf("unsafe remote artifact path accepted: %q", bad)
		}
	}
}
func TestRemoteArtifactCollectorMissingFailsClosed(t *testing.T) {
	writer := remoteSkillArtifactWriter{base: testArtifactWriter{}, collector: &finalCollector{}, request: RemoteSkillRequest{UserID: "alice"}}
	_, err := writer.ReadArtifactFile(context.Background(), "outputs/nope.txt")
	if !errors.Is(err, os.ErrNotExist) && err == nil {
		t.Fatal("missing remote file accepted")
	}
}
