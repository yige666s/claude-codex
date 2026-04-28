package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"claude-codex/internal/app/config"
	"claude-codex/internal/harness/state"
	agenttool "claude-codex/internal/harness/tools/agent"
	"claude-codex/internal/ui/tui"
)

func TestRootCommandCreatesHelloGoFile(t *testing.T) {
	projectRoot := t.TempDir()
	homeRoot := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", homeRoot)

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	command := NewRootCommandWithIO(IO{
		In:  strings.NewReader(""),
		Out: stdout,
		Err: stderr,
	})
	command.SetArgs([]string{
		"--backend", "simple",
		"--permission-mode", "bypass",
		"--cwd", projectRoot,
		"帮我创建一个 hello.go 文件",
	})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute command: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(projectRoot, "hello.go"))
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}

	if !strings.Contains(string(content), "package main") {
		t.Fatalf("expected generated Go file, got:\n%s", string(content))
	}

	if !strings.Contains(stdout.String(), "wrote") {
		t.Fatalf("expected tool summary in stdout, got %q", stdout.String())
	}

	configPath := filepath.Join(homeRoot, "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file at %s: %v", configPath, err)
	}

	sessionDir := filepath.Join(homeRoot, "sessions")
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		t.Fatalf("expected session directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one saved session, got %d", len(entries))
	}
}

