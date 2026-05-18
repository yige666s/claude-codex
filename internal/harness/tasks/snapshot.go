package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Snapshot struct {
	Version int            `json:"version"`
	Tasks   []SnapshotTask `json:"tasks"`
}

type SnapshotTask struct {
	Type            TaskType         `json:"type"`
	Base            TaskStateBase    `json:"base"`
	AgentID         string           `json:"agentId,omitempty"`
	TeammateID      string           `json:"teammateId,omitempty"`
	ParentAgentID   string           `json:"parentAgentId,omitempty"`
	ParentSessionID string           `json:"parentSessionId,omitempty"`
	Name            string           `json:"name,omitempty"`
	TeamName        string           `json:"teamName,omitempty"`
	AgentType       string           `json:"agentType,omitempty"`
	Model           string           `json:"model,omitempty"`
	Prompt          string           `json:"prompt,omitempty"`
	WorkingDir      string           `json:"workingDir,omitempty"`
	WorktreePath    string           `json:"worktreePath,omitempty"`
	WorktreeBranch  string           `json:"worktreeBranch,omitempty"`
	Result          *AgentTaskResult `json:"result,omitempty"`
	IsBackgrounded  bool             `json:"isBackgrounded"`
	PendingMessages []interface{}    `json:"pendingMessages,omitempty"`
}

func (m *TaskManager) ExportSnapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot := Snapshot{Version: 1, Tasks: make([]SnapshotTask, 0, len(m.tasks))}
	for _, task := range m.tasks {
		if item, ok := snapshotTaskFromState(task); ok {
			snapshot.Tasks = append(snapshot.Tasks, item)
		}
	}
	return snapshot
}

func (m *TaskManager) RestoreSnapshot(snapshot Snapshot) error {
	for _, item := range snapshot.Tasks {
		state, err := stateFromSnapshotTask(item)
		if err != nil {
			return err
		}
		if state != nil {
			m.AddTask(state)
		}
	}
	return nil
}

func (m *TaskManager) SaveSnapshot(path string) error {
	if path == "" {
		return fmt.Errorf("snapshot path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.ExportSnapshot(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (m *TaskManager) LoadSnapshot(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	return m.RestoreSnapshot(snapshot)
}

func snapshotTaskFromState(task TaskState) (SnapshotTask, bool) {
	switch typed := task.(type) {
	case *LocalAgentTaskState:
		return SnapshotTask{
			Type:            typed.Type,
			Base:            typed.TaskStateBase,
			AgentID:         typed.AgentID,
			ParentAgentID:   typed.ParentAgentID,
			ParentSessionID: typed.ParentSessionID,
			AgentType:       typed.AgentType,
			Model:           typed.Model,
			Prompt:          typed.Prompt,
			WorkingDir:      typed.WorkingDir,
			WorktreePath:    typed.WorktreePath,
			WorktreeBranch:  typed.WorktreeBranch,
			Result:          typed.Result,
			IsBackgrounded:  typed.IsBackgrounded,
			PendingMessages: append([]interface{}(nil), typed.PendingMessages...),
		}, true
	case *InProcessTeammateTaskState:
		return SnapshotTask{
			Type:            typed.Type,
			Base:            typed.TaskStateBase,
			TeammateID:      typed.TeammateID,
			ParentAgentID:   typed.ParentAgentID,
			ParentSessionID: typed.ParentSessionID,
			Name:            typed.Name,
			TeamName:        typed.TeamName,
			AgentType:       typed.AgentType,
			Model:           typed.Model,
			Prompt:          typed.Prompt,
			WorkingDir:      typed.WorkingDir,
			WorktreePath:    typed.WorktreePath,
			WorktreeBranch:  typed.WorktreeBranch,
			Result:          typed.Result,
			IsBackgrounded:  typed.IsBackgrounded,
			PendingMessages: append([]interface{}(nil), typed.PendingMessages...),
		}, true
	default:
		return SnapshotTask{}, false
	}
}

func stateFromSnapshotTask(item SnapshotTask) (TaskState, error) {
	base := recoveredBase(item.Base)
	switch item.Type {
	case TaskTypeLocalAgent:
		result := recoveredResult(base, item.Result)
		return &LocalAgentTaskState{
			TaskStateBase:   base,
			AgentID:         item.AgentID,
			ParentAgentID:   item.ParentAgentID,
			ParentSessionID: item.ParentSessionID,
			Prompt:          item.Prompt,
			AgentType:       item.AgentType,
			Model:           item.Model,
			WorkingDir:      item.WorkingDir,
			WorktreePath:    item.WorktreePath,
			WorktreeBranch:  item.WorktreeBranch,
			Result:          result,
			IsBackgrounded:  item.IsBackgrounded,
			PendingMessages: append([]interface{}(nil), item.PendingMessages...),
			Progress:        &AgentProgress{RecentActivities: []ToolActivity{}},
			Messages:        []interface{}{},
		}, nil
	case TaskTypeInProcessTeammate:
		result := recoveredResult(base, item.Result)
		return &InProcessTeammateTaskState{
			TaskStateBase:   base,
			TeammateID:      item.TeammateID,
			ParentAgentID:   item.ParentAgentID,
			ParentSessionID: item.ParentSessionID,
			Name:            item.Name,
			TeamName:        item.TeamName,
			AgentType:       item.AgentType,
			Model:           item.Model,
			WorkingDir:      item.WorkingDir,
			WorktreePath:    item.WorktreePath,
			WorktreeBranch:  item.WorktreeBranch,
			Prompt:          item.Prompt,
			Result:          result,
			PendingMessages: append([]interface{}(nil), item.PendingMessages...),
			Progress:        &AgentProgress{RecentActivities: []ToolActivity{}},
			Messages:        []interface{}{},
			IsBackgrounded:  item.IsBackgrounded,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported snapshot task type: %s", item.Type)
	}
}

func recoveredBase(base TaskStateBase) TaskStateBase {
	if base.Status == TaskStatusRunning || base.Status == TaskStatusPending {
		now := time.Now().UnixMilli()
		base.Status = TaskStatusKilled
		base.EndTime = &now
	}
	return base
}

func recoveredResult(base TaskStateBase, result *AgentTaskResult) *AgentTaskResult {
	if result != nil {
		return result
	}
	if base.Status == TaskStatusKilled {
		return &AgentTaskResult{Error: "task was not running after process restore", Interrupted: true}
	}
	return nil
}
