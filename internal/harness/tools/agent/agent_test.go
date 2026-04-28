package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAgentToolUsesRunner(t *testing.T) {
	tool := NewTool("/tmp/project", func(_ context.Context, request Request) (string, error) {
		if request.WorkingDir != "/tmp/project" {
			t.Fatalf("unexpected working dir: %q", request.WorkingDir)
		}
		if request.Prompt != "inspect files" {
			t.Fatalf("unexpected prompt: %q", request.Prompt)
		}
		return "ok", nil
	})

	input, _ := json.Marshal(map[string]any{"prompt": "inspect files"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("agent execute: %v", err)
	}
	if result.Output != "ok" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}

func TestAgentToolForwardsExtendedFields(t *testing.T) {
	var captured Request
	tool := NewTool("/tmp/project", func(_ context.Context, request Request) (string, error) {
		captured = request
		return "ok", nil
	})

	input, _ := json.Marshal(map[string]any{
		"prompt":        " investigate auth ",
		"description":   " Auth audit ",
		"subagent_type": " code-reviewer ",
		"model":         " gpt-5.4 ",
		"working_dir":   " /tmp/child ",
		"max_turns":     3,
	})

	if _, err := tool.Execute(context.Background(), input); err != nil {
		t.Fatalf("agent execute: %v", err)
	}

	if captured.Prompt != "investigate auth" {
		t.Fatalf("unexpected prompt: %q", captured.Prompt)
	}
	if captured.Description != "Auth audit" {
		t.Fatalf("unexpected description: %q", captured.Description)
	}
	if captured.SubagentType != "code-reviewer" {
		t.Fatalf("unexpected subagent type: %q", captured.SubagentType)
	}
	if captured.Model != "gpt-5.4" {
		t.Fatalf("unexpected model: %q", captured.Model)
	}
	if captured.WorkingDir != "/tmp/child" {
		t.Fatalf("unexpected working dir: %q", captured.WorkingDir)
	}
	if captured.MaxTurns != 3 {
		t.Fatalf("unexpected max turns: %d", captured.MaxTurns)
	}
}

func TestAgentToolStartsBackgroundTask(t *testing.T) {
	done := make(chan struct{})
	manager := NewBackgroundManager()
	tool := NewToolWithBackgroundManager("/tmp/project", func(_ context.Context, request Request) (string, error) {
		defer close(done)
		if !request.RunInBackground {
			t.Fatalf("expected background request: %+v", request)
		}
		return "background ok", nil
	}, manager)

	input, _ := json.Marshal(map[string]any{
		"prompt":            "inspect files",
		"run_in_background": true,
	})

	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("agent execute: %v", err)
	}
	var payload struct {
		AgentID string `json:"agent_id"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("decode background payload: %v", err)
	}
	if payload.AgentID == "" || payload.Status != string(BackgroundRunning) {
		t.Fatalf("unexpected background payload: %+v", payload)
	}
	<-done
	task, ok := manager.Get(payload.AgentID)
	if !ok || task.Status != BackgroundCompleted || task.Output != "background ok" {
		t.Fatalf("unexpected background task: ok=%v task=%+v", ok, task)
	}
}

func TestAgentToolForwardsWorktreeIsolation(t *testing.T) {
	var captured Request
	tool := NewTool("/tmp/project", func(_ context.Context, request Request) (string, error) {
		captured = request
		return "ok", nil
	})

	input, _ := json.Marshal(map[string]any{
		"prompt":    "inspect files",
		"isolation": "worktree",
	})

	if _, err := tool.Execute(context.Background(), input); err != nil {
		t.Fatalf("agent execute: %v", err)
	}
	if captured.Isolation != "worktree" {
		t.Fatalf("expected worktree isolation to be forwarded, got %+v", captured)
	}
}

func TestAgentToolValidatesPromptAndMaxTurns(t *testing.T) {
	tool := NewTool("/tmp/project", func(_ context.Context, request Request) (string, error) {
		t.Fatalf("runner should not be called: %+v", request)
		return "", nil
	})

	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{
			name:  "blank prompt",
			input: map[string]any{"prompt": "   "},
			want:  "prompt is required",
		},
		{
			name:  "negative max turns",
			input: map[string]any{"prompt": "inspect files", "max_turns": -1},
			want:  "max_turns must be positive",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.input)
			if _, err := tool.Execute(context.Background(), raw); err == nil || err.Error() != tc.want {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAgentToolPropagatesContextCancellation(t *testing.T) {
	tool := NewTool("/tmp/project", func(ctx context.Context, request Request) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	raw, _ := json.Marshal(map[string]any{"prompt": "inspect files"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tool.Execute(ctx, raw)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestAgentToolWaitsForRunnerCompletion(t *testing.T) {
	tool := NewTool("/tmp/project", func(_ context.Context, request Request) (string, error) {
		time.Sleep(10 * time.Millisecond)
		return request.Prompt, nil
	})

	raw, _ := json.Marshal(map[string]any{"prompt": "inspect files"})
	result, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("agent execute: %v", err)
	}
	if result.Output != "inspect files" {
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

	t.Run("remote isolation", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"prompt":    "Inspect src/auth",
			"isolation": "remote",
		})
		_, err := tool.Execute(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), "remote agent backend") {
			t.Fatalf("expected remote isolation error, got %v", err)
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
