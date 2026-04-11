package nativeinstaller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.json")
	content := VersionLockContent{
		PID:      os.Getpid(),
		Version:  "1.0.0",
		ExecPath: "/usr/local/bin/claude",
	}
	if err := WriteLockFile(path, content); err != nil {
		t.Fatalf("WriteLockFile() error = %v", err)
	}
	read, err := ReadLockContent(path)
	if err != nil {
		t.Fatalf("ReadLockContent() error = %v", err)
	}
	if read.Version != content.Version {
		t.Fatalf("unexpected content %#v", read)
	}
}

func TestIsLockActiveUsesProcessCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.json")
	if err := WriteLockFile(path, VersionLockContent{
		PID:      os.Getpid(),
		Version:  "1.0.0",
		ExecPath: "/usr/local/bin/claude",
	}); err != nil {
		t.Fatalf("WriteLockFile() error = %v", err)
	}
	if !IsLockActive(path) {
		t.Fatal("expected current process lock to be active")
	}
}
