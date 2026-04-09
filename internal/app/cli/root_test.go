package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/ui/tui"
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
	if !strings.Contains(output, "claude-go-home:") {
		t.Fatalf("expected doctor output, got %q", output)
	}

	output = runCommand("/theme", "light")
	if !strings.Contains(output, "theme set to light") {
		t.Fatalf("expected theme output, got %q", output)
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
