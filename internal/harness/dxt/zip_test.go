package dxt

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestIsPathSafe(t *testing.T) {
	if !IsPathSafe("dir/file.txt") {
		t.Fatal("expected relative file path to be safe")
	}
	if IsPathSafe("../etc/passwd") {
		t.Fatal("expected traversal path to be unsafe")
	}
}

func TestUnzipFileRejectsUnsafePath(t *testing.T) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create("../evil.txt")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := entry.Write([]byte("boom")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := UnzipFile(buf.Bytes()); err == nil {
		t.Fatal("expected unsafe zip path to be rejected")
	}
}
