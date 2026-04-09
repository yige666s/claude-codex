package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// SetupConfig contains configuration for application setup.
type SetupConfig struct {
	Cwd                              string
	PermissionMode                   string
	AllowDangerouslySkipPermissions  bool
	WorktreeEnabled                  bool
	WorktreeName                     string
	TmuxEnabled                      bool
	CustomSessionID                  string
	WorktreePRNumber                 int
	MessagingSocketPath              string
}

// Setup initializes the application environment.
func Setup(config *SetupConfig) error {
	// Check Node.js version (for compatibility during migration)
	if err := checkNodeVersion(); err != nil {
		return err
	}

	// Set custom session ID if provided
	if config.CustomSessionID != "" {
		// TODO: Implement session switching
	}

	// Initialize messaging if not in bare mode
	if !isBareMode() || config.MessagingSocketPath != "" {
		// TODO: Start UDS messaging server
	}

	// Capture teammate snapshot if swarms enabled
	if !isBareMode() && isAgentSwarmsEnabled() {
		// TODO: Capture teammate mode snapshot
	}

	// Terminal backup restoration (interactive only)
	if !isNonInteractiveSession() {
		if err := checkAndRestoreTerminalBackups(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to restore terminal backups: %v\n", err)
		}
	}

	// Set working directory
	if err := os.Chdir(config.Cwd); err != nil {
		return fmt.Errorf("failed to change directory to %s: %w", config.Cwd, err)
	}

	// Find project root
	projectRoot, err := findProjectRoot(config.Cwd)
	if err != nil {
		return fmt.Errorf("failed to find project root: %w", err)
	}

	// Initialize project-specific state
	if err := initializeProjectState(projectRoot); err != nil {
		return fmt.Errorf("failed to initialize project state: %w", err)
	}

	// Setup worktree if enabled
	if config.WorktreeEnabled {
		if err := setupWorktree(config); err != nil {
			return fmt.Errorf("failed to setup worktree: %w", err)
		}
	}

	// Validate permission mode
	if err := validatePermissionMode(config); err != nil {
		return fmt.Errorf("invalid permission configuration: %w", err)
	}

	// Initialize file watchers
	if err := initializeFileWatchers(projectRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize file watchers: %v\n", err)
	}

	// Log setup completion
	logSetupComplete(projectRoot)

	return nil
}

// checkNodeVersion checks if Node.js version is >= 18.
func checkNodeVersion() error {
	nodeVersion := os.Getenv("NODE_VERSION")
	if nodeVersion == "" {
		// Try to get from runtime
		nodeVersion = runtime.Version()
	}

	if nodeVersion != "" {
		parts := strings.Split(nodeVersion, ".")
		if len(parts) > 0 {
			major := strings.TrimPrefix(parts[0], "v")
			if majorNum, err := strconv.Atoi(major); err == nil && majorNum < 18 {
				return fmt.Errorf("Node.js version 18 or higher is required, got %s", nodeVersion)
			}
		}
	}

	return nil
}

// isBareMode checks if running in bare mode.
func isBareMode() bool {
	return os.Getenv("CLAUDE_CODE_BARE") != "" || os.Getenv("SIMPLE") != ""
}

// isNonInteractiveSession checks if this is a non-interactive session.
func isNonInteractiveSession() bool {
	return os.Getenv("CLAUDE_CODE_NON_INTERACTIVE") != ""
}

// isAgentSwarmsEnabled checks if agent swarms are enabled.
func isAgentSwarmsEnabled() bool {
	return os.Getenv("AGENT_SWARMS_ENABLED") == "true"
}

// checkAndRestoreTerminalBackups checks and restores terminal backups if needed.
func checkAndRestoreTerminalBackups() error {
	// TODO: Implement terminal backup restoration
	// - Check for iTerm2 backups
	// - Check for Terminal.app backups
	return nil
}

// findProjectRoot finds the project root directory.
func findProjectRoot(cwd string) (string, error) {
	// Try to find git root
	gitRoot, err := findGitRoot(cwd)
	if err == nil {
		return gitRoot, nil
	}

	// Fall back to current directory
	return cwd, nil
}

// findGitRoot finds the git repository root.
func findGitRoot(dir string) (string, error) {
	current := dir
	for {
		gitDir := filepath.Join(current, ".git")
		if info, err := os.Stat(gitDir); err == nil {
			if info.IsDir() {
				return current, nil
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("not a git repository")
		}
		current = parent
	}
}

// initializeProjectState initializes project-specific state.
func initializeProjectState(projectRoot string) error {
	// TODO: Initialize project configuration
	// TODO: Initialize session memory
	// TODO: Load project settings
	return nil
}

// setupWorktree sets up a git worktree for the session.
func setupWorktree(config *SetupConfig) error {
	// TODO: Implement worktree setup
	// - Create worktree directory
	// - Create branch
	// - Setup tmux session if enabled
	return nil
}

// validatePermissionMode validates the permission configuration.
func validatePermissionMode(config *SetupConfig) error {
	if config.AllowDangerouslySkipPermissions {
		// Validate that we're in a safe environment (Docker/sandbox)
		isDocker := os.Getenv("DOCKER") != ""
		isSandbox := os.Getenv("IS_SANDBOX") != ""

		if !isDocker && !isSandbox {
			return fmt.Errorf("--dangerously-skip-permissions can only be used in Docker/sandbox containers")
		}
	}

	return nil
}

// initializeFileWatchers initializes file change watchers.
func initializeFileWatchers(projectRoot string) error {
	// TODO: Initialize file watchers for:
	// - CLAUDE.md changes
	// - Configuration changes
	// - Hook changes
	return nil
}

// logSetupComplete logs setup completion.
func logSetupComplete(projectRoot string) {
	// TODO: Log analytics event
	fmt.Fprintf(os.Stderr, "Setup complete for project: %s\n", projectRoot)
}
