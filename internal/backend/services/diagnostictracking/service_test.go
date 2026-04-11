package diagnostictracking

import "testing"

func TestDiagnosticTrackingService(t *testing.T) {
	service := NewService()
	service.BeforeFileEdited("/tmp/test.go", []Diagnostic{{Message: "old", Severity: "Warning"}})
	newDiagnostics := service.GetNewDiagnostics([]DiagnosticFile{{
		URI: "/tmp/test.go",
		Diagnostics: []Diagnostic{
			{Message: "old", Severity: "Warning"},
			{Message: "new", Severity: "Error"},
		},
	}})
	if len(newDiagnostics) != 1 || len(newDiagnostics[0].Diagnostics) != 1 || newDiagnostics[0].Diagnostics[0].Message != "new" {
		t.Fatalf("unexpected diagnostics diff: %#v", newDiagnostics)
	}
}
