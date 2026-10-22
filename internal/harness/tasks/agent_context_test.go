package tasks

import (
	"context"
	"testing"
	"time"
)

func TestAttachedAgentTaskCancelsWithParent(t *testing.T) {
	manager := NewTaskManager()
	terminal, unsubscribe := manager.SubscribeTerminalEvents(1)
	defer unsubscribe()
	parent, cancel := context.WithCancel(WithAttachedAgentTasks(context.Background()))
	started := make(chan struct{})
	task, err := manager.StartLocalAgent(parent, StartLocalAgentOptions{
		Prompt:     "inspect independent source",
		OutputFile: t.TempDir() + "/output.txt",
		Runner: func(ctx context.Context, _ LocalAgentRunRequest) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case id := <-terminal:
		if id != task.ID {
			t.Fatalf("terminal task = %s, want %s", id, task.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker was not cancelled")
	}
	state, ok := manager.GetTask(task.ID)
	if !ok || state.GetStatus() != TaskStatusKilled {
		t.Fatalf("state = %#v", state)
	}
}
