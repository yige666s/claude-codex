package memdir

import (
	"path/filepath"
	"testing"
)

func TestDetectSessionFileTypeAndPattern(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_HOME", base)
	sessionMemory := filepath.Join(base, "session-memory", "x.md")
	transcript := filepath.Join(base, "projects", "demo", "run.jsonl")

	if got := DetectSessionFileType(sessionMemory); got != "session_memory" {
		t.Fatalf("unexpected session memory detection %q", got)
	}
	if got := DetectSessionFileType(transcript); got != "session_transcript" {
		t.Fatalf("unexpected transcript detection %q", got)
	}
	if got := DetectSessionPatternType("projects/*.jsonl"); got != "session_transcript" {
		t.Fatalf("unexpected pattern detection %q", got)
	}
}

func TestMemoryScopeAndManagedDetection(t *testing.T) {
	projectRoot := t.TempDir()
	personal := filepath.Join(GetAutoMemPath(projectRoot), "note.md")
	if got := MemoryScopeForPath(personal, projectRoot); got != MemoryScopePersonal {
		t.Fatalf("unexpected memory scope %q", got)
	}
	if !IsAutoManagedMemoryFile(personal, projectRoot) {
		t.Fatal("expected auto memory file to be managed")
	}
	if !IsMemoryDirectory(GetAutoMemPath(projectRoot), projectRoot) {
		t.Fatal("expected auto memory dir to be detected")
	}
}
