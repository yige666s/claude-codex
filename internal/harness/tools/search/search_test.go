package search

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestGrepToolFallsBackAndFindsMatches(t *testing.T) {
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
	if !strings.Contains(result.Output, "main.go:1:package main") {
		t.Fatalf("expected grep hit, got %q", result.Output)
	}
}
