package github

import (
	"context"
	"os/exec"
)

type AuthStatus string

const (
	StatusAuthenticated    AuthStatus = "authenticated"
	StatusNotAuthenticated AuthStatus = "not_authenticated"
	StatusNotInstalled     AuthStatus = "not_installed"
)

var (
	lookPath   = exec.LookPath
	runCommand = func(ctx context.Context, name string, args ...string) (int, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return exitErr.ExitCode(), nil
			}
			return -1, err
		}
		return 0, nil
	}
)

func GetAuthStatus(ctx context.Context) AuthStatus {
	if _, err := lookPath("gh"); err != nil {
		return StatusNotInstalled
	}
	exitCode, err := runCommand(ctx, "gh", "auth", "token")
	if err != nil {
		return StatusNotAuthenticated
	}
	if exitCode == 0 {
		return StatusAuthenticated
	}
	return StatusNotAuthenticated
}
