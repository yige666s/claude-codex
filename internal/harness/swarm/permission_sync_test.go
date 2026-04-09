package swarm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
)

func TestCreatePermissionRequestValidatesAndClonesInput(t *testing.T) {
	req, err := CreatePermissionRequest(PermissionRequestParams{
		WorkerID:              "worker-1@team",
		WorkerName:            "worker-1",
		TeamName:              "demo-team",
		ToolName:              "Bash",
		ToolUseID:             "toolu_123",
		Description:           "run pwd",
		Input:                 map[string]any{"command": "pwd"},
		PermissionSuggestions: []any{"allow"},
	})
	if err != nil {
		t.Fatalf("CreatePermissionRequest() error = %v", err)
	}
	if req.Status != "pending" {
		t.Fatalf("Status = %q, want pending", req.Status)
	}
	if req.ID == "" {
		t.Fatal("expected generated request ID")
	}
	req.Input["command"] = "mutated"
	if got := req.Input["command"]; got != "mutated" {
		t.Fatalf("request input mutation check failed, got %v", got)
	}

	source := map[string]any{"command": "pwd"}
	req2, err := CreatePermissionRequest(PermissionRequestParams{
		WorkerID:   "worker-1@team",
		WorkerName: "worker-1",
		TeamName:   "demo-team",
		ToolName:   "Bash",
		ToolUseID:  "toolu_456",
		Input:      source,
	})
	if err != nil {
		t.Fatalf("CreatePermissionRequest() error = %v", err)
	}
	source["command"] = "changed-after-create"
	if got := req2.Input["command"]; got != "pwd" {
		t.Fatalf("CreatePermissionRequest() should clone input map, got %v", got)
	}
}

func TestRequestPermissionUsesLeaderBridgeWhenRegistered(t *testing.T) {
	UnregisterLeaderPermissionHandler()
	t.Cleanup(UnregisterLeaderPermissionHandler)

	want := &PermissionResolution{
		Decision:   "approved",
		ResolvedBy: "leader",
		Feedback:   "approved inline",
		PermissionUpdates: []permissions.PermissionUpdate{
			{Type: permissions.UpdateSetMode, Mode: permissions.ModeDefault},
		},
	}
	RegisterLeaderPermissionHandler(func(ctx context.Context, req *SwarmPermissionRequest) (*PermissionResolution, error) {
		if req.ToolName != "Edit" {
			t.Fatalf("ToolName = %q, want Edit", req.ToolName)
		}
		return want, nil
	})

	got, err := RequestPermission(context.Background(), &SwarmPermissionRequest{
		ID:         "perm-inline",
		WorkerID:   "worker-1@team",
		WorkerName: "worker-1",
		TeamName:   "demo-team",
		ToolName:   "Edit",
		ToolUseID:  "toolu_inline",
		Status:     "pending",
		CreatedAt:  time.Now().UnixMilli(),
	}, PermissionRequestOptions{})
	if err != nil {
		t.Fatalf("RequestPermission() error = %v", err)
	}
	if got == nil || got.Decision != "approved" || got.Feedback != "approved inline" {
		t.Fatalf("RequestPermission() = %#v, want inline approved resolution", got)
	}
	if len(got.PermissionUpdates) != 1 {
		t.Fatalf("PermissionUpdates len = %d, want 1", len(got.PermissionUpdates))
	}
}

func TestRequestPermissionFallsBackToFilesystem(t *testing.T) {
	UnregisterLeaderPermissionHandler()
	t.Cleanup(UnregisterLeaderPermissionHandler)

	home := t.TempDir()
	t.Setenv("HOME", home)

	req := &SwarmPermissionRequest{
		ID:          "perm-file",
		WorkerID:    "worker-2@team",
		WorkerName:  "worker-2",
		TeamName:    "demo-team",
		ToolName:    "Bash",
		ToolUseID:   "toolu_file",
		Description: "ls",
		Input:       map[string]any{"command": "ls"},
		Status:      "pending",
		CreatedAt:   time.Now().UnixMilli(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh := make(chan *PermissionResolution, 1)
	errCh := make(chan error, 1)
	go func() {
		resolution, err := RequestPermission(ctx, req, PermissionRequestOptions{PollInterval: 10 * time.Millisecond})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- resolution
	}()

	var pending []*SwarmPermissionRequest
	var err error
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		pending, err = ReadPendingPermissions(req.TeamName)
		if err != nil {
			t.Fatalf("ReadPendingPermissions() error = %v", err)
		}
		if len(pending) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pending) != 1 {
		t.Fatalf("pending requests = %d, want 1", len(pending))
	}

	update := permissions.PermissionUpdate{
		Type:        permissions.UpdateAddRules,
		Destination: permissions.SourceSession,
		Behavior:    permissions.BehaviorAllow,
		Rules: []permissions.RuleValue{
			{ToolName: "Bash", RuleContent: "ls"},
		},
	}
	if err := ResolvePermission(req.TeamName, req.ID, PermissionResolution{
		Decision:          "approved",
		ResolvedBy:        "leader",
		Feedback:          "ok",
		PermissionUpdates: []permissions.PermissionUpdate{update},
	}); err != nil {
		t.Fatalf("ResolvePermission() error = %v", err)
	}

	select {
	case resolution := <-resultCh:
		if resolution.Decision != "approved" {
			t.Fatalf("Decision = %q, want approved", resolution.Decision)
		}
		if len(resolution.PermissionUpdates) != 1 {
			t.Fatalf("PermissionUpdates len = %d, want 1", len(resolution.PermissionUpdates))
		}
		if resolution.PermissionUpdates[0].Destination != permissions.SourceSession {
			t.Fatalf("Destination = %q, want %q", resolution.PermissionUpdates[0].Destination, permissions.SourceSession)
		}
	case err := <-errCh:
		t.Fatalf("RequestPermission() error = %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for permission resolution")
	}
}

func TestWaitForPermissionResponseHonorsContextCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := WaitForPermissionResponse(ctx, "demo-team", "missing", 5*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitForPermissionResponse() error = %v, want deadline exceeded", err)
	}
}
