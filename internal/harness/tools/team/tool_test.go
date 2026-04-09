package team

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/harness/coordinator"
)

func TestCreateDeleteTool(t *testing.T) {
	manager := coordinator.NewManager(coordinator.Config{ScratchpadDir: t.TempDir()})
	create := NewTeamCreateTool(manager)
	raw, _ := json.Marshal(map[string]any{"name": "alpha"})
	result, err := create.Execute(context.Background(), raw)
	if err == nil {
		t.Fatalf("expected error for unimplemented feature, got: %q", result.Output)
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("expected 'not yet implemented' error, got: %v", err)
	}

	del := NewTeamDeleteTool(manager)
	result, err = del.Execute(context.Background(), raw)
	if err == nil {
		t.Fatalf("expected error for unimplemented feature, got: %q", result.Output)
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("expected 'not yet implemented' error, got: %v", err)
	}
}
