package github

import (
	"context"
	"errors"
	"testing"
)

func TestGetGHAuthStatus(t *testing.T) {
	originalLookPath := lookPath
	originalRun := runCommand
	defer func() {
		lookPath = originalLookPath
		runCommand = originalRun
	}()

	lookPath = func(file string) (string, error) {
		if file != "gh" {
			t.Fatalf("unexpected file lookup %q", file)
		}
		return "", errors.New("missing")
	}
	if got := GetAuthStatus(context.Background()); got != StatusNotInstalled {
		t.Fatalf("expected not installed, got %q", got)
	}

	lookPath = func(string) (string, error) { return "/usr/bin/gh", nil }
	runCommand = func(context.Context, string, ...string) (int, error) { return 0, nil }
	if got := GetAuthStatus(context.Background()); got != StatusAuthenticated {
		t.Fatalf("expected authenticated, got %q", got)
	}

	runCommand = func(context.Context, string, ...string) (int, error) { return 1, nil }
	if got := GetAuthStatus(context.Background()); got != StatusNotAuthenticated {
		t.Fatalf("expected not authenticated, got %q", got)
	}
}
