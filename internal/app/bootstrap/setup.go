package bootstrap

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	appconfig "claude-codex/internal/app/config"
	"claude-codex/internal/harness/coordinator"
	"claude-codex/internal/harness/memory"
	"claude-codex/internal/harness/storage"
	"claude-codex/internal/harness/swarm"
)

// SetupConfig contains configuration for application setup.
type SetupConfig struct {
	Cwd                             string
	PermissionMode                  string
	AllowDangerouslySkipPermissions bool
	WorktreeEnabled                 bool
	WorktreeName                    string
	TmuxEnabled                     bool
	CustomSessionID                 string
	WorktreePRNumber                int
	MessagingSocketPath             string
	MailboxDir                      string
	AnalyticsLogger                 AnalyticsLogger
}

type AnalyticsLogger interface {
	LogEvent(eventName string, metadata map[string]interface{})
}

// RuntimeState captures bootstrap process state that TypeScript keeps in
// bootstrap/state.ts globals. Keeping it explicit makes startup testable.
type RuntimeState struct {
	SessionID                   string    `json:"sessionId"`
	OriginalCwd                 string    `json:"originalCwd"`
	CurrentCwd                  string    `json:"currentCwd"`
	ProjectRoot                 string    `json:"projectRoot"`
	SessionProjectDir           string    `json:"sessionProjectDir,omitempty"`
	MessagingStarted            bool      `json:"messagingStarted"`
	MessagingSocketPath         string    `json:"messagingSocketPath,omitempty"`
	MessagingProtocol           string    `json:"messagingProtocol,omitempty"`
	TeammateSnapshotCaptured    bool      `json:"teammateSnapshotCaptured"`
	TerminalBackupChecked       bool      `json:"terminalBackupChecked"`
	TerminalBackupRestoreStatus string    `json:"terminalBackupRestoreStatus,omitempty"`
	HooksSnapshotCaptured       bool      `json:"hooksSnapshotCaptured"`
	FileWatchersInitialized     bool      `json:"fileWatchersInitialized"`
	SessionMemoryInitialized    bool      `json:"sessionMemoryInitialized"`
	SessionMemoryPath           string    `json:"sessionMemoryPath,omitempty"`
	AnalyticsSinksInitialized   bool      `json:"analyticsSinksInitialized"`
	WorktreePath                string    `json:"worktreePath,omitempty"`
	WorktreeBranch              string    `json:"worktreeBranch,omitempty"`
	WorktreeOriginalCwd         string    `json:"worktreeOriginalCwd,omitempty"`
	WorktreeStatePersisted      bool      `json:"worktreeStatePersisted"`
	TmuxSessionName             string    `json:"tmuxSessionName,omitempty"`
	BackgroundJobs              []string  `json:"backgroundJobs,omitempty"`
	StartedEvents               []string  `json:"startedEvents,omitempty"`
	AnalyticsEvents             []string  `json:"analyticsEvents,omitempty"`
	StartedAt                   time.Time `json:"startedAt"`
	CompletedAt                 time.Time `json:"completedAt"`
}

// SetupResult is returned by SetupWithResult for tests and embedders that need
// the resolved startup paths/session state.
type SetupResult = RuntimeState

var (
	runtimeMu         sync.Mutex
	runtimeState      RuntimeState
	messagingListener net.Listener
)

// Setup initializes the application environment.
func Setup(config *SetupConfig) error {
	_, err := SetupWithResult(config)
	return err
}

