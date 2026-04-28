package swarm

import "testing"

func TestBuildTeamStatusSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := CreateTeamFile("Alpha Team", "demo", "leader@alpha-team", "session-lead"); err != nil {
		t.Fatalf("CreateTeamFile() error = %v", err)
	}
	members := []TeamMember{
		{
			AgentID:     "worker-1@alpha-team",
			Name:        "worker-1",
			Color:       "cyan",
			TmuxPaneID:  InProcessMarker,
			CWD:         home,
			BackendType: string(BackendTypeInProcess),
			IsActive:    true,
			Mode:        "default",
		},
		{
			AgentID:     "worker-2@alpha-team",
			Name:        "worker-2",
			Color:       "green",
			TmuxPaneID:  "%7",
			CWD:         home,
			BackendType: string(BackendTypeTmux),
			IsActive:    false,
			Mode:        "plan",
		},
	}
	for _, member := range members {
		if err := AddMember("Alpha Team", member); err != nil {
			t.Fatalf("AddMember() error = %v", err)
		}
	}

	snapshot, err := BuildTeamStatusSnapshot("Alpha Team")
	if err != nil {
		t.Fatalf("BuildTeamStatusSnapshot() error = %v", err)
	}
	if snapshot.TeamName != "Alpha Team" || snapshot.TotalTeammates != 2 || snapshot.ActiveTeammates != 1 {
		t.Fatalf("unexpected counts: %+v", snapshot)
	}
	if snapshot.BackendCounts[BackendTypeInProcess] != 1 || snapshot.BackendCounts[BackendTypeTmux] != 1 {
		t.Fatalf("unexpected backend counts: %+v", snapshot.BackendCounts)
	}
	if len(snapshot.Members) != 2 || snapshot.Members[0].DisplayStatus != "running" || snapshot.Members[1].DisplayStatus != "disconnected" {
		t.Fatalf("unexpected member statuses: %+v", snapshot.Members)
	}
	if snapshot.FooterText != "1/2 teammates" {
		t.Fatalf("FooterText = %q, want 1/2 teammates", snapshot.FooterText)
	}
}

func TestBuildTeamStatusSnapshotMissingTeam(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	snapshot, err := BuildTeamStatusSnapshot("missing")
	if err != nil {
		t.Fatalf("BuildTeamStatusSnapshot() error = %v", err)
	}
	if snapshot != nil {
		t.Fatalf("expected nil snapshot for missing team, got %+v", snapshot)
	}
}