func TestRootCommandStatusFilesPermissionsAndPlugin(t *testing.T) {
	projectRoot := t.TempDir()
	homeRoot := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", homeRoot)
	t.Setenv("CLAUDE_CONFIG_HOME", configHome)

	if err := os.WriteFile(filepath.Join(projectRoot, "tracked.go"), []byte("package tracked\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, projectRoot, "init")
	runGit(t, projectRoot, "config", "user.email", "test@example.com")
	runGit(t, projectRoot, "config", "user.name", "Test User")
	runGit(t, projectRoot, "add", "tracked.go")
	runGit(t, projectRoot, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(projectRoot, "changed.go"), []byte("package changed\n"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}

	pluginRoot := filepath.Join(projectRoot, "plugins", "demo")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	manifest := map[string]any{
		"name":        "demo",
		"version":     "1.0.0",
		"description": "Demo plugin",
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "plugin.json"), data, 0o644); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}

	runCommand := func(args ...string) string {
		t.Helper()
		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)
		command := NewRootCommandWithIO(IO{
			In:  strings.NewReader(""),
			Out: stdout,
			Err: stderr,
		})
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("execute %v: %v\nstderr: %s", args, err, stderr.String())
		}
		return stdout.String()
	}

	output := runCommand("--backend", "simple", "--permission-mode", "bypass", "--cwd", projectRoot, "/status")
	for _, want := range []string{"cwd: " + projectRoot, "backend: simple", "permission_mode: bypass", "git_branch:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected /status output to contain %q, got %q", want, output)
		}
	}

	output = runCommand("--cwd", projectRoot, "/files")
	if !strings.Contains(output, "tracked.go") {
		t.Fatalf("expected /files output to include tracked file, got %q", output)
	}

	output = runCommand("--cwd", projectRoot, "/files", "changed")
	if !strings.Contains(output, "changed.go") {
		t.Fatalf("expected /files changed output to include changed file, got %q", output)
	}

	output = runCommand("--cwd", projectRoot, "/permissions", "add", "allow", "Read")
	if !strings.Contains(output, "added allow permission: Read") {
		t.Fatalf("expected add permission output, got %q", output)
	}
	output = runCommand("--cwd", projectRoot, "/permissions")
	if !strings.Contains(output, "allow:\n  - Read") {
		t.Fatalf("expected permissions list to include Read allow rule, got %q", output)
	}
	output = runCommand("--cwd", projectRoot, "/permissions", "remove", "allow", "Read")
	if !strings.Contains(output, "removed allow permission: Read") {
		t.Fatalf("expected remove permission output, got %q", output)
	}

	output = runCommand("--cwd", projectRoot, "/plugin", "install-local", "demo@local", pluginRoot)
	if !strings.Contains(output, "installed plugin demo@local") {
		t.Fatalf("expected plugin install output, got %q", output)
	}
	output = runCommand("--cwd", projectRoot, "/plugin", "list")
	if !strings.Contains(output, "demo@local") || !strings.Contains(output, "1.0.0") {
		t.Fatalf("expected plugin list to include installed plugin, got %q", output)
	}
	output = runCommand("--cwd", projectRoot, "/plugin", "disable", "demo@local")
	if !strings.Contains(output, "disabled plugin demo@local") {
		t.Fatalf("expected plugin disable output, got %q", output)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func TestRootCommandBranchTagExportAndUsage(t *testing.T) {
	projectRoot := t.TempDir()
	homeRoot := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", homeRoot)
	t.Setenv("CLAUDE_CONFIG_HOME", configHome)

	if err := os.WriteFile(filepath.Join(projectRoot, "tracked.go"), []byte("package tracked\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, projectRoot, "init")
	runGit(t, projectRoot, "config", "user.email", "test@example.com")
	runGit(t, projectRoot, "config", "user.name", "Test User")
	runGit(t, projectRoot, "add", "tracked.go")
	runGit(t, projectRoot, "commit", "-m", "initial")

	session := state.NewSession(projectRoot)
	session.Description = "root command export fixture"
	session.AddUserMessage("hello from the saved session")
	session.AddAssistantMessage("saved session reply")
	if _, err := session.Save(homeRoot); err != nil {
		t.Fatalf("save session: %v", err)
	}

	runCommand := func(args ...string) string {
		t.Helper()
		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)
		command := NewRootCommandWithIO(IO{
			In:  strings.NewReader(""),
			Out: stdout,
			Err: stderr,
		})
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("execute %v: %v\nstderr: %s", args, err, stderr.String())
		}
		return stdout.String()
	}

	output := runCommand("--cwd", projectRoot, "/branch")
	if !strings.Contains(output, "current_branch:") {
		t.Fatalf("expected /branch to show current branch, got %q", output)
	}
	output = runCommand("--cwd", projectRoot, "/branch", "create", "codex-test")
	if !strings.Contains(output, "created branch codex-test") {
		t.Fatalf("expected branch create output, got %q", output)
	}
	output = runCommand("--cwd", projectRoot, "/branch", "list")
	if !strings.Contains(output, "codex-test") {
		t.Fatalf("expected branch list to include created branch, got %q", output)
	}

	output = runCommand("--cwd", projectRoot, "/tag", "add", "important", "latest")
	if !strings.Contains(output, "added tag important") {
		t.Fatalf("expected tag add output, got %q", output)
	}
	output = runCommand("--cwd", projectRoot, "/tag", "list", "latest")
	if !strings.Contains(output, "tags: important") {
		t.Fatalf("expected tag list output, got %q", output)
	}
	output = runCommand("--cwd", projectRoot, "/tag", "remove", "important", "latest")
	if !strings.Contains(output, "removed tag important") {
		t.Fatalf("expected tag remove output, got %q", output)
	}

	output = runCommand("--cwd", projectRoot, "/export", "latest", "markdown")
	if !strings.Contains(output, "# Claude Codex Session") || !strings.Contains(output, "hello from the saved session") {
		t.Fatalf("expected markdown export to include session transcript, got %q", output)
	}
	exportPath := filepath.Join(homeRoot, "exports", "session.json")
	output = runCommand("--cwd", projectRoot, "/export", "latest", "json", exportPath)
	if !strings.Contains(output, "exported session") {
		t.Fatalf("expected export file output, got %q", output)
	}
	exported, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read exported session: %v", err)
	}
	if !strings.Contains(string(exported), session.ID) {
		t.Fatalf("expected exported json to include session id, got %s", exported)
	}

	output = runCommand("--cwd", projectRoot, "/usage")
	if !strings.Contains(output, "sessions: 1") || !strings.Contains(output, "total_tokens:") {
		t.Fatalf("expected usage summary output, got %q", output)
	}
	output = runCommand("--cwd", projectRoot, "/usage", "sessions", "5")
	if !strings.Contains(output, session.ID) || !strings.Contains(output, "tokens=") {
		t.Fatalf("expected usage sessions output, got %q", output)
	}
}

