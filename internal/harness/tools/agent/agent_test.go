package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreagent "claude-codex/internal/harness/agent"
	coretasks "claude-codex/internal/harness/tasks"
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

func TestAgentToolAppliesDefinitionExecutionDefaults(t *testing.T) {
	root := t.TempDir()
	t.Cleanup(coreagent.ClearAgentDefinitionsCache)
	agentsDir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	definition := `{
  "agent_type": "researcher",
  "prompt": "Research carefully.",
  "background": true,
  "isolation": "none",
  "max_turns": 7,
  "model": "haiku",
  "memory": "project",
  "skills": ["inspect"],
  "mcp_servers": ["filesystem"],
  "requiredMcpServers": ["filesystem"],
  "omit_claude_md": true
}`
	if err := os.WriteFile(filepath.Join(agentsDir, "researcher.json"), []byte(definition), 0o644); err != nil {
		t.Fatal(err)
	}
	coreagent.ClearAgentDefinitionsCache()

	manager := coretasks.NewTaskManager()
	requests := make(chan Request, 1)
	tool := NewToolWithTaskManager(root, func(_ context.Context, request Request) (string, error) {
		requests <- request
		return "ok", nil
	}, manager)
	tool.SetAvailableMCPServers([]string{"filesystem"})

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"prompt":"study","subagent_type":"researcher"}`))
	if err != nil {
		t.Fatalf("agent execute: %v", err)
	}
	var payload struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.TaskID == "" || payload.Status != string(coretasks.TaskStatusRunning) {
		t.Fatalf("expected background runtime task, got %+v", payload)
	}
	req := <-requests
	if !req.RunInBackground || req.MaxTurns != 7 || req.Model != "haiku" {
		t.Fatalf("definition defaults not applied: %+v", req)
	}
	if req.DefinitionMemory != "project" || !req.OmitClaudeMd || len(req.DefinitionSkills) != 1 || req.DefinitionSkills[0] != "inspect" {
		t.Fatalf("definition context not forwarded: %+v", req)
	}
	if len(req.DefinitionMCPServers) != 1 || req.DefinitionMCPServers[0] != "filesystem" {
		t.Fatalf("MCP servers not forwarded: %+v", req)
	}
	if len(req.DefinitionRequiredMCPServers) != 1 || req.DefinitionRequiredMCPServers[0] != "filesystem" {
		t.Fatalf("required MCP servers not forwarded: %+v", req)
	}
	waitForRuntimeTaskStatus(t, manager, payload.TaskID, coretasks.TaskStatusCompleted)
}

func TestAgentToolRejectsMissingRequiredMCPServers(t *testing.T) {
	root := t.TempDir()
	t.Cleanup(coreagent.ClearAgentDefinitionsCache)
	agentsDir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	definition := `{"agent_type":"notes-worker","required_mcp_servers":["notes"],"prompt":"Use notes."}`
	if err := os.WriteFile(filepath.Join(agentsDir, "notes-worker.json"), []byte(definition), 0o644); err != nil {
		t.Fatal(err)
	}
	coreagent.ClearAgentDefinitionsCache()

	tool := NewToolWithTaskManager(root, func(context.Context, Request) (string, error) {
		t.Fatal("runner should not be called when required MCP server is missing")
		return "", nil
	}, coretasks.NewTaskManager())

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"prompt":"study","subagent_type":"notes-worker"}`))
	if err == nil || !strings.Contains(err.Error(), "requires MCP servers") {
		t.Fatalf("expected missing MCP server error, got %v", err)
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
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := manager.Get(payload.AgentID)
		if ok && task.Status == BackgroundCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, ok := manager.Get(payload.AgentID)
	if !ok || task.Status != BackgroundCompleted || task.Output != "background ok" {
		t.Fatalf("unexpected background task: ok=%v task=%+v", ok, task)
	}
}

func TestAgentToolStartsRuntimeLocalAgentTaskByDefault(t *testing.T) {
	manager := coretasks.NewTaskManager()
	done := make(chan Request, 1)
	tool := NewToolWithTaskManager("/tmp/project", func(_ context.Context, request Request) (string, error) {
		done <- request
		return "runtime ok", nil
	}, manager)

	input, _ := json.Marshal(map[string]any{
		"prompt":            "inspect files",
		"subagent_type":     "explore",
		"run_in_background": true,
	})

	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("agent execute: %v", err)
	}
	var payload struct {
		TaskID         string `json:"task_id"`
		AgentID        string `json:"agent_id"`
		Status         string `json:"status"`
		AgentType      string `json:"agent_type"`
		InvocationKind string `json:"invocation_kind"`
	}
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("decode runtime payload: %v", err)
	}
	if payload.TaskID == "" || payload.AgentID == "" || payload.Status != string(coretasks.TaskStatusRunning) {
		t.Fatalf("unexpected runtime payload: %+v", payload)
	}
	req := <-done
	if req.SubagentType != "explore" || req.InvocationKind != string(coreagent.InvocationSubagent) {
		t.Fatalf("unexpected routed request: %+v", req)
	}
	waitForRuntimeTaskStatus(t, manager, payload.TaskID, coretasks.TaskStatusCompleted)
	task, _ := manager.GetTask(payload.TaskID)
	local := task.(*coretasks.LocalAgentTaskState)
	if local.Result == nil || local.Result.Output != "runtime ok" {
		t.Fatalf("unexpected local task result: %+v", local.Result)
	}
}

