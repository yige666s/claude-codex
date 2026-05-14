package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneLocalUploadedArtifactsDeletesOnlyOldUploadedStagingFiles(t *testing.T) {
	root := t.TempDir()
	runtime := NewRuntime(RuntimeConfig{UserWorkspaceRoot: root}, nil, nil, nil, nil)
	now := time.Now().UTC()
	store := &localArtifactCleanupStore{items: []*Artifact{
		{ID: "old", Kind: AssetKindArtifact, UserID: "alice", ObjectKey: "users/alice/artifacts/old.svg", Filename: "old.svg", CreatedAt: now.Add(-25 * time.Hour)},
		{ID: "fresh-file", Kind: AssetKindArtifact, UserID: "alice", ObjectKey: "users/alice/artifacts/fresh.svg", Filename: "fresh.svg", CreatedAt: now.Add(-25 * time.Hour)},
		{ID: "not-uploaded", Kind: AssetKindArtifact, UserID: "alice", Filename: "not-uploaded.svg", CreatedAt: now.Add(-25 * time.Hour)},
		{ID: "attachment", Kind: AssetKindAttachment, UserID: "alice", ObjectKey: "users/alice/attachments/attachment.svg", Filename: "attachment.svg", CreatedAt: now.Add(-25 * time.Hour)},
	}}
	runtime.SetArtifactService(&ArtifactService{Store: store})

	stagingDir := filepath.Join(runtime.userWorkspace("alice"), generatedArtifactStagingDir)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	oldTime := now.Add(-25 * time.Hour)
	writeStagingFile(t, stagingDir, "old.svg", oldTime)
	writeStagingFile(t, stagingDir, "fresh.svg", now)
	writeStagingFile(t, stagingDir, "not-uploaded.svg", oldTime)
	writeStagingFile(t, stagingDir, "attachment.svg", oldTime)

	result, err := runtime.PruneLocalUploadedArtifacts(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("prune local artifacts: %v", err)
	}
	if result.Deleted != 1 || result.Errors != 0 {
		t.Fatalf("unexpected prune result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(stagingDir, "old.svg")); !os.IsNotExist(err) {
		t.Fatalf("old uploaded artifact should be deleted, stat err=%v", err)
	}
	for _, filename := range []string{"fresh.svg", "not-uploaded.svg", "attachment.svg"} {
		if _, err := os.Stat(filepath.Join(stagingDir, filename)); err != nil {
			t.Fatalf("%s should remain: %v", filename, err)
		}
	}
}

func writeStagingFile(t *testing.T, dir, filename string, modTime time.Time) {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(filename), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", filename, err)
	}
}

type localArtifactCleanupStore struct {
	items []*Artifact
}

func (s *localArtifactCleanupStore) Create(context.Context, *Artifact) error {
	return nil
}

func (s *localArtifactCleanupStore) Get(context.Context, string, string, string) (*Artifact, error) {
	return nil, os.ErrNotExist
}

func (s *localArtifactCleanupStore) List(context.Context, string, string, string) ([]*Artifact, error) {
	return nil, nil
}

func (s *localArtifactCleanupStore) MarkDeleted(context.Context, string, string, string, time.Time) error {
	return nil
}

func (s *localArtifactCleanupStore) DeleteSession(context.Context, string, string) ([]*Artifact, error) {
	return nil, nil
}

func (s *localArtifactCleanupStore) DeleteUser(context.Context, string) ([]*Artifact, error) {
	return nil, nil
}

func (s *localArtifactCleanupStore) PruneDeletedBefore(context.Context, time.Time) (int, error) {
	return 0, nil
}

func (s *localArtifactCleanupStore) ListUploadedArtifactsBefore(_ context.Context, cutoff time.Time) ([]*Artifact, error) {
	var out []*Artifact
	for _, item := range s.items {
		if item.CreatedAt.Before(cutoff) {
			out = append(out, item)
		}
	}
	return out, nil
}
