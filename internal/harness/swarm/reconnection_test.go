package swarm

import "testing"

func TestComputeInitialTeamContextForLeader(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := CreateTeamFile("Alpha Team", "demo", "leader@alpha-team", "session-lead"); err != nil {
		t.Fatalf("CreateTeamFile() error = %v", err)
	}

	ctx, err := ComputeInitialTeamContext(InitialTeamContextOptions{
		TeamName:  "Alpha Team",
		AgentName: TeamLeadName,
	})
	if err != nil {
		t.Fatalf("ComputeInitialTeamContext() error = %v", err)
	}
	if ctx == nil {
		t.Fatal("expected leader context")
	}
	if !ctx.IsLeader || ctx.LeadAgentID != "leader@alpha-team" || ctx.SelfAgentID != "" {
		t.Fatalf("unexpected leader context: %+v", ctx)
	}
	if ctx.TeamFilePath != GetTeamFilePath("Alpha Team") {
		t.Fatalf("TeamFilePath = %q, want %q", ctx.TeamFilePath, GetTeamFilePath("Alpha Team"))
	}
}

func TestComputeInitialTeamContextRestoresTeammateFromTeamFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := CreateTeamFile("Alpha Team", "demo", "leader@alpha-team", "session-lead"); err != nil {
		t.Fatalf("CreateTeamFile() error = %v", err)
	}
	if err := AddMember("Alpha Team", TeamMember{
		AgentID:          "worker-1@Alpha Team",
		Name:             "worker-1",
		Color:            "cyan",
		PlanModeRequired: true,
		TmuxPaneID:       InProcessMarker,
		CWD:              home,
		SessionID:        "session-worker",
		BackendType:      string(BackendTypeInProcess),
		IsActive:         true,
		Mode:             "plan",
	}); err != nil {
		t.Fatalf("AddMember() error = %v", err)
	}

	ctx, err := ComputeInitialTeamContext(InitialTeamContextOptions{
		TeamName:  "Alpha Team",
		AgentName: "worker-1",
	})
	if err != nil {
		t.Fatalf("ComputeInitialTeamContext() error = %v", err)
	}
	if ctx == nil {
		t.Fatal("expected teammate context")
	}
	if ctx.IsLeader || ctx.SelfAgentID != "worker-1@Alpha Team" || ctx.BackendType != BackendTypeInProcess {
		t.Fatalf("unexpected teammate context: %+v", ctx)
	}
	if !ctx.PlanModeRequired || ctx.SessionID != "session-worker" || ctx.Mode != "plan" {
		t.Fatalf("member state not restored: %+v", ctx)
	}
}

func TestComputeInitialTeamContextMissingInputs(t *testing.T) {
	ctx, err := ComputeInitialTeamContext(InitialTeamContextOptions{})
	if err != nil {
		t.Fatalf("ComputeInitialTeamContext() error = %v", err)
	}
	if ctx != nil {
		t.Fatalf("expected nil context for missing inputs, got %+v", ctx)
	}
}
