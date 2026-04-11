package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectVCS(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".jj"), 0o755); err != nil {
		t.Fatalf("mkdir .jj: %v", err)
	}
	got, err := DetectVCS(dir)
	if err != nil {
		t.Fatalf("DetectVCS() error = %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("expected vcs markers, got %#v", got)
	}
}

func TestNormalizeComparablePath(t *testing.T) {
	got := NormalizeComparablePath(`a\b\c`)
	if got != "a/b/c" {
		t.Fatalf("unexpected comparable path %q", got)
	}
}