func TestRootCommandConfigMemoryResumeAndCost(t *testing.T) {
	projectRoot := t.TempDir()
	homeRoot := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", homeRoot)

	runCommand := func(args ...string) string {
		t.Helper()
		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)
		command := NewRootCommandWithIO(IO{
			In:  strings.NewReader(""),
			Out: stdout,
			Err: stderr,
		})
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("execute %v: %v", args, err)
		}
		return stdout.String()
	}

	runCommand("--backend", "simple", "--permission-mode", "bypass", "--cwd", projectRoot, "帮我创建一个 hello.go 文件")

	output := runCommand("/config", "set", "backend", "simple")
	if !strings.Contains(output, "updated backend") {
		t.Fatalf("expected config update output, got %q", output)
	}

	output = runCommand("/memory", "append", "remember", "the", "hello.go", "file")
	if !strings.Contains(output, "memory updated") {
		t.Fatalf("expected memory update output, got %q", output)
	}

	output = runCommand("/memory", "show")
	if !strings.Contains(output, "remember the hello.go file") {
		t.Fatalf("expected memory contents, got %q", output)
	}

	output = runCommand("--backend", "simple", "--permission-mode", "bypass", "--cwd", projectRoot, "/resume", "latest", "读取 hello.go")
	if !strings.Contains(output, "package main") {
		t.Fatalf("expected resumed read output, got %q", output)
	}

	output = runCommand("/cost", "latest")
	if !strings.Contains(output, "total_tokens:") {
		t.Fatalf("expected cost output, got %q", output)
	}

	output = runCommand("/doctor")
	if !strings.Contains(output, "claude-codex-home:") {
		t.Fatalf("expected doctor output, got %q", output)
	}

	output = runCommand("/theme", "light")
	if !strings.Contains(output, "theme set to light") {
		t.Fatalf("expected theme output, got %q", output)
	}
}