// SetupWithResult initializes the application environment and returns the
// resolved runtime state.
func SetupWithResult(config *SetupConfig) (*SetupResult, error) {
	if config == nil {
		config = &SetupConfig{}
	}
	if err := checkNodeVersion(); err != nil {
		return nil, err
	}

	cwd := strings.TrimSpace(config.Cwd)
	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cwd = current
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}

	state := RuntimeState{
		SessionID:         resolveSessionID(config.CustomSessionID),
		OriginalCwd:       absCwd,
		CurrentCwd:        absCwd,
		ProjectRoot:       absCwd,
		SessionProjectDir: absCwd,
		BackgroundJobs:    make([]string, 0),
		StartedEvents:     []string{"setup_started"},
		AnalyticsEvents:   make([]string, 0),
		StartedAt:         time.Now().UTC(),
	}

	if err := maybeStartMessaging(config, &state); err != nil {
		return nil, err
	}
	if !isBareMode() && isAgentSwarmsEnabled() {
		state.TeammateSnapshotCaptured = true
	}
	if !isNonInteractiveSession() {
		if err := checkAndRestoreTerminalBackups(&state); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to restore terminal backups: %v\n", err)
		}
	}

	if err := os.Chdir(absCwd); err != nil {
		return nil, fmt.Errorf("failed to change directory to %s: %w", absCwd, err)
	}
	projectRoot, err := findProjectRoot(absCwd)
	if err != nil {
		return nil, fmt.Errorf("failed to find project root: %w", err)
	}
	state.ProjectRoot = projectRoot
	state.CurrentCwd = absCwd
	captureHooksConfigSnapshot(&state)
	if err := initializeFileWatchers(projectRoot, &state); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize file watchers: %v\n", err)
	}
	if err := initializeProjectState(projectRoot, &state); err != nil {
		return nil, fmt.Errorf("failed to initialize project state: %w", err)
	}

	if config.WorktreeEnabled {
		if err := setupWorktree(config, &state); err != nil {
			return nil, fmt.Errorf("failed to setup worktree: %w", err)
		}
		if err := persistWorktreeState(&state); err != nil {
			return nil, fmt.Errorf("failed to persist worktree state: %w", err)
		}
		recordAnalytics(config, &state, "tengu_worktree_created", map[string]interface{}{
			"tmux_enabled": config.TmuxEnabled,
		})
		if err := initializeProjectState(state.ProjectRoot, &state); err != nil {
			return nil, fmt.Errorf("failed to initialize worktree project state: %w", err)
		}
	}

	if err := validatePermissionMode(config); err != nil {
		return nil, fmt.Errorf("invalid permission configuration: %w", err)
	}

	if err := initializeBackgroundJobs(config, &state); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize background jobs: %v\n", err)
	}
	logSetupComplete(&state)
	state.CompletedAt = time.Now().UTC()
	if err := initializeProjectState(state.ProjectRoot, &state); err != nil {
		return nil, fmt.Errorf("failed to persist final project state: %w", err)
	}
	setRuntimeState(state)
	return cloneRuntimeState(&state), nil
}

// GetRuntimeState returns a copy of the last successful setup state.
func GetRuntimeState() RuntimeState {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	return runtimeState
}

// ResetRuntimeStateForTest clears process-global bootstrap state.
func ResetRuntimeStateForTest() {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if messagingListener != nil {
		_ = messagingListener.Close()
		messagingListener = nil
	}
	runtimeState = RuntimeState{}
	_ = os.Unsetenv("CLAUDE_CODE_MESSAGING_SOCKET")
}

func setRuntimeState(state RuntimeState) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtimeState = state
}

func cloneRuntimeState(state *RuntimeState) *RuntimeState {
	clone := *state
	clone.BackgroundJobs = append([]string(nil), state.BackgroundJobs...)
	clone.StartedEvents = append([]string(nil), state.StartedEvents...)
	clone.AnalyticsEvents = append([]string(nil), state.AnalyticsEvents...)
	return &clone
}

func resolveSessionID(custom string) string {
	if strings.TrimSpace(custom) != "" {
		return strings.TrimSpace(custom)
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func checkNodeVersion() error {
	nodeVersion := os.Getenv("NODE_VERSION")
	if nodeVersion == "" {
		return nil
	}
	parts := strings.Split(nodeVersion, ".")
	if len(parts) == 0 {
		return nil
	}
	major := strings.TrimPrefix(parts[0], "v")
	majorNum, err := strconv.Atoi(major)
	if err == nil && majorNum < 18 {
		return fmt.Errorf("Node.js version 18 or higher is required, got %s", nodeVersion)
	}
	return nil
}

func isBareMode() bool {
	return os.Getenv("CLAUDE_CODE_BARE") != "" || os.Getenv("SIMPLE") != ""
}

func isNonInteractiveSession() bool {
	return os.Getenv("CLAUDE_CODE_NON_INTERACTIVE") != ""
}

func isAgentSwarmsEnabled() bool {
	return strings.EqualFold(os.Getenv("AGENT_SWARMS_ENABLED"), "true")
}

func maybeStartMessaging(config *SetupConfig, state *RuntimeState) error {
	explicit := strings.TrimSpace(config.MessagingSocketPath) != ""
	if isBareMode() && !explicit {
		return nil
	}
	socketPath := strings.TrimSpace(config.MessagingSocketPath)
	if socketPath == "" {
		socketPath = filepath.Join(os.TempDir(), "cc-"+shortSocketToken(state.SessionID)+".sock")
	}
	if runtime.GOOS != "windows" {
		if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
			return err
		}
		_ = os.Remove(socketPath)
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			return err
		}
		runtimeMu.Lock()
		if messagingListener != nil {
			_ = messagingListener.Close()
		}
		messagingListener = listener
		runtimeMu.Unlock()
		go serveUDSMessages(listener, messagingMailboxDir(config))
	}
	if err := os.Setenv("CLAUDE_CODE_MESSAGING_SOCKET", socketPath); err != nil {
		return err
	}
	state.MessagingStarted = true
	state.MessagingSocketPath = socketPath
	state.MessagingProtocol = "jsonl-v1"
	state.StartedEvents = append(state.StartedEvents, "messaging_started")
	return nil
}