func TestAgentToolRoutesForkWhenEnabled(t *testing.T) {
	t.Setenv("CLAUDE_CODE_FORK_SUBAGENT", "1")
	manager := coretasks.NewTaskManager()
	requests := make(chan Request, 1)
	tool := NewToolWithTaskManager("/tmp/project", func(_ context.Context, request Request) (string, error) {
		requests <- request
		return "fork ok", nil
	}, manager)

	ctx := coreagent.WithAgentContext(context.Background(), coreagent.AgentContext{
		AgentID:         "parent-agent",
		ParentSessionID: "session-1",
		RecentMessages:  []string{"user: fix auth", "assistant: I will inspect auth"},
		SessionMetadata: map[string]string{"mode": "coordinator"},
	})
	result, err := tool.Execute(ctx, json.RawMessage(`{"prompt":"investigate"}`))
	if err != nil {
		t.Fatalf("agent execute: %v", err)
	}
	var payload struct {
		TaskID         string `json:"task_id"`
		AgentType      string `json:"agent_type"`
		InvocationKind string `json:"invocation_kind"`
	}
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("decode fork payload: %v", err)
	}
	if payload.TaskID == "" || payload.AgentType != coreagent.FORK_SUBAGENT_TYPE || payload.InvocationKind != string(coreagent.InvocationFork) {
		t.Fatalf("unexpected fork payload: %+v", payload)
	}
	req := <-requests
	if !req.RunInBackground || req.SubagentType != coreagent.FORK_SUBAGENT_TYPE || req.InvocationKind != string(coreagent.InvocationFork) {
		t.Fatalf("unexpected fork request: %+v", req)
	}
	if req.ParentAgentID != "parent-agent" || req.ParentSessionID != "session-1" || req.ParentMetadata["mode"] != "coordinator" {
		t.Fatalf("fork did not inherit parent context: %+v", req)
	}
	if !strings.Contains(req.Prompt, "<fork-parent-context>") || !strings.Contains(req.Prompt, "fix auth") {
		t.Fatalf("fork prompt missing inherited context: %q", req.Prompt)
	}
}

func TestAgentToolRejectsRecursiveFork(t *testing.T) {
	t.Setenv("CLAUDE_CODE_FORK_SUBAGENT", "1")
	tool := NewToolWithTaskManager("/tmp/project", func(_ context.Context, request Request) (string, error) {
		return "should not run", nil
	}, coretasks.NewTaskManager())
	ctx := coreagent.WithAgentContext(context.Background(), coreagent.AgentContext{
		InvocationKind: coreagent.InvocationFork,
	})

	_, err := tool.Execute(ctx, json.RawMessage(`{"prompt":"fork again"}`))
	if err == nil || !strings.Contains(err.Error(), "fork is not available") {
		t.Fatalf("expected recursive fork error, got %v", err)
	}
}

func TestAgentToolRoutesNamedTeamAgentAsBackground(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := coretasks.NewTaskManager()
	requests := make(chan Request, 1)
	tool := NewToolWithTaskManager("/tmp/project", func(_ context.Context, request Request) (string, error) {
		requests <- request
		return "team ok", nil
	}, manager)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"prompt":"work","team_name":"alpha","name":"reviewer"}`))
	if err != nil {
		t.Fatalf("agent execute: %v", err)
	}
	var payload struct {
		TaskID         string `json:"task_id"`
		AgentType      string `json:"agent_type"`
		InvocationKind string `json:"invocation_kind"`
	}
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("decode team payload: %v", err)
	}
	if payload.TaskID == "" || payload.AgentType != "reviewer" || payload.InvocationKind != string(coreagent.InvocationTeammate) {
		t.Fatalf("unexpected team payload: %+v", payload)
	}
	req := <-requests
	if !req.RunInBackground || req.SubagentType != "reviewer" || req.InvocationKind != string(coreagent.InvocationTeammate) {
		t.Fatalf("unexpected team request: %+v", req)
	}
}

func TestAgentToolForwardsWorktreeIsolation(t *testing.T) {
	var captured Request
	root := initAgentToolGitRepo(t)
	tool := NewTool(root, func(_ context.Context, request Request) (string, error) {
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
	if captured.WorktreePath == "" || captured.WorkingDir != captured.WorktreePath || captured.WorktreePath == root {
		t.Fatalf("expected worktree working dir, got %+v", captured)
	}
	if !strings.Contains(captured.Prompt, "isolated git worktree") {
		t.Fatalf("expected worktree notice in prompt, got %q", captured.Prompt)
	}
}

func waitForRuntimeTaskStatus(t *testing.T, manager *coretasks.TaskManager, taskID string, status coretasks.TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := manager.GetTask(taskID)
		if ok && task.GetStatus() == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := manager.GetTask(taskID)
	t.Fatalf("task %s did not reach %s, got %#v", taskID, status, task)
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
	if got.Mode != "plan" {
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

func initAgentToolGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runAgentToolGit(t, root, "init")
	runAgentToolGit(t, root, "config", "user.email", "test@example.com")
	runAgentToolGit(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAgentToolGit(t, root, "add", "README.md")
	runAgentToolGit(t, root, "commit", "-m", "init")
	return root
}

func runAgentToolGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
