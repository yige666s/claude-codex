package powershell

import "testing"

func TestNeverSuggestCommandIncludesDangerousFamilies(t *testing.T) {
	for _, name := range []string{
		"invoke-expression",
		"invoke-webrequest",
		"import-module",
		"set-alias",
		"invoke-cimmethod",
		"format-table",
		"pwsh",
		"node",
	} {
		if !NeverSuggestCommand(name) {
			t.Fatalf("NeverSuggestCommand(%q) = false, want true", name)
		}
	}
}

func TestNeverSuggestCommandTrimsAndLowercases(t *testing.T) {
	if !NeverSuggestCommand("  Invoke-WebRequest  ") {
		t.Fatal("NeverSuggestCommand() should match case-insensitively with trimming")
	}
}

func TestNeverSuggestCommandRejectsSafeNames(t *testing.T) {
	for _, name := range []string{"get-item", "write-warning", "git"} {
		if NeverSuggestCommand(name) {
			t.Fatalf("NeverSuggestCommand(%q) = true, want false", name)
		}
	}
}
