package notebook

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotebookEditToolAppendCell(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.ipynb")
	if err := os.WriteFile(path, []byte(`{"metadata":{"kernelspec":{"name":"python3"}},"nbformat":4,"nbformat_minor":5,"cells":[{"cell_type":"markdown","source":["# title\n"],"metadata":{"id":"a1"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewEditTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":      "test.ipynb",
		"operation": "append",
		"cell_type": "code",
		"source":    "print('hi')\n",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("notebook edit execute: %v", err)
	}
	if !strings.Contains(result.Output, "updated notebook") {
		t.Fatalf("unexpected output: %q", result.Output)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "print('hi')") {
		t.Fatalf("expected appended cell, got %s", string(updated))
	}
	if !strings.Contains(string(updated), `"nbformat": 4`) {
		t.Fatalf("expected top-level notebook fields to be preserved, got %s", string(updated))
	}
	if !strings.Contains(string(updated), `"kernelspec"`) {
		t.Fatalf("expected metadata to be preserved, got %s", string(updated))
	}
}
