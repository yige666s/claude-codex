package teleport

import "testing"

func TestSelectEnvironmentPrefersConfiguredID(t *testing.T) {
	envs := []EnvironmentResource{
		{Kind: EnvironmentKindBridge, EnvironmentID: "bridge-1", Name: "Bridge"},
		{Kind: EnvironmentKindAnthropicCloud, EnvironmentID: "cloud-1", Name: "Cloud"},
	}
	info := SelectEnvironment(envs, "cloud-1", "project")
	if info.SelectedEnvironment == nil || info.SelectedEnvironment.EnvironmentID != "cloud-1" {
		t.Fatalf("unexpected selection %#v", info)
	}
	if info.SelectedSource != "project" {
		t.Fatalf("expected source project, got %#v", info)
	}
}

func TestSelectEnvironmentDefaultsToFirstNonBridge(t *testing.T) {
	envs := []EnvironmentResource{
		{Kind: EnvironmentKindBridge, EnvironmentID: "bridge-1", Name: "Bridge"},
		{Kind: EnvironmentKindBYOC, EnvironmentID: "byoc-1", Name: "BYOC"},
	}
	info := SelectEnvironment(envs, "", "")
	if info.SelectedEnvironment == nil || info.SelectedEnvironment.EnvironmentID != "byoc-1" {
		t.Fatalf("unexpected selection %#v", info)
	}
}