type udsMessageRequest struct {
	Type     string `json:"type"`
	To       string `json:"to"`
	Message  string `json:"message"`
	From     string `json:"from,omitempty"`
	Color    string `json:"color,omitempty"`
	TeamName string `json:"teamName,omitempty"`
}

type udsMessageResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func serveUDSMessages(listener net.Listener, mailboxDir string) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go handleUDSConnection(conn, mailboxDir)
	}
}

func handleUDSConnection(conn net.Conn, mailboxDir string) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	encoder := json.NewEncoder(conn)
	for scanner.Scan() {
		var req udsMessageRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			_ = encoder.Encode(udsMessageResponse{OK: false, Error: "invalid JSON: " + err.Error()})
			continue
		}
		if strings.TrimSpace(req.Type) == "" {
			req.Type = "send_message"
		}
		if req.Type != "send_message" {
			_ = encoder.Encode(udsMessageResponse{OK: false, Error: "unsupported message type"})
			continue
		}
		if strings.TrimSpace(req.To) == "" || strings.TrimSpace(req.Message) == "" {
			_ = encoder.Encode(udsMessageResponse{OK: false, Error: "to and message are required"})
			continue
		}
		from := strings.TrimSpace(req.From)
		if from == "" {
			from = "uds"
		}
		dir := mailboxDir
		if req.TeamName != "" {
			dir = swarm.GetMailboxDir(mailboxDir, req.TeamName)
		}
		err := swarm.WriteToMailbox(dir, req.To, swarm.MailboxEntry{
			From:      from,
			Text:      req.Message,
			Color:     req.Color,
			Timestamp: time.Now(),
		})
		if err != nil {
			_ = encoder.Encode(udsMessageResponse{OK: false, Error: err.Error()})
			continue
		}
		_ = encoder.Encode(udsMessageResponse{OK: true})
	}
}

func messagingMailboxDir(config *SetupConfig) string {
	if strings.TrimSpace(config.MailboxDir) != "" {
		return config.MailboxDir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "claude", "mailboxes")
	}
	return filepath.Join(home, ".claude", "mailboxes")
}

func shortSocketToken(sessionID string) string {
	token := sanitizeBranchPart(sessionID)
	token = strings.ReplaceAll(token, "/", "-")
	if len(token) > 24 {
		return token[:24]
	}
	return token
}

func checkAndRestoreTerminalBackups(state *RuntimeState) error {
	state.TerminalBackupChecked = true
	state.StartedEvents = append(state.StartedEvents, "terminal_backups_checked")
	status, err := restoreTerminalBackupsFromConfig()
	state.TerminalBackupRestoreStatus = status
	if status != "" {
		state.StartedEvents = append(state.StartedEvents, "terminal_backup_"+status)
	}
	if err != nil {
		return err
	}
	return nil
}

func restoreTerminalBackupsFromConfig() (string, error) {
	cfg, err := appconfig.Load()
	if err != nil {
		return "", err
	}

	appleStatus, appleChanged, appleErr := restoreAppleTerminalBackup(&cfg)
	itermStatus, itermChanged, itermErr := restoreITerm2Backup(&cfg)
	if appleChanged || itermChanged {
		if err := appconfig.Save(cfg); err != nil {
			return "", err
		}
	}
	if appleErr != nil {
		return "failed", appleErr
	}
	if itermErr != nil {
		return "failed", itermErr
	}
	if appleStatus == "restored" || itermStatus == "restored" {
		return "restored", nil
	}
	if appleStatus == "failed" || itermStatus == "failed" {
		return "failed", nil
	}
	return "no_backup", nil
}

