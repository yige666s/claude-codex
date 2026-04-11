package ultraplan

import "testing"

func TestExitPlanModeScannerApprovedAndTeleport(t *testing.T) {
	scanner := NewExitPlanModeScanner()
	result, err := scanner.Ingest([]SDKMessage{
		{Type: "assistant", ToolUses: []ToolUse{{ID: "t1", Name: ExitPlanModeToolName}}},
	})
	if err != nil || result.Kind != ScanPending {
		t.Fatalf("expected pending, got %#v err=%v", result, err)
	}

	result, err = scanner.Ingest([]SDKMessage{
		{Type: "user", ToolResults: []ToolResult{{ToolUseID: "t1", Content: "## Approved Plan:\nship it", IsError: false}}},
	})
	if err != nil || result.Kind != ScanApproved || result.Plan != "ship it" {
		t.Fatalf("expected approved plan, got %#v err=%v", result, err)
	}

	scanner = NewExitPlanModeScanner()
	_, _ = scanner.Ingest([]SDKMessage{{Type: "assistant", ToolUses: []ToolUse{{ID: "t2", Name: ExitPlanModeToolName}}}})
	result, err = scanner.Ingest([]SDKMessage{
		{Type: "user", ToolResults: []ToolResult{{ToolUseID: "t2", Content: UltraplanTeleportSentinel + "\nrun local", IsError: true}}},
	})
	if err != nil || result.Kind != ScanTeleport || result.Plan != "run local" {
		t.Fatalf("expected teleport plan, got %#v err=%v", result, err)
	}
}

func TestExtractApprovedPlanFailsWithoutMarker(t *testing.T) {
	if _, err := ExtractApprovedPlan("missing"); err == nil {
		t.Fatal("expected missing marker to fail")
	}
}
