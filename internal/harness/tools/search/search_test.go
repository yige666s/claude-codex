package search

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlobToolMatchesFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewGlobTool(root)
	input, _ := json.Marshal(map[string]any{"pattern": "*.go"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("glob execute: %v", err)
	}
	if !strings.Contains(result.Output, "main.go") {
		t.Fatalf("expected main.go in output, got %q", result.Output)
	}
}

func TestGlobToolSortsMatchesByModificationTime(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.go")
	newPath := filepath.Join(root, "new.go")
	if err := os.WriteFile(oldPath, []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("package new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-1 * time.Hour)
	newTime := time.Now()
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	tool := NewGlobTool(root)
	input, _ := json.Marshal(map[string]any{"pattern": "*.go"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("glob execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) < 2 || lines[0] != "new.go" || lines[1] != "old.go" {
		t.Fatalf("expected newest file first, got %q", result.Output)
	}
}

func TestGrepToolDefaultsToFilesWithMatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewGrepTool(root)
	input, _ := json.Marshal(map[string]any{"pattern": "package main"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("grep execute: %v", err)
	}
	if strings.TrimSpace(result.Output) != "main.go" {
		t.Fatalf("expected grep hit, got %q", result.Output)
	}
}

func TestGrepToolSupportsContentModeAndGlobFilter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.txt"), []byte("package text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewGrepTool(root)
	input, _ := json.Marshal(map[string]any{"pattern": "package", "output_mode": "content", "glob": "*.go"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("grep execute: %v", err)
	}
	if !strings.Contains(result.Output, "main.go:1:package main") || strings.Contains(result.Output, "main.txt") {
		t.Fatalf("unexpected grep output: %q", result.Output)
	}
}

func TestGrepToolSupportsCountMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("foo\nbar\nfoo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewGrepTool(root)
	input, _ := json.Marshal(map[string]any{"pattern": "foo", "output_mode": "count"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("grep execute: %v", err)
	}
	if !strings.Contains(result.Output, "main.go:2") {
		t.Fatalf("expected count output, got %q", result.Output)
	}
}
