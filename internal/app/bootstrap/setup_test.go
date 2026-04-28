package bootstrap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	appconfig "claude-codex/internal/app/config"
	"claude-codex/internal/harness/storage"
)

func TestSetupRecordsSessionProjectStateAndMessagingSocket(t *testing.T) {
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
		ResetRuntimeStateForTest()
	})

	cwd := t.TempDir()
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("cc-bootstrap-%d.sock", os.Getpid()))
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
	})
	result, err := SetupWithResult(&SetupConfig{
		Cwd:                 cwd,
		CustomSessionID:     "session-123",
		MessagingSocketPath: socketPath,
	})
	if err != nil {
		t.Fatalf("SetupWithResult() error = %v", err)
	}

	if result.SessionID != "session-123" {
		t.Fatalf("SessionID = %q, want custom session", result.SessionID)
	}
	if result.OriginalCwd != cwd || result.CurrentCwd != cwd || result.ProjectRoot != cwd {
		t.Fatalf("unexpected cwd/project state: %+v", result)
	}
	if !result.MessagingStarted || result.MessagingSocketPath != socketPath {
		t.Fatalf("expected explicit messaging socket to start, got %+v", result)
	}
	if got := os.Getenv("CLAUDE_CODE_MESSAGING_SOCKET"); got != socketPath {
		t.Fatalf("CLAUDE_CODE_MESSAGING_SOCKET = %q, want %q", got, socketPath)
	}
	if !result.FileWatchersInitialized || !result.HooksSnapshotCaptured || !result.SessionMemoryInitialized || !result.AnalyticsSinksInitialized {
		t.Fatalf("expected bootstrap subsystems to initialize, got %+v", result)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".claude-codex", "bootstrap_state.json")); err != nil {
		t.Fatalf("expected project bootstrap state file: %v", err)
	}
}

func TestSetupUDSMessagingWritesMailbox(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UDS messaging is Unix-only")
	}
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
		ResetRuntimeStateForTest()
	})
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("cc-bootstrap-uds-%d.sock", os.Getpid()))
	mailboxDir := t.TempDir()
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	_, err = SetupWithResult(&SetupConfig{
		Cwd:                 t.TempDir(),
		CustomSessionID:     "session-uds",
		MessagingSocketPath: socketPath,
		MailboxDir:          mailboxDir,
	})
	if err != nil {
		t.Fatalf("SetupWithResult() error = %v", err)
	}

	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("dial uds: %v", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintln(conn, `{"type":"send_message","to":"worker@team","message":"hello","from":"leader"}`); err != nil {
		t.Fatalf("write uds request: %v", err)
	}
	var response map[string]any
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatalf("decode uds response: %v", err)
	}
	if response["ok"] != true {
		t.Fatalf("unexpected uds response: %+v", response)
	}
	data, err := os.ReadFile(filepath.Join(mailboxDir, "worker-team.jsonl"))
	if err != nil {
		t.Fatalf("read mailbox: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("mailbox missing message: %s", data)
	}
}

func TestSetupInitializesSessionMemoryFile(t *testing.T) {
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
		ResetRuntimeStateForTest()
	})
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_HOME", configHome)

	result, err := SetupWithResult(&SetupConfig{
		Cwd:             t.TempDir(),
		CustomSessionID: "session-memory",
	})
	if err != nil {
		t.Fatalf("SetupWithResult() error = %v", err)
	}

	memoryPath := filepath.Join(configHome, "session-memory", "session-memory.md")
	if _, err := os.Stat(memoryPath); err != nil {
		t.Fatalf("expected session memory file: %v", err)
	}
	if !result.SessionMemoryInitialized {
		t.Fatalf("expected session memory to initialize, got %+v", result)
	}
}

func TestSetupBareModeSkipsImplicitMessagingAndTeammateSnapshot(t *testing.T) {
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
		ResetRuntimeStateForTest()
	})
	t.Setenv("CLAUDE_CODE_BARE", "1")
	t.Setenv("AGENT_SWARMS_ENABLED", "true")

	result, err := SetupWithResult(&SetupConfig{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("SetupWithResult() error = %v", err)
	}
	if result.MessagingStarted {
		t.Fatalf("bare mode should skip implicit messaging: %+v", result)
	}
	if result.TeammateSnapshotCaptured {
		t.Fatalf("bare mode should skip teammate snapshot: %+v", result)
	}
	if result.SessionMemoryInitialized {
		t.Fatalf("bare mode should skip session memory: %+v", result)
	}
}

func TestSetupWorktreeSwitchesProjectRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
		ResetRuntimeStateForTest()
	})

	repo := t.TempDir()
	appHome := filepath.Join(t.TempDir(), ".claude-codex")
	t.Setenv("CLAUDE_GO_HOME", appHome)
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial")

	result, err := SetupWithResult(&SetupConfig{
		Cwd:             repo,
		CustomSessionID: "session-worktree",
		WorktreeEnabled: true,
		WorktreeName:    "feature/bootstrap",
	})
	if err != nil {
		t.Fatalf("SetupWithResult() worktree error = %v", err)
	}
	if result.WorktreePath == "" {
		t.Fatalf("expected worktree path, got %+v", result)
	}
	if result.ProjectRoot != result.WorktreePath || result.CurrentCwd != result.WorktreePath {
		t.Fatalf("expected project root/current cwd to switch to worktree, got %+v", result)
	}
	if _, err := os.Stat(filepath.Join(result.WorktreePath, "README.md")); err != nil {
		t.Fatalf("expected worktree checkout: %v", err)
	}
	homeDir, err := appconfig.AppHome()
	if err != nil {
		t.Fatalf("resolve app home: %v", err)
	}
	store, err := storage.NewSessionStorage(homeDir, "session-worktree", result.WorktreePath)
	if err != nil {
		t.Fatalf("create session storage view: %v", err)
	}
	defer store.Close()
	if !transcriptContainsWorktreeState(t, store.GetTranscriptPath()) {
		t.Fatalf("expected worktree-state transcript entry")
	}
}

func TestSetupLogsAnalyticsEvents(t *testing.T) {
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
		ResetRuntimeStateForTest()
	})
	sink := &captureAnalytics{}

	result, err := SetupWithResult(&SetupConfig{
		Cwd:             t.TempDir(),
		CustomSessionID: "session-analytics",
		AnalyticsLogger: sink,
	})
	if err != nil {
		t.Fatalf("SetupWithResult() error = %v", err)
	}
	if !containsString(result.AnalyticsEvents, "tengu_started") || !sink.hasEvent("tengu_started") {
		t.Fatalf("expected tengu_started analytics event, result=%+v sink=%+v", result, sink.events)
	}
}

func TestSetupClearsMissingTerminalBackupState(t *testing.T) {
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
		ResetRuntimeStateForTest()
	})
	home := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", home)
	configPath := filepath.Join(home, "config.json")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"schema_version":3,"appleTerminalSetupInProgress":true,"appleTerminalBackupPath":"/missing/terminal.plist.bak","iterm2SetupInProgress":true,"iterm2BackupPath":"/missing/iterm.plist.bak"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	result, err := SetupWithResult(&SetupConfig{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("SetupWithResult() error = %v", err)
	}
	if result.TerminalBackupRestoreStatus != "no_backup" {
		t.Fatalf("TerminalBackupRestoreStatus = %q, want no_backup", result.TerminalBackupRestoreStatus)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg["appleTerminalSetupInProgress"] == true || cfg["iterm2SetupInProgress"] == true {
		t.Fatalf("expected missing backup markers to be cleared: %s", data)
	}
}

func TestSetupRejectsDangerousSkipPermissionsOutsideSandbox(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("root/sandbox gate is Unix-oriented")
	}
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCwd)
		ResetRuntimeStateForTest()
	})

	_, err = SetupWithResult(&SetupConfig{
		Cwd:                             t.TempDir(),
		AllowDangerouslySkipPermissions: true,
	})
	if err == nil {
		t.Fatal("expected dangerous skip permissions to be rejected outside sandbox")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func transcriptContainsWorktreeState(t *testing.T, path string) bool {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open transcript: %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("decode transcript line: %v", err)
		}
		if entry["type"] == "worktree-state" {
			return true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan transcript: %v", err)
	}
	return false
}

type captureAnalytics struct {
	events []string
}

func (c *captureAnalytics) LogEvent(eventName string, _ map[string]interface{}) {
	c.events = append(c.events, eventName)
}

func (c *captureAnalytics) hasEvent(name string) bool {
	return containsString(c.events, name)
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
