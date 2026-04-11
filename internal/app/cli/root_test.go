package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-codex/internal/app/config"
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
