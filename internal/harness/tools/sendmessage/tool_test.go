package sendmessage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	coretasks "claude-codex/internal/harness/tasks"
)

func TestSendMessageQueuesRuntimeLocalAgentMessage(t *testing.T) {
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
	t.Cleanup(func() {
		_ = coretasks.DefaultManager().KillTask(task.ID, func(updater func(prev interface{}) interface{}) {})
	})

	raw, _ := json.Marshal(map[string]any{
		"to":      task.ID,
		"message": "continue please",
	})
	result, err := (&Tool{}).Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("SendMessage Execute() error = %v", err)
	}
	if result.Output == "" {
		t.Fatal("expected output")
	}

	messages, err := coretasks.DefaultManager().DrainLocalAgentMessages(task.ID)
	if err != nil {
		t.Fatalf("DrainLocalAgentMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0] != "continue please" {
		t.Fatalf("unexpected queued messages: %#v", messages)
	}
}

func TestSendMessageQueuesInProcessTeammateMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	task, err := coretasks.DefaultManager().StartInProcessTeammate(context.Background(), coretasks.StartInProcessTeammateOptions{
		Prompt:     "wait",
		Name:       "reviewer",
		TeamName:   "alpha",
		OutputFile: filepath.Join(t.TempDir(), "teammate.output"),
		Runner: func(ctx context.Context, req coretasks.InProcessTeammateRunRequest) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("StartInProcessTeammate() error = %v", err)
	}
	t.Cleanup(func() {
		_ = coretasks.DefaultManager().KillTask(task.ID, func(updater func(prev interface{}) interface{}) {})
	})

	raw, _ := json.Marshal(map[string]any{
		"to":      "reviewer@alpha",
		"message": "continue teammate",
	})
	result, err := (&Tool{}).Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("SendMessage Execute() error = %v", err)
	}
	if result.Output == "" {
		t.Fatal("expected output")
	}

	messages, err := coretasks.DefaultManager().DrainInProcessTeammateMessages(task.ID)
	if err != nil {
		t.Fatalf("DrainInProcessTeammateMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0] != "continue teammate" {
		t.Fatalf("unexpected queued messages: %#v", messages)
	}
}
