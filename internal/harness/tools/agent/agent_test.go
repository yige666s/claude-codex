package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentToolUsesRunner(t *testing.T) {
	tool := NewTool("/tmp/project", func(_ context.Context, req Request) (string, error) {
		return req.WorkingDir + ":" + req.Prompt, nil
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

func TestAgentToolForwardsSurfaceFields(t *testing.T) {
	var got Request
	tool := NewTool("/tmp/project", func(_ context.Context, req Request) (string, error) {
		got = req
		return req.SubagentType + ":" + req.Model + ":" + req.WorkingDir, nil
	})

	input, _ := json.Marshal(map[string]any{
		"description":   "Investigate auth bug",
		"prompt":        "Inspect src/auth",
		"subagent_type": "worker",
		"model":         "opus",
		"cwd":           "/tmp/child",
		"name":          "auth-worker",
		"team_name":     "alpha",
		"mode":          "plan",
	})

	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("agent execute: %v", err)
	}
	if result.Output != "worker:opus:/tmp/child" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if got.Description != "Investigate auth bug" {
		t.Fatalf("description not forwarded: %#v", got)
	}
	if got.Name != "auth-worker" || got.TeamName != "alpha" || got.Mode != "plan" {
		t.Fatalf("metadata not forwarded: %#v", got)
	}
	if got.WorkingDir != "/tmp/child" {
		t.Fatalf("cwd should normalize into working_dir, got %#v", got)
	}
}

func TestAgentToolRejectsUnsupportedExecutionModes(t *testing.T) {
	tool := NewTool("/tmp/project", func(_ context.Context, req Request) (string, error) {
		return req.Prompt, nil
	})

	t.Run("background", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"prompt":            "Inspect src/auth",
			"run_in_background": true,
		})
		_, err := tool.Execute(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), "background") {
			t.Fatalf("expected background error, got %v", err)
		}
	})

	t.Run("isolation", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"prompt":    "Inspect src/auth",
			"isolation": "worktree",
		})
		_, err := tool.Execute(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), "isolation") {
			t.Fatalf("expected isolation error, got %v", err)
		}
	})

	t.Run("mismatched cwd", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"prompt":      "Inspect src/auth",
			"cwd":         "/tmp/a",
			"working_dir": "/tmp/b",
		})
		_, err := tool.Execute(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), "must match") {
			t.Fatalf("expected cwd mismatch error, got %v", err)
		}
	})
}

func TestAgentToolInputSchemaIncludesSurfaceFields(t *testing.T) {
	schema := string(NewTool("/tmp/project", nil).InputSchema())
	for _, key := range []string{
		`"description"`,
		`"subagent_type"`,
		`"model"`,
		`"run_in_background"`,
		`"isolation"`,
		`"cwd"`,
		`"team_name"`,
	} {
		if !strings.Contains(schema, key) {
			t.Fatalf("schema missing %s: %s", key, schema)
		}
	}
}