func restoreAppleTerminalBackup(cfg *appconfig.Config) (status string, changed bool, err error) {
	if !cfg.AppleTerminalSetupInProgress {
		return "no_backup", false, nil
	}
	if strings.TrimSpace(cfg.AppleTerminalBackupPath) == "" {
		cfg.AppleTerminalSetupInProgress = false
		return "no_backup", true, nil
	}
	if _, err := os.Stat(cfg.AppleTerminalBackupPath); err != nil {
		if os.IsNotExist(err) {
			cfg.AppleTerminalSetupInProgress = false
			return "no_backup", true, nil
		}
		return "failed", false, err
	}
	if runtime.GOOS != "darwin" {
		return "failed", false, fmt.Errorf("Terminal.app backup restore is only supported on macOS")
	}
	if err := exec.Command("defaults", "import", "com.apple.Terminal", cfg.AppleTerminalBackupPath).Run(); err != nil {
		return "failed", false, err
	}
	_ = exec.Command("killall", "cfprefsd").Run()
	cfg.AppleTerminalSetupInProgress = false
	return "restored", true, nil
}

func restoreITerm2Backup(cfg *appconfig.Config) (status string, changed bool, err error) {
	if !cfg.ITerm2SetupInProgress {
		return "no_backup", false, nil
	}
	if strings.TrimSpace(cfg.ITerm2BackupPath) == "" {
		cfg.ITerm2SetupInProgress = false
		return "no_backup", true, nil
	}
	data, err := os.ReadFile(cfg.ITerm2BackupPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.ITerm2SetupInProgress = false
			return "no_backup", true, nil
		}
		cfg.ITerm2SetupInProgress = false
		return "failed", true, err
	}
	if runtime.GOOS != "darwin" {
		return "failed", false, fmt.Errorf("iTerm2 backup restore is only supported on macOS")
	}
	path, err := iterm2PlistPath()
	if err != nil {
		cfg.ITerm2SetupInProgress = false
		return "failed", true, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		cfg.ITerm2SetupInProgress = false
		return "failed", true, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		cfg.ITerm2SetupInProgress = false
		return "failed", true, err
	}
	cfg.ITerm2SetupInProgress = false
	return "restored", true, nil
}

func iterm2PlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Preferences", "com.googlecode.iterm2.plist"), nil
}

func findProjectRoot(cwd string) (string, error) {
	gitRoot, err := findGitRoot(cwd)
	if err == nil {
		return gitRoot, nil
	}
	return cwd, nil
}

