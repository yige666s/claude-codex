package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-codex/internal/app/config"
	"claude-codex/internal/harness/memdir"
	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
	agenttool "claude-codex/internal/harness/tools/agent"
	filetool "claude-codex/internal/harness/tools/file"
)

func TestRuntimeServicesEmitMagicDocSuggestionsOnRead(t *testing.T) {
	filetool.ResetReadListenersForTest()
	t.Cleanup(filetool.ResetReadListenersForTest)

	projectRoot := t.TempDir()
	homeRoot := t.TempDir()

	docPath := filepath.Join(projectRoot, "docs", "MAGIC.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("# MAGIC DOC: Release Checklist\n_Always check this before editing docs._\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestionCh := make(chan string, 1)
	runtime := newRuntimeServices(projectRoot, homeRoot, suggestionCh, nil)
	runtime.warmupMagicDocs()
	unregister := runtime.registerFileReadListener()
	defer unregister()

	tool := filetool.NewReadTool(projectRoot)
	input, err := json.Marshal(map[string]any{"path": "docs/MAGIC.md"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), input); err != nil {
		t.Fatalf("read execute: %v", err)
	}

	select {
	case suggestion := <-suggestionCh:
		if !strings.Contains(suggestion, "Release Checklist") || !strings.Contains(suggestion, "Always check this before editing docs.") {
			t.Fatalf("unexpected suggestion: %q", suggestion)
		}
	default:
		t.Fatal("expected prompt suggestion after reading magic doc")
	}
}

func TestRuntimeServicesAutoDreamWritesDailyLog(t *testing.T) {
	projectRoot := t.TempDir()
	homeRoot := t.TempDir()
	configHome := t.TempDir()

	t.Setenv("CLAUDE_CONFIG_HOME", configHome)

	runtime := newRuntimeServices(projectRoot, homeRoot, nil, nil)
	sessionsDir := filepath.Join(homeRoot, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		session := state.NewSession(projectRoot)
		session.AddUserMessage("task " + string(rune('A'+i)))
		session.AddAssistantMessage("done " + string(rune('A'+i)))
		if _, err := session.Save(homeRoot); err != nil {
			t.Fatalf("save session: %v", err)
		}
	}

	runtime.maybeRunAutoDream()

	dreamPath := memdir.GetAutoMemDailyLogPath(projectRoot, time.Now().Format("2006-01-02"))
	data, err := os.ReadFile(dreamPath)
	if err != nil {
		t.Fatalf("read auto dream output: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# Auto Dream") || !strings.Contains(content, "Consolidation Prompt") {
		t.Fatalf("unexpected auto dream content: %q", content)
	}
}

func TestMakeSubagentRunnerReportsAgentSummary(t *testing.T) {
	projectRoot := t.TempDir()

	var events []toolkit.ProgressEvent
	var eventsMu sync.Mutex
	runner := makeSubagentRunner(
		config.Config{Backend: "simple", MaxTurns: 4},
		permissions.ModeBypass,
		IO{In: strings.NewReader(""), Out: os.Stdout, Err: os.Stderr},
		nil,
		nil,
		func(event toolkit.ProgressEvent) {
			eventsMu.Lock()
			events = append(events, event)
			eventsMu.Unlock()
		},
		nil,
	)

	output, err := runner(context.Background(), agenttool.Request{
		Prompt:       "帮我创建一个 hello.go 文件",
		Description:  "Create hello.go",
		WorkingDir:   projectRoot,
		SubagentType: "executor",
	})
	if err != nil {
		t.Fatalf("run subagent: %v", err)
	}
	if !strings.Contains(output, "wrote") {
		t.Fatalf("unexpected subagent output: %q", output)
	}
	eventsMu.Lock()
	collected := append([]toolkit.ProgressEvent(nil), events...)
	eventsMu.Unlock()

	if len(collected) == 0 {
		t.Fatal("expected progress events from agent summary integration")
	}

	foundStarted := false
	foundCompleted := false
	for _, event := range collected {
		if event.ToolName != "agent" {
			continue
		}
		if event.Status == "started" && strings.Contains(event.Message, "Create hello.go") {
			foundStarted = true
		}
		if event.Status == "completed" {
			foundCompleted = true
		}
	}
	if !foundStarted {
		t.Fatalf("expected agent started summary in events: %#v", collected)
	}
	if !foundCompleted {
		t.Fatalf("expected agent completed summary in events: %#v", collected)
	}
}

func TestPrepareSubagentWorkingDirDefaults(t *testing.T) {
	cwd := t.TempDir()
	dir, cleanup, err := prepareSubagentWorkingDir(context.Background(), agenttool.Request{WorkingDir: cwd})
	if err != nil {
		t.Fatalf("prepare working dir: %v", err)
	}
	defer cleanup()
	if dir != cwd {
		t.Fatalf("unexpected dir: %q", dir)
	}
}
