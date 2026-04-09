package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalManagerSearchSymbols(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc Alpha() {}\nfunc Beta() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager := NewLocalManager()
	symbols, err := manager.SearchSymbols(context.Background(), root, "alp", 10)
	if err != nil {
		t.Fatalf("search symbols: %v", err)
	}
	if len(symbols) != 1 || !strings.Contains(symbols[0].Name, "Alpha") {
		t.Fatalf("unexpected symbols: %#v", symbols)
	}
}
