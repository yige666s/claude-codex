package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentToolUsesRunner(t *testing.T) {
	tool := NewTool("/tmp/project", func(_ context.Context, workingDir, prompt string) (string, error) {
		return workingDir + ":" + prompt, nil
	})

	input, _ := json.Marshal(map[string]any{"prompt": "inspect files"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("agent execute: %v", err)
	}
	if !strings.Contains(result.Output, "/tmp/project:inspect files") {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}
