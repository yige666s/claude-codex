package swarm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestTeamFileLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const teamName = "Alpha Team"
	if _, err := CreateTeamFile(teamName, "demo", "leader@alpha-team", "session-1"); err != nil {
		t.Fatalf("CreateTeamFile failed: %v", err)
	}

	member := TeamMember{
		AgentID:       "worker-1@alpha-team",
		Name:          "worker-1",
		JoinedAt:      time.Now().UnixMilli(),
		TmuxPaneID:    InProcessMarker,
		CWD:           "/tmp/project",
		Subscriptions: []string{"team"},
	}
	if err := AddMember(teamName, member); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}
	if err := SetMemberActive(teamName, member.Name, true); err != nil {
		t.Fatalf("SetMemberActive failed: %v", err)
	}
	if err := SetMemberMode(teamName, member.Name, "plan"); err != nil {
		t.Fatalf("SetMemberMode failed: %v", err)
	}

	tf, err := ReadTeamFile(teamName)
	if err != nil {
		t.Fatalf("ReadTeamFile failed: %v", err)
	}
	if tf == nil || tf.Name != teamName {
		t.Fatalf("unexpected team file: %+v", tf)
	}
	if len(tf.Members) != 1 || !tf.Members[0].IsActive || tf.Members[0].Mode != "plan" {
		t.Fatalf("unexpected member state: %+v", tf.Members)
	}

	removed, err := RemoveMemberByAgentID(teamName, member.AgentID)
	if err != nil {
		t.Fatalf("RemoveMemberByAgentID failed: %v", err)
	}
	if !removed {
		t.Fatal("expected member removal to succeed")
	}
	removed, err = RemoveMemberByAgentID(teamName, member.AgentID)
	if err != nil {
		t.Fatalf("second RemoveMemberByAgentID failed: %v", err)
	}
	if removed {
		t.Fatal("expected second removal to report false")
	}

	teams, err := ListTeams()
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}
	if !slices.Contains(teams, sanitizeName(teamName)) {
		t.Fatalf("expected %q in listed teams, got %#v", sanitizeName(teamName), teams)
	}
	if !IsTeamLeader("") || !IsTeamLeader(TeamLeadName) || IsTeamLeader("worker-1") {
		t.Fatal("unexpected team leader detection result")
	}
}

func TestMailboxReadAndClear(t *testing.T) {
	mailboxDir := t.TempDir()
	entry := MailboxEntry{From: "leader", Text: "hello"}
	if err := WriteToMailbox(mailboxDir, "Worker.One", entry); err != nil {
		t.Fatalf("WriteToMailbox failed: %v", err)
	}

	path := GetMailboxFile(mailboxDir, "Worker.One")
	if filepath.Base(path) != "worker-one.jsonl" {
		t.Fatalf("unexpected mailbox filename: %s", path)
	}

	entries, err := ReadAndClearMailbox(mailboxDir, "Worker.One")
	if err != nil {
		t.Fatalf("ReadAndClearMailbox failed: %v", err)
	}
	if len(entries) != 1 || entries[0].From != entry.From || entries[0].Text != entry.Text || entries[0].Timestamp.IsZero() {
		t.Fatalf("unexpected drained entries: %+v", entries)
	}

	entries, err = ReadAndClearMailbox(mailboxDir, "Worker.One")
	if err != nil {
		t.Fatalf("second ReadAndClearMailbox failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected mailbox to be empty after drain, got %+v", entries)
	}
}

func TestPermissionRequestLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	teamName := "Alpha Team"
	req := &SwarmPermissionRequest{
		ID:          "perm-123",
		WorkerID:    "worker-1@alpha-team",
		WorkerName:  "worker-1",
		TeamName:    teamName,
		ToolName:    "bash",
		ToolUseID:   "tool-1",
		Description: "run bash",
		Input:       map[string]any{"command": "git status"},
		Status:      "pending",
		CreatedAt:   time.Now().UnixMilli(),
	}
	if err := WritePendingPermission(req); err != nil {
		t.Fatalf("WritePendingPermission failed: %v", err)
	}

	pending, err := ReadPendingPermissions(teamName)
	if err != nil {
		t.Fatalf("ReadPendingPermissions failed: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != req.ID {
		t.Fatalf("unexpected pending requests: %+v", pending)
	}

	resolution := PermissionResolution{Decision: "approved", Feedback: "ok"}
	if err := ResolvePermission(teamName, req.ID, resolution); err != nil {
		t.Fatalf("ResolvePermission failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(GetPermissionsDir(teamName), "pending", req.ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("expected pending file to be removed, stat err=%v", err)
	}

	resolved, err := PollForPermissionResponse(teamName, req.ID)
	if err != nil {
		t.Fatalf("PollForPermissionResponse failed: %v", err)
	}
	if resolved == nil || resolved.Decision != "approved" || resolved.ResolvedBy != "leader" || resolved.Feedback != "ok" {
		t.Fatalf("unexpected resolution: %+v", resolved)
	}
	resolved, err = PollForPermissionResponse(teamName, req.ID)
	if err != nil {
		t.Fatalf("second PollForPermissionResponse failed: %v", err)
	}
	if resolved != nil {
		t.Fatalf("expected resolved response to be drained, got %+v", resolved)
	}

	oldPath := filepath.Join(GetPermissionsDir(teamName), "resolved", "old.json")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatalf("mkdir resolved dir failed: %v", err)
	}
	if err := os.WriteFile(oldPath, []byte(`{"decision":"approved"}`), 0o644); err != nil {
		t.Fatalf("write old resolution failed: %v", err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}
	removed, err := CleanupOldResolutions(teamName, time.Hour)
	if err != nil {
		t.Fatalf("CleanupOldResolutions failed: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected one old resolution to be removed, got %d", removed)
	}
	if id := GenerateRequestID(); len(id) == 0 || id[:5] != "perm-" {
		t.Fatalf("unexpected request id: %q", id)
	}
}

func TestInProcessBackendLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	mailboxDir := t.TempDir()
	runnerStarted := make(chan struct{}, 1)
	runnerStopped := make(chan struct{}, 1)
	backend := NewInProcessBackend(func(ctx context.Context, cfg InProcessRunConfig) (<-chan string, error) {
		if cfg.AgentID == "" || cfg.Identity.Name == "" {
			return nil, fmt.Errorf("missing identity")
		}
		stream := make(chan string)
		runnerStarted <- struct{}{}
		go func() {
			defer close(stream)
			<-ctx.Done()
			runnerStopped <- struct{}{}
		}()
		return stream, nil
	}, mailboxDir)

	result, err := backend.Spawn(TeammateSpawnConfig{
		TeammateIdentity: TeammateIdentity{Name: "worker-2", TeamName: "Alpha Team", Color: "cyan"},
		CWD:              t.TempDir(),
		WorktreePath:     t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	if !result.Success || result.AgentID == "" || result.TaskID == "" {
		t.Fatalf("unexpected spawn result: %+v", result)
	}
	select {
	case <-runnerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start")
	}
	if !backend.IsActive(result.AgentID) {
		t.Fatal("expected spawned agent to be active")
	}

	message := TeammateMessage{From: "leader-fixed", Text: "ping", Timestamp: time.Now()}
	if err := backend.SendMessage(result.AgentID, message); err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	entries, err := ReadAndClearMailbox(mailboxDir, string(result.AgentID))
	if err != nil {
		t.Fatalf("ReadAndClearMailbox after SendMessage failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Text != message.Text {
		t.Fatalf("unexpected sent message entries: %+v", entries)
	}

	if err := backend.Terminate(result.AgentID, "done"); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	entries, err = ReadAndClearMailbox(mailboxDir, string(result.AgentID))
	if err != nil {
		t.Fatalf("ReadAndClearMailbox after Terminate failed: %v", err)
	}
	if len(entries) != 1 || entries[0].From != TeamLeadName || entries[0].Text == "" {
		t.Fatalf("unexpected terminate message: %+v", entries)
	}

	if err := backend.Kill(result.AgentID); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}
	select {
	case <-runnerStopped:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stop after kill")
	}
	if backend.IsActive(result.AgentID) {
		t.Fatal("killed agent should not remain active")
	}
	if err := backend.Kill(result.AgentID); err == nil {
		t.Fatal("expected second kill to fail")
	}
}
