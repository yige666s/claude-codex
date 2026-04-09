package memdir

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestScanForSecrets(t *testing.T) {
	t.Run("detects representative secret types", func(t *testing.T) {
		matches := ScanForSecrets(strings.Join([]string{
			"aws AKIAABCDEFGHIJKLMNOP",
			"github ghp_abcdefghijklmnopqrstuvwxyz1234567890AB",
			"private -----BEGIN PRIVATE KEY-----\nabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz\n-----END PRIVATE KEY-----",
		}, "\n"))

		got := make([]string, 0, len(matches))
		for _, match := range matches {
			got = append(got, match.Label)
		}

		want := []string{"AWS Access Token", "GitHub PAT", "Private Key"}
		for _, label := range want {
			found := false
			for _, gotLabel := range got {
				if gotLabel == label {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected label %q in %v", label, got)
			}
		}
	})

	t.Run("returns one match per rule", func(t *testing.T) {
		content := "ghp_abcdefghijklmnopqrstuvwxyz1234567890AB\nanother ghp_abcdefghijklmnopqrstuvwxyz1234567890AB"
		matches := ScanForSecrets(content)
		if len(matches) != 1 {
			t.Fatalf("expected one deduplicated match, got %d: %#v", len(matches), matches)
		}
		if matches[0].RuleID != "github-pat" {
			t.Fatalf("expected github-pat, got %#v", matches[0])
		}
	})
}

func TestCheckTeamMemSecrets(t *testing.T) {
	root := t.TempDir()
	teamPath := filepath.Join(GetTeamMemPath(root), "shared.md")

	if err := CheckTeamMemSecrets(filepath.Join(root, "notes.md"), "ghp_abcdefghijklmnopqrstuvwxyz1234567890AB", root); err != nil {
		t.Fatalf("expected non-team path to skip scanning, got %v", err)
	}

	err := CheckTeamMemSecrets(teamPath, "ghp_abcdefghijklmnopqrstuvwxyz1234567890AB", root)
	if err == nil {
		t.Fatal("expected team memory write to be rejected")
	}
	if !strings.Contains(err.Error(), "GitHub PAT") {
		t.Fatalf("expected GitHub PAT label in error, got %q", err)
	}
	if !strings.Contains(err.Error(), "team memory") {
		t.Fatalf("expected team memory context in error, got %q", err)
	}
}