func TestRootCommandSkillsHelpAndModel(t *testing.T) {
	projectRoot := t.TempDir()
	homeRoot := t.TempDir()
	userHome := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", homeRoot)
	t.Setenv("HOME", userHome)

	skillDir := filepath.Join(projectRoot, ".claude", "skills", "inspect-hello")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	skillContent := `---
name: Inspect Hello
description: Read hello.go and print it
---

读取 hello.go 并输出文件内容。
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "hello.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write hello.go: %v", err)
	}

	runCommand := func(args ...string) string {
		t.Helper()
		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)
		command := NewRootCommandWithIO(IO{
			In:  strings.NewReader(""),
			Out: stdout,
			Err: stderr,
		})
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("execute %v: %v", args, err)
		}
		return stdout.String()
	}

	output := runCommand("--backend", "simple", "--permission-mode", "bypass", "--cwd", projectRoot, "/skills")
	if !strings.Contains(output, "/inspect-hello") {
		t.Fatalf("expected /skills output to include custom skill, got %q", output)
	}

	output = runCommand("--backend", "simple", "--permission-mode", "bypass", "--cwd", projectRoot, "/help")
	if !strings.Contains(output, "/skills") || !strings.Contains(output, "/inspect-hello") || !strings.Contains(output, "/model") {
		t.Fatalf("expected /help to include skills and model, got %q", output)
	}

	output = runCommand("--cwd", projectRoot, "/model", "claude-opus-test")
	if !strings.Contains(output, "updated model to claude-opus-test") {
		t.Fatalf("expected model set output, got %q", output)
	}

	output = runCommand("--cwd", projectRoot, "/model")
	if !strings.Contains(output, "claude-opus-test") {
		t.Fatalf("expected /model to show updated model, got %q", output)
	}
}

func TestRootCommandWithoutArgsStartsTUI(t *testing.T) {
	projectRoot := t.TempDir()
	homeRoot := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", homeRoot)

	called := false
	previous := startTUI
	startTUI = func(options tui.Options) error {
		called = true
		if options.Theme != "dark" {
			t.Fatalf("expected default dark theme, got %s", options.Theme)
		}
		if options.Session == nil {
			t.Fatal("expected session to be provided to TUI")
		}
		return nil
	}
	defer func() {
		startTUI = previous
	}()

	command := NewRootCommandWithIO(IO{
		In:  strings.NewReader(""),
		Out: new(bytes.Buffer),
		Err: new(bytes.Buffer),
	})
	command.SetArgs([]string{"--cwd", projectRoot})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if !called {
		t.Fatalf("expected zero-arg execution to start the TUI")
	}
}

func TestRootCommandMaxTurnsFlagAllowsZeroOverride(t *testing.T) {
	projectRoot := t.TempDir()
	homeRoot := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", homeRoot)

	if err := config.Save(config.Config{
		SchemaVersion:  config.CurrentSchemaVersion,
		Backend:        "simple",
		Provider:       "anthropic",
		Model:          "claude-sonnet-4-5",
		PermissionMode: "bypass",
		Theme:          "dark",
		APIBaseURL:     "https://api.anthropic.com",
		APIKey:         config.DefaultAnthropicAPIKeyPlaceholder,
		TimeoutSeconds: 600,
		MaxTurns:       8,
		SecretStore:    "auto",
		Telemetry:      config.Default().Telemetry,
		OAuth:          config.Default().OAuth,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	called := false
	previous := startTUI
	startTUI = func(options tui.Options) error {
		called = true
		return nil
	}
	defer func() {
		startTUI = previous
	}()

	command := NewRootCommandWithIO(IO{
		In:  strings.NewReader(""),
		Out: new(bytes.Buffer),
		Err: new(bytes.Buffer),
	})
	command.SetArgs([]string{"--cwd", projectRoot, "--max-turns", "0"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if !called {
		t.Fatal("expected TUI to start")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.MaxTurns != 0 {
		t.Fatalf("expected max_turns override to persist 0, got %d", cfg.MaxTurns)
	}
}

func TestBuildSubagentPromptIncludesMetadata(t *testing.T) {
	prompt := buildSubagentPrompt(agenttool.Request{
		Description:  "Review auth flow",
		SubagentType: "code-reviewer",
		Prompt:       "Inspect src/auth for session bugs.",
	})

	wantParts := []string{
		"Task summary: Review auth flow",
		"Requested subagent type: code-reviewer",
		"Inspect src/auth for session bugs.",
	}
	for _, part := range wantParts {
		if !strings.Contains(prompt, part) {
			t.Fatalf("expected prompt to contain %q, got %q", part, prompt)
		}
	}
}

func TestNewPlannerSupportsOpenAIBackend(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	planner, err := newPlanner(config.Config{
		Backend:        "openai",
		Model:          "gpt-4o",
		TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatalf("newPlanner(openai): %v", err)
	}
	if planner == nil {
		t.Fatal("expected openai planner to be created")
	}
}

func TestNewPlannerTreatsAnthropicPlaceholderAsMissingKey(t *testing.T) {
	_, err := newPlanner(config.Config{
		Backend:        "anthropic",
		Model:          "claude-sonnet-4-5",
		APIKey:         config.DefaultAnthropicAPIKeyPlaceholder,
		TimeoutSeconds: 30,
	})
	if err == nil {
		t.Fatal("expected placeholder anthropic api key to be rejected")
	}
}

func TestBuildSubagentPromptWithoutMetadata(t *testing.T) {
	prompt := buildSubagentPrompt(agenttool.Request{Prompt: "Inspect src/auth for session bugs."})
	if prompt != "Inspect src/auth for session bugs." {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestBuildRegistryIncludesDelegationTools(t *testing.T) {
	registry, err := buildRegistry(config.Config{}, t.TempDir(), func(context.Context, agenttool.Request) (string, error) {
		return "ok", nil
	}, nil)
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	for _, name := range []string{"agent", "TaskCreate", "TaskOutput", "TaskStop", "SendMessage"} {
		if _, err := registry.Get(name); err != nil {
			t.Fatalf("expected %s in registry: %v", name, err)
		}
	}
}
