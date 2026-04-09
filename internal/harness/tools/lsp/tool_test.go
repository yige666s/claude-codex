package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lspapi "github.com/ding/claude-code/claude-go/internal/app/lsp"
)

func TestToolWorkspaceSymbols(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Hello() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewTool(root, lspapi.NewLocalManager(root))
	input, _ := json.Marshal(map[string]any{"action": "workspace_symbols", "query": "Hello"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result.Output, "Hello") {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}
