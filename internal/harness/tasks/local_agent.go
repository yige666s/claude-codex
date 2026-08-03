package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalAgentRunRequest is passed to a background local agent runner.
type LocalAgentRunRequest struct {
	TaskID          string
	AgentID         string
	ParentAgentID   string
	ParentSessionID string
	Prompt          string
	AgentType       string
	Model           string
	WorkingDir      string
}

// LocalAgentRunner runs a local agent and returns its final text output.
type LocalAgentRunner func(context.Context, LocalAgentRunRequest) (string, error)

type StartLocalAgentOptions struct {
	Prompt          string
	Description     string
	AgentID         string
	ParentAgentID   string
	ParentSessionID string
	AgentType       string
	Model           string
	WorkingDir      string
	WorktreePath    string
	WorktreeBranch  string
	SelectedAgent   interface{}
	ToolUseID       string
	OutputFile      string
	IsBackgrounded  bool
	Retain          bool
	Runner          LocalAgentRunner
}

// LocalAgentTask implements the local_agent task lifecycle.
type LocalAgentTask struct {
	manager *TaskManager
}

func (t *LocalAgentTask) GetName() string   { return "LocalAgentTask" }
func (t *LocalAgentTask) GetType() TaskType { return TaskTypeLocalAgent }

func (t *LocalAgentTask) Kill(taskID string, _ func(updater func(prev interface{}) interface{})) error {
	if t == nil || t.manager == nil {
		return fmt.Errorf("local agent task manager is not configured")
	}
	task, ok := t.manager.GetTask(taskID)
	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}
	agentTask, ok := task.(*LocalAgentTaskState)
	if !ok {
		return fmt.Errorf("task %s is not a local agent task", taskID)
	}
	if agentTask.AbortController != nil {
		agentTask.AbortController()
	}
	err := t.manager.UpdateTask(taskID, func(task TaskState) (TaskState, error) {
		agentTask, ok := task.(*LocalAgentTaskState)
		if !ok {
			return nil, fmt.Errorf("task %s is not a local agent task", taskID)
		}
		now := time.Now().UnixMilli()
		agentTask.Status = TaskStatusKilled
		agentTask.EndTime = &now
		agentTask.Result = &AgentTaskResult{Error: context.Canceled.Error(), Interrupted: true}
		return agentTask, nil
	})
	if err == nil {
		t.manager.emitTerminalEvent(taskID)
	}
	return err
}

// StartLocalAgent starts a background local agent task.
func (m *TaskManager) StartLocalAgent(parent context.Context, opts StartLocalAgentOptions) (*LocalAgentTaskState, error) {
	if opts.Runner == nil {
		return nil, fmt.Errorf("local agent runner is required")
	}
	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	taskID, err := GenerateTaskID(TaskTypeLocalAgent)
	if err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(opts.AgentID)
	if agentID == "" {
		agentID = taskID
	}
	description := strings.TrimSpace(opts.Description)
	if description == "" {
		description = prompt
	}
	outputFile := strings.TrimSpace(opts.OutputFile)
	if outputFile == "" {
		outputFile = defaultLocalAgentOutputPath(taskID)
	}
	if err := os.MkdirAll(filepath.Dir(outputFile), 0o755); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	base := CreateTaskStateBase(taskID, TaskTypeLocalAgent, description, opts.ToolUseID, outputFile)
	base.Status = TaskStatusRunning
	state := &LocalAgentTaskState{
		TaskStateBase:   base,
		AgentID:         agentID,
		ParentAgentID:   opts.ParentAgentID,
		ParentSessionID: opts.ParentSessionID,
		Prompt:          prompt,
		SelectedAgent:   opts.SelectedAgent,
		AgentType:       opts.AgentType,
		Model:           opts.Model,
		WorkingDir:      opts.WorkingDir,
		WorktreePath:    opts.WorktreePath,
		WorktreeBranch:  opts.WorktreeBranch,
		AbortController: cancel,
		IsBackgrounded:  opts.IsBackgrounded,
		Retain:          opts.Retain,
		PendingMessages: []interface{}{},
		Progress: &AgentProgress{
			RecentActivities: []ToolActivity{},
		},
		Messages: []interface{}{},
	}
	m.AddTask(state)
	snapshot := cloneTaskState(state).(*LocalAgentTaskState)

	go m.runLocalAgent(ctx, taskID, LocalAgentRunRequest{
		TaskID:          taskID,
		AgentID:         agentID,
		ParentAgentID:   opts.ParentAgentID,
		ParentSessionID: opts.ParentSessionID,
		Prompt:          prompt,
		AgentType:       opts.AgentType,
		Model:           opts.Model,
		WorkingDir:      opts.WorkingDir,
	}, opts.Runner, outputFile)

	return snapshot, nil
}

func (m *TaskManager) runLocalAgent(ctx context.Context, taskID string, request LocalAgentRunRequest, runner LocalAgentRunner, outputFile string) {
	output, err := runner(ctx, request)
	if strings.TrimSpace(output) != "" {
		_ = appendTaskOutput(outputFile, output)
	}
	now := time.Now().UnixMilli()
	if err := m.UpdateTask(taskID, func(task TaskState) (TaskState, error) {
		agentTask, ok := task.(*LocalAgentTaskState)
		if !ok {
			return nil, fmt.Errorf("task %s is not a local agent task", taskID)
		}
		agentTask.EndTime = &now
		agentTask.AbortController = nil
		if err == nil {
			agentTask.Status = TaskStatusCompleted
			agentTask.Result = &AgentTaskResult{Output: output}
			return agentTask, nil
		}
		if ctx.Err() != nil {
			agentTask.Status = TaskStatusKilled
			agentTask.Result = &AgentTaskResult{Error: ctx.Err().Error(), Interrupted: true}
			return agentTask, nil
		}
		agentTask.Status = TaskStatusFailed
		agentTask.Result = &AgentTaskResult{Error: err.Error()}
		_ = appendTaskOutput(outputFile, "\nError: "+err.Error())
		return agentTask, nil
	}); err == nil {
		m.emitTerminalEvent(taskID)
	}
}

func defaultLocalAgentOutputPath(taskID string) string {
	return filepath.Join(os.TempDir(), "claude-codex", "tasks", taskID+".output")
}

func appendTaskOutput(path string, chunk string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(chunk)
	return err
}

func (m *TaskManager) QueueLocalAgentMessage(taskID string, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("message is required")
	}
	return m.UpdateTask(taskID, func(task TaskState) (TaskState, error) {
		agentTask, ok := task.(*LocalAgentTaskState)
		if !ok {
			return nil, fmt.Errorf("task %s is not a local agent task", taskID)
		}
		if IsTerminalTaskStatus(agentTask.Status) {
			return nil, fmt.Errorf("task %s is already %s", taskID, agentTask.Status)
		}
		agentTask.PendingMessages = append(agentTask.PendingMessages, message)
		return agentTask, nil
	})
}

func (m *TaskManager) DrainLocalAgentMessages(taskID string) ([]interface{}, error) {
	var messages []interface{}
	err := m.UpdateTask(taskID, func(task TaskState) (TaskState, error) {
		agentTask, ok := task.(*LocalAgentTaskState)
		if !ok {
			return nil, fmt.Errorf("task %s is not a local agent task", taskID)
		}
		messages = append(messages, agentTask.PendingMessages...)
		agentTask.PendingMessages = []interface{}{}
		return agentTask, nil
	})
	return messages, err
}
