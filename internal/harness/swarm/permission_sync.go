package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultPermissionPollInterval = 250 * time.Millisecond

// PermissionRequestParams are the inputs for constructing a pending swarm
// permission request from worker/runtime metadata.
type PermissionRequestParams struct {
	WorkerID              string
	WorkerName            string
	WorkerColor           string
	TeamName              string
	ToolName              string
	ToolUseID             string
	Description           string
	Input                 map[string]any
	PermissionSuggestions []any
}

// PermissionRequestOptions configure RequestPermission fallback polling.
type PermissionRequestOptions struct {
	PollInterval time.Duration
}

// MailboxEntry is a single message in an agent's mailbox file.
type MailboxEntry struct {
	From      string    `json:"from"`
	Text      string    `json:"text"`
	Color     string    `json:"color,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// GetMailboxDir returns the mailbox base directory for a team.
func GetMailboxDir(baseDir, teamName string) string {
	if teamName != "" {
		return filepath.Join(GetTeamDir(teamName), "mailbox")
	}
	return filepath.Join(baseDir)
}

// GetMailboxFile returns the mailbox file path for an agent.
func GetMailboxFile(mailboxDir, agentName string) string {
	return filepath.Join(mailboxDir, sanitizeName(agentName)+".jsonl")
}

// WriteToMailbox appends an entry to an agent's mailbox file.
func WriteToMailbox(mailboxDir, agentName string, entry MailboxEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if err := os.MkdirAll(mailboxDir, 0o755); err != nil {
		return fmt.Errorf("WriteToMailbox: mkdir: %w", err)
	}
	path := GetMailboxFile(mailboxDir, agentName)
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("WriteToMailbox: marshal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("WriteToMailbox: open: %w", err)
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// ReadAndClearMailbox reads all entries from an agent's mailbox and truncates the file.
func ReadAndClearMailbox(mailboxDir, agentName string) ([]MailboxEntry, error) {
	path := GetMailboxFile(mailboxDir, agentName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ReadAndClearMailbox: %w", err)
	}

	var entries []MailboxEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e MailboxEntry
		if err := json.Unmarshal([]byte(line), &e); err == nil {
			entries = append(entries, e)
		}
	}

	// Truncate after reading (drain semantics)
	_ = os.WriteFile(path, nil, 0o644)

	return entries, nil
}

// --- Permission sync (file-based fallback) ---

// GetPermissionsDir returns the permissions directory for a team.
func GetPermissionsDir(teamName string) string {
	return filepath.Join(GetTeamDir(teamName), "permissions")
}

// GenerateRequestID creates a unique permission request ID.
func GenerateRequestID() string {
	return fmt.Sprintf("perm-%d-%d", time.Now().UnixMilli(), rand.Int63n(100000))
}

// CreatePermissionRequest builds a pending permission request with the same
// default bookkeeping fields as the TypeScript implementation.
func CreatePermissionRequest(params PermissionRequestParams) (*SwarmPermissionRequest, error) {
	switch {
	case strings.TrimSpace(params.TeamName) == "":
		return nil, fmt.Errorf("CreatePermissionRequest: team name is required")
	case strings.TrimSpace(params.WorkerID) == "":
		return nil, fmt.Errorf("CreatePermissionRequest: worker ID is required")
	case strings.TrimSpace(params.WorkerName) == "":
		return nil, fmt.Errorf("CreatePermissionRequest: worker name is required")
	case strings.TrimSpace(params.ToolName) == "":
		return nil, fmt.Errorf("CreatePermissionRequest: tool name is required")
	case strings.TrimSpace(params.ToolUseID) == "":
		return nil, fmt.Errorf("CreatePermissionRequest: toolUseID is required")
	}

	input := make(map[string]any, len(params.Input))
	for k, v := range params.Input {
		input[k] = v
	}
	suggestions := append([]any(nil), params.PermissionSuggestions...)

	return &SwarmPermissionRequest{
		ID:                    GenerateRequestID(),
		WorkerID:              params.WorkerID,
		WorkerName:            params.WorkerName,
		WorkerColor:           params.WorkerColor,
		TeamName:              params.TeamName,
		ToolName:              params.ToolName,
		ToolUseID:             params.ToolUseID,
		Description:           params.Description,
		Input:                 input,
		PermissionSuggestions: suggestions,
		Status:                "pending",
		CreatedAt:             time.Now().UnixMilli(),
	}, nil
}

// WritePendingPermission writes a permission request to the pending directory.
func WritePendingPermission(req *SwarmPermissionRequest) error {
	dir := filepath.Join(GetPermissionsDir(req.TeamName), "pending")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("WritePendingPermission: mkdir: %w", err)
	}
	path := filepath.Join(dir, req.ID+".json")
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return fmt.Errorf("WritePendingPermission: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// ReadPendingPermissions reads all pending permission requests for a team.
func ReadPendingPermissions(teamName string) ([]*SwarmPermissionRequest, error) {
	dir := filepath.Join(GetPermissionsDir(teamName), "pending")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ReadPendingPermissions: %w", err)
	}

	var reqs []*SwarmPermissionRequest
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req SwarmPermissionRequest
		if err := json.Unmarshal(data, &req); err == nil {
			reqs = append(reqs, &req)
		}
	}
	return reqs, nil
}

// ResolvePermission writes the leader's resolution to the resolved directory
// and removes the pending file.
func ResolvePermission(teamName, requestID string, resolution PermissionResolution) error {
	pendingPath := filepath.Join(GetPermissionsDir(teamName), "pending", requestID+".json")
	resolvedDir := filepath.Join(GetPermissionsDir(teamName), "resolved")
	if err := os.MkdirAll(resolvedDir, 0o755); err != nil {
		return fmt.Errorf("ResolvePermission: mkdir: %w", err)
	}

	// Write resolution
	resolvedPath := filepath.Join(resolvedDir, requestID+".json")
	resolution.ResolvedBy = "leader"
	data, _ := json.MarshalIndent(resolution, "", "  ")
	if err := os.WriteFile(resolvedPath, data, 0o644); err != nil {
		return fmt.Errorf("ResolvePermission: write: %w", err)
	}

	// Remove pending
	_ = os.Remove(pendingPath)
	return nil
}

// PollForPermissionResponse polls the resolved directory for a response to requestID.
// Returns nil if not yet resolved.
func PollForPermissionResponse(teamName, requestID string) (*PermissionResolution, error) {
	path := filepath.Join(GetPermissionsDir(teamName), "resolved", requestID+".json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("PollForPermissionResponse: %w", err)
	}
	var res PermissionResolution
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("PollForPermissionResponse: parse: %w", err)
	}
	// Remove after reading
	_ = os.Remove(path)
	return &res, nil
}

// WaitForPermissionResponse blocks until the given request is resolved or the
// caller's context is cancelled.
func WaitForPermissionResponse(ctx context.Context, teamName, requestID string, pollInterval time.Duration) (*PermissionResolution, error) {
	if pollInterval <= 0 {
		pollInterval = defaultPermissionPollInterval
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		resolution, err := PollForPermissionResponse(teamName, requestID)
		if err != nil {
			return nil, err
		}
		if resolution != nil {
			return resolution, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// RequestPermission routes a permission request through the leader bridge when
// available and otherwise falls back to the filesystem-backed mailbox flow.
func RequestPermission(ctx context.Context, req *SwarmPermissionRequest, opts PermissionRequestOptions) (*PermissionResolution, error) {
	if req == nil {
		return nil, fmt.Errorf("RequestPermission: request is nil")
	}

	if handler := GetLeaderPermissionHandler(); handler != nil {
		return handler(ctx, req)
	}

	if err := WritePendingPermission(req); err != nil {
		return nil, err
	}

	return WaitForPermissionResponse(ctx, req.TeamName, req.ID, opts.PollInterval)
}

// CleanupOldResolutions removes resolved permission files older than maxAge.
func CleanupOldResolutions(teamName string, maxAge time.Duration) (int, error) {
	dir := filepath.Join(GetPermissionsDir(teamName), "resolved")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
			count++
		}
	}
	return count, nil
}
