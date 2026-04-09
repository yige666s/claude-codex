package swarm

import (
	"context"
	"testing"
	"time"
)

func TestInProcessBackendSpawnPropagatesPermissionRuntimeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := CreateTeamFile("demo-team", "demo", TeamLeadName, "session-1"); err != nil {
		t.Fatalf("CreateTeamFile() error = %v", err)
	}

	runCfgCh := make(chan InProcessRunConfig, 1)
	backend := NewInProcessBackend(func(ctx context.Context, cfg InProcessRunConfig) (<-chan string, error) {
		runCfgCh <- cfg
		stream := make(chan string)
		close(stream)
		return stream, nil
	}, GetMailboxDir(home, "demo-team"))

	result, err := backend.Spawn(TeammateSpawnConfig{
		TeammateIdentity: TeammateIdentity{
			Name:     "researcher",
			TeamName: "demo-team",
			Color:    "blue",
		},
		Prompt:                 "inspect permissions",
		CWD:                    home,
		Model:                  "gpt-5.4",
		SystemPrompt:           "system",
		SystemPromptMode:       "append",
		WorktreePath:           home + "/worktree",
		ParentSessionID:        "parent-session",
		Permissions:            []string{"Read", "Edit"},
		AllowPermissionPrompts: true,
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Spawn() success = false: %+v", result)
	}

	select {
	case runCfg := <-runCfgCh:
		if got := runCfg.AllowedTools; len(got) != 2 || got[0] != "Read" || got[1] != "Edit" {
			t.Fatalf("AllowedTools = %#v, want [Read Edit]", got)
		}
		if !runCfg.AllowPermissionPrompts {
			t.Fatal("AllowPermissionPrompts = false, want true")
		}
		if runCfg.ParentSessionID != "parent-session" {
			t.Fatalf("ParentSessionID = %q, want parent-session", runCfg.ParentSessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner config")
	}
}
