package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-codex/internal/harness/swarm"
)

type InProcessTeammateRunRequest struct {
	TaskID          string
	TeammateID      string
	ParentAgentID   string
	ParentSessionID string
	Name            string
	TeamName        string
	Prompt          string
	AgentType       string
	Model           string
	WorkingDir      string
}

type InProcessTeammateRunner func(context.Context, InProcessTeammateRunRequest) (string, error)

type StartInProcessTeammateOptions struct {
	Prompt          string
	Description     string
	Name            string
	TeamName        string
	ParentAgentID   string
	ParentSessionID string
	AgentType       string
	Model           string
	WorkingDir      string
	WorktreePath    string
	WorktreeBranch  string
	ToolUseID       string
	OutputFile      string
	IsBackgrounded  bool
	Runner          InProcessTeammateRunner
}

type InProcessTeammateTask struct {
	manager *TaskManager
}

func (t *InProcessTeammateTask) GetName() string   { return "InProcessTeammateTask" }
func (t *InProcessTeammateTask) GetType() TaskType { return TaskTypeInProcessTeammate }

func (t *InProcessTeammateTask) Kill(taskID string, _ func(updater func(prev interface{}) interface{})) error {
	if t == nil || t.manager == nil {
		return fmt.Errorf("in-process teammate task manager is not configured")
	}
	task, ok := t.manager.GetTask(taskID)
	if !ok {
		if teammate, found := t.manager.FindInProcessTeammate(taskID); found {
			task = teammate
			taskID = teammate.ID
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}
	teammate, ok := task.(*InProcessTeammateTaskState)
	if !ok {
		return fmt.Errorf("task %s is not an in-process teammate task", taskID)
	}
	if teammate.AbortController != nil {
		teammate.AbortController()
	}
	err := t.manager.UpdateTask(taskID, func(task TaskState) (TaskState, error) {
		teammate, ok := task.(*InProcessTeammateTaskState)
		if !ok {
			return nil, fmt.Errorf("task %s is not an in-process teammate task", taskID)
		}
		now := time.Now().UnixMilli()
		teammate.Status = TaskStatusKilled
		teammate.EndTime = &now
		teammate.Result = &AgentTaskResult{Error: context.Canceled.Error(), Interrupted: true}
		if teammate.TeamName != "" && teammate.Name != "" {
			_ = swarm.SetMemberActive(teammate.TeamName, teammate.Name, false)
		}
		return teammate, nil
	})
	if err == nil {
		t.manager.emitTerminalEvent(taskID)
	}
	return err
}

func (m *TaskManager) StartInProcessTeammate(parent context.Context, opts StartInProcessTeammateOptions) (*InProcessTeammateTaskState, error) {
	if opts.Runner == nil {
		return nil, fmt.Errorf("in-process teammate runner is required")
	}
	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	name := strings.TrimSpace(opts.Name)
	teamName := strings.TrimSpace(opts.TeamName)
	if name == "" || teamName == "" {
		return nil, fmt.Errorf("name and team_name are required")
	}
	taskID, err := GenerateTaskID(TaskTypeInProcessTeammate)
	if err != nil {
		return nil, err
	}
	teammateID := string(swarm.FormatAgentID(name, teamName))
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
	base := CreateTaskStateBase(taskID, TaskTypeInProcessTeammate, description, opts.ToolUseID, outputFile)
	base.Status = TaskStatusRunning
	state := &InProcessTeammateTaskState{
		TaskStateBase:   base,
		TeammateID:      teammateID,
		ParentAgentID:   opts.ParentAgentID,
		ParentSessionID: opts.ParentSessionID,
		Name:            name,
		TeamName:        teamName,
		AgentType:       opts.AgentType,
		Model:           opts.Model,
		WorkingDir:      opts.WorkingDir,
		WorktreePath:    opts.WorktreePath,
		WorktreeBranch:  opts.WorktreeBranch,
		Prompt:          prompt,
		AbortController: cancel,
		PendingMessages: []interface{}{},
		Messages:        []interface{}{},
		Progress:        &AgentProgress{RecentActivities: []ToolActivity{}},
		IsBackgrounded:  opts.IsBackgrounded,
	}
	m.AddTask(state)
	upsertTeammateMember(teamName, state, opts.WorkingDir)

	go m.runInProcessTeammate(ctx, taskID, InProcessTeammateRunRequest{
		TaskID:          taskID,
		TeammateID:      teammateID,
		ParentAgentID:   opts.ParentAgentID,
		ParentSessionID: opts.ParentSessionID,
		Name:            name,
		TeamName:        teamName,
		Prompt:          prompt,
		AgentType:       opts.AgentType,
		Model:           opts.Model,
		WorkingDir:      opts.WorkingDir,
	}, opts.Runner, outputFile)

	return state, nil
}

func (m *TaskManager) runInProcessTeammate(ctx context.Context, taskID string, request InProcessTeammateRunRequest, runner InProcessTeammateRunner, outputFile string) {
	output, err := runner(ctx, request)
	if strings.TrimSpace(output) != "" {
		_ = appendTaskOutput(outputFile, output)
	}
	now := time.Now().UnixMilli()
	if err := m.UpdateTask(taskID, func(task TaskState) (TaskState, error) {
		teammate, ok := task.(*InProcessTeammateTaskState)
		if !ok {
			return nil, fmt.Errorf("task %s is not an in-process teammate task", taskID)
		}
		teammate.EndTime = &now
		teammate.AbortController = nil
		defer func() {
			if teammate.TeamName != "" && teammate.Name != "" {
				_ = swarm.SetMemberActive(teammate.TeamName, teammate.Name, false)
			}
		}()
		if err == nil {
			teammate.Status = TaskStatusCompleted
			teammate.Result = &AgentTaskResult{Output: output}
			return teammate, nil
		}
		if ctx.Err() != nil {
			teammate.Status = TaskStatusKilled
			teammate.Result = &AgentTaskResult{Error: ctx.Err().Error(), Interrupted: true}
			return teammate, nil
		}
		teammate.Status = TaskStatusFailed
		teammate.Result = &AgentTaskResult{Error: err.Error()}
		_ = appendTaskOutput(outputFile, "\nError: "+err.Error())
		return teammate, nil
	}); err == nil {
		m.emitTerminalEvent(taskID)
	}
}

func upsertTeammateMember(teamName string, state *InProcessTeammateTaskState, cwd string) {
	if strings.TrimSpace(teamName) == "" || state == nil {
		return
	}
	tf, err := swarm.ReadTeamFile(teamName)
	if err != nil {
		return
	}
	if tf == nil {
		if _, err := swarm.CreateTeamFile(teamName, "", swarm.TeamLeadName, ""); err != nil {
			return
		}
	}
	_ = swarm.UpsertMember(teamName, swarm.TeamMember{
		AgentID:       state.TeammateID,
		Name:          state.Name,
		AgentType:     state.AgentType,
		Model:         state.Model,
		Prompt:        state.Prompt,
		JoinedAt:      time.Now().UnixMilli(),
		TmuxPaneID:    swarm.InProcessMarker,
		CWD:           cwd,
		Subscriptions: []string{},
		BackendType:   string(swarm.BackendTypeInProcess),
		IsActive:      true,
	})
}

func (m *TaskManager) QueueInProcessTeammateMessage(nameOrID string, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("message is required")
	}
	teammate, ok := m.FindInProcessTeammate(nameOrID)
	if !ok {
		return fmt.Errorf("teammate not found: %s", nameOrID)
	}
	return m.UpdateTask(teammate.ID, func(task TaskState) (TaskState, error) {
		teammate, ok := task.(*InProcessTeammateTaskState)
		if !ok {
			return nil, fmt.Errorf("task %s is not an in-process teammate task", nameOrID)
		}
		if IsTerminalTaskStatus(teammate.Status) {
			return nil, fmt.Errorf("teammate %s is already %s", nameOrID, teammate.Status)
		}
		teammate.PendingMessages = append(teammate.PendingMessages, message)
		return teammate, nil
	})
}

func (m *TaskManager) DrainInProcessTeammateMessages(nameOrID string) ([]interface{}, error) {
	teammate, ok := m.FindInProcessTeammate(nameOrID)
	if !ok {
		return nil, fmt.Errorf("teammate not found: %s", nameOrID)
	}
	var messages []interface{}
	err := m.UpdateTask(teammate.ID, func(task TaskState) (TaskState, error) {
		teammate, ok := task.(*InProcessTeammateTaskState)
		if !ok {
			return nil, fmt.Errorf("task %s is not an in-process teammate task", nameOrID)
		}
		messages = append(messages, teammate.PendingMessages...)
		teammate.PendingMessages = []interface{}{}
		return teammate, nil
	})
	return messages, err
}
