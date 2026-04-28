package skill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-codex/internal/harness/skills"
	agenttool "claude-codex/internal/harness/tools/agent"
)

func TestForkedSkillUsesSubagentRunner(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, "skills", "review")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatalf("mkdir skill root: %v", err)
	}
	content := `---
name: Review
description: Review code
context: fork
agent: code-reviewer
model: opus
---

Review $ARGUMENTS
`
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	manager := skills.NewSkillManager()
	if err := manager.LoadSkillsFromDirectory(filepath.Join(tmpDir, "skills"), skills.SourceFile); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	var captured agenttool.Request
	tool := NewToolWithRunner(manager, "/workspace", func(_ context.Context, req agenttool.Request) (string, error) {
		captured = req
		return "subagent-result", nil
	})
	payload, _ := json.Marshal(Input{Skill: "review", Args: "src/auth.go"})
	result, err := tool.Execute(context.Background(), payload)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Output != "subagent-result" {
		t.Fatalf("expected subagent output, got %q", result.Output)
	}
	if captured.SubagentType != "code-reviewer" || captured.Model != "opus" || captured.WorkingDir != "/workspace" {
		t.Fatalf("unexpected subagent request: %+v", captured)
	}
	for _, want := range []string{
		"<command-name>/review</command-name>",
		"Review src/auth.go",
	} {
		if !strings.Contains(captured.Prompt, want) {
			t.Fatalf("expected fork prompt to contain %q, got %q", want, captured.Prompt)
		}
	}
}

func TestForkedSkillRequiresRunner(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, "skills", "forked")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatalf("mkdir skill root: %v", err)
	}
	content := "---\nname: Forked\ncontext: fork\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	manager := skills.NewSkillManager()
	if err := manager.LoadSkillsFromDirectory(filepath.Join(tmpDir, "skills"), skills.SourceFile); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	tool := NewTool(manager)
	payload, _ := json.Marshal(Input{Skill: "forked"})
	_, err := tool.Execute(context.Background(), payload)
	if err == nil || !strings.Contains(err.Error(), "requires a configured subagent runner") {
		t.Fatalf("expected missing runner error, got %v", err)
	}
}