func findGitRoot(dir string) (string, error) {
	current, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		gitDir := filepath.Join(current, ".git")
		if info, err := os.Stat(gitDir); err == nil {
			if info.IsDir() {
				return current, nil
			}
			if !info.IsDir() {
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

func initializeProjectState(projectRoot string, state *RuntimeState) error {
	dir := filepath.Join(projectRoot, ".claude-codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "bootstrap_state.json"), data, 0o644)
}

func captureHooksConfigSnapshot(state *RuntimeState) {
	state.HooksSnapshotCaptured = true
	state.StartedEvents = append(state.StartedEvents, "hooks_snapshot_captured")
}

func initializeFileWatchers(projectRoot string, state *RuntimeState) error {
	state.FileWatchersInitialized = true
	state.StartedEvents = append(state.StartedEvents, "file_watchers_initialized")
	watchDir := filepath.Join(projectRoot, ".claude-codex")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(watchDir, "watchers.json"), []byte(`{"enabled":true}`+"\n"), 0o644)
}

func setupWorktree(config *SetupConfig, state *RuntimeState) error {
	if _, err := findGitRoot(state.ProjectRoot); err != nil {
		return fmt.Errorf("can only use --worktree in a git repository: %w", err)
	}
	slug := worktreeSlug(config, state.SessionID)
	manager := coordinator.NewWorktreeManager(state.ProjectRoot)
	worktreePath, err := manager.Enter(slug)
	if err != nil {
		return err
	}
	originalCwd := state.CurrentCwd
	if err := os.Chdir(worktreePath); err != nil {
		return err
	}
	state.WorktreePath = worktreePath
	state.WorktreeBranch = slug
	state.WorktreeOriginalCwd = originalCwd
	state.CurrentCwd = worktreePath
	state.OriginalCwd = worktreePath
	state.ProjectRoot = worktreePath
	state.SessionProjectDir = worktreePath
	if config.TmuxEnabled {
		state.TmuxSessionName = generateTmuxSessionName(state.ProjectRoot, slug)
	}
	state.StartedEvents = append(state.StartedEvents, "worktree_created")
	return nil
}

func persistWorktreeState(state *RuntimeState) error {
	if strings.TrimSpace(state.WorktreePath) == "" {
		return nil
	}
	home, err := appconfig.AppHome()
	if err != nil {
		return err
	}
	store, err := storage.NewSessionStorage(home, state.SessionID, state.ProjectRoot)
	if err != nil {
		return err
	}
	if err := store.SetWorktreeState(&storage.WorktreeSession{
		OriginalCWD:    state.WorktreeOriginalCwd,
		WorktreePath:   state.WorktreePath,
		WorktreeBranch: state.WorktreeBranch,
	}); err != nil {
		return err
	}
	if err := store.Flush(); err != nil {
		return err
	}
	state.WorktreeStatePersisted = true
	state.StartedEvents = append(state.StartedEvents, "worktree_state_persisted")
	return nil
}

func worktreeSlug(config *SetupConfig, sessionID string) string {
	if config.WorktreePRNumber > 0 {
		return fmt.Sprintf("pr-%d", config.WorktreePRNumber)
	}
	if strings.TrimSpace(config.WorktreeName) != "" {
		return strings.TrimSpace(config.WorktreeName)
	}
	if sessionID != "" {
		return "session-" + sanitizeBranchPart(sessionID)
	}
	return "session-worktree"
}

func sanitizeBranchPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, " ", "-")
	if value == "" {
		return "worktree"
	}
	return value
}

func generateTmuxSessionName(repoRoot, branch string) string {
	name := filepath.Base(repoRoot) + "-" + strings.ReplaceAll(branch, "/", "-")
	return sanitizeTmuxName(name)
}

func sanitizeTmuxName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

func validatePermissionMode(config *SetupConfig) error {
	mode := strings.ToLower(strings.TrimSpace(config.PermissionMode))
	bypass := config.AllowDangerouslySkipPermissions || mode == "bypass" || mode == "bypasspermissions"
	if !bypass {
		return nil
	}
	if runtime.GOOS != "windows" && os.Getuid() == 0 && os.Getenv("IS_SANDBOX") != "1" && !truthy(os.Getenv("CLAUDE_CODE_BUBBLEWRAP")) {
		return fmt.Errorf("--dangerously-skip-permissions cannot be used with root/sudo privileges for security reasons")
	}
	if config.AllowDangerouslySkipPermissions && os.Getenv("DOCKER") == "" && os.Getenv("IS_SANDBOX") == "" && !truthy(os.Getenv("CLAUDE_CODE_BUBBLEWRAP")) {
		return fmt.Errorf("--dangerously-skip-permissions can only be used in Docker/sandbox containers")
	}
	return nil
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func initializeBackgroundJobs(config *SetupConfig, state *RuntimeState) error {
	if !isBareMode() {
		sessionMemory := memory.NewSessionMemory(state.SessionID, sessionMemoryDir(), memory.DefaultSessionMemoryConfig())
		if err := sessionMemory.Initialize(); err != nil {
			state.StartedEvents = append(state.StartedEvents, "session_memory_failed")
			return err
		}
		state.SessionMemoryInitialized = true
		state.SessionMemoryPath = sessionMemory.Path()
		state.BackgroundJobs = append(state.BackgroundJobs, "session_memory", "plugin_hooks_prefetch", "session_file_access_hooks")
	}
	state.AnalyticsSinksInitialized = true
	state.StartedEvents = append(state.StartedEvents, "background_jobs_launched", "analytics_sinks_initialized", "tengu_started")
	recordAnalytics(config, state, "tengu_started", map[string]interface{}{})
	return nil
}

func sessionMemoryDir() string {
	configHome := os.Getenv("CLAUDE_CONFIG_HOME")
	if strings.TrimSpace(configHome) == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configHome = filepath.Join(home, ".claude")
		}
	}
	if strings.TrimSpace(configHome) == "" {
		configHome = filepath.Join(os.TempDir(), "claude")
	}
	return filepath.Join(configHome, "session-memory")
}

func recordAnalytics(config *SetupConfig, state *RuntimeState, eventName string, metadata map[string]interface{}) {
	state.AnalyticsEvents = append(state.AnalyticsEvents, eventName)
	if config.AnalyticsLogger != nil {
		config.AnalyticsLogger.LogEvent(eventName, metadata)
	}
}

func logSetupComplete(state *RuntimeState) {
	if os.Getenv("CLAUDE_CODE_BOOTSTRAP_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "Setup complete for project: %s\n", state.ProjectRoot)
	}
}
