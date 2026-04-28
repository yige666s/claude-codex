package team

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"claude-codex/internal/harness/coordinator"
	"claude-codex/internal/harness/swarm"
)

func TestCreateDeleteTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	manager := coordinator.NewManager(coordinator.Config{ScratchpadDir: t.TempDir()})
	create := NewTeamCreateTool(manager)
	raw, _ := json.Marshal(map[string]any{
		"name":            "alpha",
		"description":     "demo team",
		"lead_agent_id":   "lead@alpha",
		"lead_session_id": "session-lead",
	})
	result, err := create.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if !strings.Contains(result.Output, `Created team "alpha"`) {
		t.Fatalf("unexpected create output: %q", result.Output)
	}

	teams, err := manager.ListTeams()
	if err != nil {
		t.Fatalf("list teams: %v", err)
	}
	if len(teams) != 1 || teams[0].Name != "alpha" {
		t.Fatalf("unexpected teams after create: %#v", teams)
	}
	tf, err := swarm.ReadTeamFile("alpha")
	if err != nil {
		t.Fatalf("read swarm team file: %v", err)
	}
	if tf == nil || tf.Description != "demo team" || tf.LeadAgentID != "lead@alpha" || tf.LeadSessionID != "session-lead" {
		t.Fatalf("unexpected swarm team file: %+v", tf)
	}

	_, err = create.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "team already exists") {
		t.Fatalf("expected duplicate create error, got: %v", err)
	}

	del := NewTeamDeleteTool(manager)
	result, err = del.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("delete team: %v", err)
	}
	if !strings.Contains(result.Output, `Deleted team "alpha".`) {
		t.Fatalf("unexpected delete output: %q", result.Output)
	}

	teams, err = manager.ListTeams()
	if err != nil {
		t.Fatalf("list teams after delete: %v", err)
	}
	if len(teams) != 0 {
		t.Fatalf("expected empty team list after delete, got: %#v", teams)
	}
	tf, err = swarm.ReadTeamFile("alpha")
	if err != nil {
		t.Fatalf("read swarm team file after delete: %v", err)
	}
	if tf != nil {
		t.Fatalf("expected swarm team file to be deleted, got %+v", tf)
	}

	_, err = del.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "team not found") {
		t.Fatalf("expected missing delete error, got: %v", err)
	}
}
