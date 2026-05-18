package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coretasks "claude-codex/internal/harness/tasks"
	toolkit "claude-codex/internal/harness/tools"
)

func withTaskToolHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CODE_TASK_LIST_ID", "team/alpha")
	return home
}

func execTool(t *testing.T, tool toolkit.Tool, payload string) string {
	t.Helper()
	out, err := tool.Execute(context.Background(), json.RawMessage(payload))
	if err != nil {
		t.Fatalf("Execute(%s) error = %v", payload, err)
	}
	return out.Output
}

func TestTaskToolsPersistSequentialIDsAndHighWater(t *testing.T) {
	withTaskToolHome(t)
	create := NewTaskCreateTool()
	update := NewTaskUpdateTool()

	first := execTool(t, create, `{"subject":"First","description":"Do first"}`)
	second := execTool(t, create, `{"subject":"Second","description":"Do second"}`)
	if !strings.Contains(first, `"id":"1"`) || !strings.Contains(second, `"id":"2"`) {
		t.Fatalf("expected sequential task ids, got first=%s second=%s", first, second)
	}

	deleted := execTool(t, update, `{"taskId":"2","status":"deleted"}`)
	if !strings.Contains(deleted, `"updatedFields":["deleted"]`) {
		t.Fatalf("expected deleted update result, got %s", deleted)
	}

	third := execTool(t, create, `{"subject":"Third","description":"Do third"}`)
	if !strings.Contains(third, `"id":"3"`) {
		t.Fatalf("expected high-water mark to prevent id reuse, got %s", third)
	}
}

func TestTaskUpdateMaintainsBidirectionalDependenciesAndListFiltersCompletedBlockers(t *testing.T) {
	withTaskToolHome(t)
	create := NewTaskCreateTool()
	update := NewTaskUpdateTool()
	get := NewTaskGetTool()
	list := NewTaskListTool()

	execTool(t, create, `{"subject":"Build foundation","description":"Create base"}`)
	execTool(t, create, `{"subject":"Use foundation","description":"Build on base"}`)

	updated := execTool(t, update, `{"taskId":"1","addBlocks":["2"]}`)
	if !strings.Contains(updated, `"updatedFields":["blocks"]`) {
		t.Fatalf("expected blocks field update, got %s", updated)
	}

	got := execTool(t, get, `{"taskId":"2"}`)
	if !strings.Contains(got, `"blockedBy":["1"]`) {
		t.Fatalf("expected reverse blockedBy relationship, got %s", got)
	}

	execTool(t, update, `{"taskId":"1","status":"completed"}`)
	listed := execTool(t, list, `{}`)
	if strings.Contains(listed, `"blockedBy":["1"]`) {
		t.Fatalf("expected completed blockers to be filtered from TaskList, got %s", listed)
	}
}

func TestTaskUpdateMissingTaskReturnsStructuredFailure(t *testing.T) {
	withTaskToolHome(t)
	update := NewTaskUpdateTool()

	out := execTool(t, update, `{"taskId":"missing","status":"completed"}`)
	if !strings.Contains(out, `"success":false`) || !strings.Contains(out, `"error":"Task not found"`) {
		t.Fatalf("expected structured missing-task failure, got %s", out)
	}
}

func TestTaskUpdateMergesAndDeletesMetadata(t *testing.T) {
	home := withTaskToolHome(t)
	create := NewTaskCreateTool()
	update := NewTaskUpdateTool()

	execTool(t, create, `{"subject":"Meta","description":"Track metadata","metadata":{"keep":"yes","remove":"soon"}}`)
	out := execTool(t, update, `{"taskId":"1","metadata":{"remove":null,"added":true}}`)
	if !strings.Contains(out, `"updatedFields":["metadata"]`) {
		t.Fatalf("expected metadata update, got %s", out)
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude", "tasks", "team-alpha", "1.json"))
	if err != nil {
		t.Fatalf("read task file: %v", err)
	}
	if strings.Contains(string(data), `"remove"`) || !strings.Contains(string(data), `"added": true`) {
		t.Fatalf("expected metadata merge/delete in task file, got %s", data)
	}
}

func TestTaskToolsSanitizeTaskListID(t *testing.T) {
	home := withTaskToolHome(t)
	create := NewTaskCreateTool()
	execTool(t, create, `{"subject":"Safe path","description":"Persist safely"}`)

	if _, err := os.Stat(filepath.Join(home, ".claude", "tasks", "team-alpha", "1.json")); err != nil {
		t.Fatalf("expected sanitized task path to exist: %v", err)
	}
}

func TestTaskOutputReadsRuntimeLocalAgentTask(t *testing.T) {
	task, err := coretasks.DefaultManager().StartLocalAgent(context.Background(), coretasks.StartLocalAgentOptions{
		Prompt:         "inspect runtime",
		Description:    "runtime agent",
		OutputFile:     filepath.Join(t.TempDir(), "agent.output"),
		IsBackgrounded: true,
		Runner: func(_ context.Context, req coretasks.LocalAgentRunRequest) (string, error) {
			return "runtime done", nil
		},
	})
	if err != nil {
		t.Fatalf("StartLocalAgent() error = %v", err)
	}

	output := execTool(t, NewTaskOutputTool(), `{"task_id":"`+task.ID+`","block":true,"timeout":2000}`)
	if !strings.Contains(output, `"retrieval_status":"success"`) || !strings.Contains(output, `"output":"runtime done"`) {
		t.Fatalf("expected runtime local agent output, got %s", output)
	}
}

func TestTaskGetAndListIncludeRuntimeLocalAgentTask(t *testing.T) {
	task, err := coretasks.DefaultManager().StartLocalAgent(context.Background(), coretasks.StartLocalAgentOptions{
		Prompt:      "list runtime",
		Description: "listed runtime agent",
		AgentType:   "explore",
		OutputFile:  filepath.Join(t.TempDir(), "agent.output"),
		Runner: func(_ context.Context, req coretasks.LocalAgentRunRequest) (string, error) {
			return "listed", nil
		},
	})
	if err != nil {
		t.Fatalf("StartLocalAgent() error = %v", err)
	}

	got := execTool(t, NewTaskGetTool(), `{"taskId":"`+task.ID+`"}`)
	if !strings.Contains(got, `"task_id":"`+task.ID+`"`) || !strings.Contains(got, `"agent_type":"explore"`) {
		t.Fatalf("expected runtime task from TaskGet, got %s", got)
	}

	listed := execTool(t, NewTaskListTool(), `{}`)
	if !strings.Contains(listed, `"task_id":"`+task.ID+`"`) {
		t.Fatalf("expected runtime task from TaskList, got %s", listed)
	}
}

func TestTaskStopStopsRuntimeLocalAgentTask(t *testing.T) {
	task, err := coretasks.DefaultManager().StartLocalAgent(context.Background(), coretasks.StartLocalAgentOptions{
		Prompt:     "wait",
		OutputFile: filepath.Join(t.TempDir(), "agent.output"),
		Runner: func(ctx context.Context, req coretasks.LocalAgentRunRequest) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("StartLocalAgent() error = %v", err)
	}

	stopped := execTool(t, NewTaskStopTool(), `{"task_id":"`+task.ID+`"}`)
	if !strings.Contains(stopped, `"task_type":"local_agent"`) {
		t.Fatalf("expected runtime task stop result, got %s", stopped)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := coretasks.DefaultManager().GetTask(task.ID)
		if ok && current.GetStatus() == coretasks.TaskStatusKilled {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, _ := coretasks.DefaultManager().GetTask(task.ID)
	t.Fatalf("task did not stop: %#v", current)
}
