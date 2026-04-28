package tui

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/state"
)

type testRegistry struct {
	commands []Command
	execute  func(context.Context, string, []string) (string, error)
	match    func(string) (string, []string, bool)
}

func (r testRegistry) List() []Command {
	return r.commands
}

func (r testRegistry) Execute(ctx context.Context, name string, args []string) (string, error) {
	return r.execute(ctx, name, args)
}

func (r testRegistry) MatchSkillPrompt(prompt string) (string, []string, bool) {
	if r.match == nil {
		return "", nil, false
	}
	return r.match(prompt)
}

func TestModelThemeToggleStaysInInsertOnlyFlow(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:      "Claude Go",
		WorkingDir: session.WorkingDir,
		Theme:      "dark",
		Session:    session,
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	m := updated.(appModel)
	if m.theme != "light" {
		t.Fatalf("expected light theme after tab, got %s", m.theme)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(appModel)
	if m.lastStatus == "normal mode" || m.lastStatus == "insert mode" {
		t.Fatalf("did not expect vim mode status, got %q", m.lastStatus)
	}
}

func TestEmptyInputPlaceholderRenderedOnce(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:      "Claude Go",
		WorkingDir: session.WorkingDir,
		Theme:      "dark",
		Session:    session,
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})

	view := model.View()
	if got := strings.Count(view, "Describe what to do"); got != 1 {
		t.Fatalf("expected placeholder to render once, got %d in %q", got, view)
	}
}

func TestIdleDisplayStatusDoesNotLeakRunning(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:      "Claude Go",
		WorkingDir: session.WorkingDir,
		Theme:      "dark",
		Session:    session,
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.busy = false
	model.lastStatus = "running"

	if got := model.displayStatus(); got != "idle" {
		t.Fatalf("expected idle display status, got %q", got)
	}
}

func TestRenderTranscriptIncludesRoles(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("hello")
	session.AddAssistantMessage("**world**")
	styles := stylesForTheme("dark")
	output := renderTranscript(session, nil, styles, 80, "")
	if !strings.Contains(output, "User") || !strings.Contains(output, "Assistant") {
		t.Fatalf("unexpected transcript output: %q", output)
	}
}

func TestRenderTranscriptCollapsesMultiToolAssistantTurns(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("inspect project")
	session.AddAssistantMessageWithTools("", []state.ToolCall{{
		ID:    "call-1",
		Name:  "bash",
		Input: []byte(`{"command":"ls"}`),
	}})
	session.AddToolResult("call-1", "bash", []byte(`{"command":"ls"}`), "file1\nfile2")
	session.AddAssistantMessageWithTools("", []state.ToolCall{{
		ID:    "call-2",
		Name:  "bash",
		Input: []byte(`{"command":"pwd"}`),
	}})
	session.AddToolResult("call-2", "bash", []byte(`{"command":"pwd"}`), "/tmp/project")
	session.AddAssistantMessage("done")

	output := renderTranscript(session, nil, stylesForTheme("dark"), 80, "")
	if strings.Count(output, "Assistant") != 1 {
		t.Fatalf("expected collapsed assistant block, got %q", output)
	}
	for _, want := range []string{"🔧 bash: ls", "🔧 bash: pwd", "done"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected collapsed transcript to contain %q, got %q", want, output)
		}
	}
}

func TestModelRendersPromptSuggestionAndAcceptsTab(t *testing.T) {
	session := state.NewSession(t.TempDir())
	suggestionCh := make(chan string, 1)
	suggestionCh <- "Inspect CLAUDE.md before editing nearby files"

	model := newModel(Options{
		Title:              "Claude Go",
		WorkingDir:         session.WorkingDir,
		Theme:              "dark",
		Session:            session,
		PermissionMode:     "default",
		PromptSuggestionCh: suggestionCh,
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})

	updated, cmd := model.Update(promptSuggestionMsg{text: "Inspect CLAUDE.md before editing nearby files"})
	if cmd == nil {
		t.Fatal("expected wait command after prompt suggestion")
	}
	m := updated.(appModel)
	view := m.View()
	if !strings.Contains(view, "Suggestion: Inspect CLAUDE.md before editing nearby files") {
		t.Fatalf("expected prompt suggestion in view, got %q", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(appModel)
	if got := m.input.Value(); got != "Inspect CLAUDE.md before editing nearby files" {
		t.Fatalf("expected tab to accept prompt suggestion, got %q", got)
	}
}

func TestUpAndDownNavigateInputHistory(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.history = []string{"first", "second"}
	model.historyIdx = len(model.history)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	m := updated.(appModel)
	if got := m.input.Value(); got != "second" {
		t.Fatalf("expected up to show last history item, got %q", got)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(appModel)
	if got := m.input.Value(); got != "first" {
		t.Fatalf("expected second up to show previous history item, got %q", got)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(appModel)
	if got := m.input.Value(); got != "second" {
		t.Fatalf("expected down to move forward in history, got %q", got)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(appModel)
	if got := m.input.Value(); got != "" {
		t.Fatalf("expected final down to clear input, got %q", got)
	}
}

func TestUpAndDownPreferSlashSuggestionsOverHistory(t *testing.T) {
	session := state.NewSession(t.TempDir())
	registry := testRegistry{
		commands: []Command{
			{Name: "/help", Description: "help"},
			{Name: "/history", Description: "history"},
		},
		execute: func(context.Context, string, []string) (string, error) { return "", nil },
	}
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Registry:       registry,
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.history = []string{"first", "second"}
	model.historyIdx = len(model.history)
	model.input.SetValue("/")
	model.updateSuggestions()
	model.suggestionIndex = 1

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	m := updated.(appModel)
	if m.suggestionIndex != 0 {
		t.Fatalf("expected up to move suggestion selection, got %d", m.suggestionIndex)
	}
	if got := m.input.Value(); got != "/" {
		t.Fatalf("expected history to stay untouched when suggestions visible, got %q", got)
	}
}

func TestModelSuppressesPromptSuggestionInPlanMode(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "plan",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})

	updated, _ := model.Update(promptSuggestionMsg{text: "Inspect CLAUDE.md before editing nearby files"})
	m := updated.(appModel)
	if strings.Contains(m.View(), "Suggestion:") {
		t.Fatalf("expected plan mode to suppress suggestion, got %q", m.View())
	}
}

func TestModelSidebarShowsContextAndTasks(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("inspect README.md")
	session.AddAssistantMessage("Understood")

	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		AuthStatus:     AuthViewData{Status: "authenticated"},
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.width = 140
	model.output.Width = 80
	model.sidebar.Width = 32
	model.activeTasks["agent"] = taskSnapshot{
		Name:      "agent",
		Status:    "running",
		Progress:  0.4,
		Message:   "Reviewing auth flow",
		UpdatedAt: time.Now(),
	}
	model.refreshViews()

	view := model.View()
	for _, want := range []string{"Context", "Auth: authenticated", "Runtime", "agent [running] 40%"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in view, got %q", want, view)
		}
	}
}

func TestPermissionDialogRendersToolSummary(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.permission = &permissionEnvelope{
		request: permissions.Request{
			ToolName: "bash",
			Level:    permissions.LevelExecute,
			Summary:  "git status",
			Metadata: map[string]string{"command": "git status"},
		},
		reply: make(chan permissionResult, 1),
	}
	view := model.View()
	for _, want := range []string{"Permission Request", "Tool: bash", "Summary: git status"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in view, got %q", want, view)
		}
	}
}

func TestModelTogglesTaskOverlayWithShortcut(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.activeTasks["bash"] = taskSnapshot{
		Name:      "bash",
		Status:    "running",
		Progress:  0.2,
		Message:   "running tests",
		UpdatedAt: time.Now(),
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m := updated.(appModel)
	if m.overlay != overlayTasks {
		t.Fatalf("expected tasks overlay, got %q", m.overlay)
	}
	if !strings.Contains(m.View(), "Runtime Tasks") {
		t.Fatalf("expected runtime tasks overlay in view, got %q", m.View())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(appModel)
	if m.overlay != overlayNone {
		t.Fatalf("expected overlay to close, got %q", m.overlay)
	}
}

func TestModelShowsHelpOverlay(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	m := updated.(appModel)
	view := m.View()
	if !strings.Contains(view, "Help") || !strings.Contains(view, "toggle runtime/tasks overlay") || !strings.Contains(view, "toggle compact overlay") {
		t.Fatalf("expected help overlay in view, got %q", view)
	}
}

func TestTaskOverlaySupportsDetailToggle(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.activeTasks["agent"] = taskSnapshot{
		Name:      "agent",
		Status:    "running",
		Progress:  0.5,
		Message:   "Reviewing auth flow",
		UpdatedAt: time.Now(),
	}
	model.toggleOverlay(overlayTasks, "tasks")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(appModel)
	if !m.taskOverlayDetail {
		t.Fatal("expected task overlay detail mode")
	}
	if !strings.Contains(m.View(), "Detail") || !strings.Contains(m.View(), "Reviewing auth flow") {
		t.Fatalf("expected task detail content, got %q", m.View())
	}
}

func TestPermissionDialogUsesBashSpecificRendering(t *testing.T) {
	view := renderPermissionDialog(permissions.Request{
		ToolName: "bash",
		Level:    permissions.LevelExecute,
		Summary:  "rm -rf build",
		Metadata: map[string]string{
			"command":        "rm -rf build",
			"command_prefix": "rm",
		},
	})
	for _, want := range []string{"Bash Permission Request", "Potentially destructive recursive deletion detected", "Don't ask again for rm commands in this workspace"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in bash permission view, got %q", want, view)
		}
	}
}

func TestPermissionDecisionKeysReturnTypedDecision(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Session:        session,
		PermissionMode: "default",
		Input:          strings.NewReader(""),
		Output:         new(bytes.Buffer),
	})
	reply := make(chan permissionResult, 1)
	model.permission = &permissionEnvelope{
		request: permissions.Request{
			ToolName: "Bash",
			Level:    permissions.LevelExecute,
			Summary:  "npm publish --dry-run",
			Suggestions: []permissions.PermissionUpdate{{
				Type:        permissions.UpdateAddRules,
				Destination: permissions.SourceSession,
				Behavior:    permissions.BehaviorAllow,
				Rules:       []permissions.RuleValue{{ToolName: "Bash", RuleContent: "npm publish:*"}},
			}},
		},
		reply: reply,
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(appModel)
	if model.permission != nil {
		t.Fatal("expected permission dialog to close")
	}
	result := <-reply
	if result.err != nil {
		t.Fatalf("unexpected permission result error: %v", result.err)
	}
	if result.decision.Behavior != permissions.BehaviorAllow || !result.decision.Remember {
		t.Fatalf("expected remembered allow decision, got %+v", result.decision)
	}
	if len(result.decision.Updates) != 1 || result.decision.Updates[0].Rules[0].RuleContent != "npm publish:*" {
		t.Fatalf("expected suggested update to be returned, got %+v", result.decision.Updates)
	}

	view := renderPermissionDialog(permissions.Request{
		ToolName:    "Bash",
		Level:       permissions.LevelExecute,
		Suggestions: result.decision.Updates,
	})
	if !strings.Contains(view, "[a] always allow") {
		t.Fatalf("expected always-allow key hint, got %q", view)
	}
}

func TestTaskOverlaySupportsDetailMode(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.activeTasks["agent"] = taskSnapshot{
		Name:      "agent",
		Status:    "running",
		Progress:  0.6,
		Message:   "reviewing auth flow",
		UpdatedAt: time.Now(),
	}
	model.toggleOverlay(overlayTasks, "tasks")
	view := model.View()
	if !strings.Contains(view, "Use ↑/↓ to select, Enter for detail") {
		t.Fatalf("expected tasks list overlay, got %q", view)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(appModel)
	view = m.View()
	if !strings.Contains(view, "Detail:") || !strings.Contains(view, "reviewing auth flow") {
		t.Fatalf("expected task detail overlay, got %q", view)
	}
}

func TestPermissionDialogRendersBashSpecificCopy(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.permission = &permissionEnvelope{
		request: permissions.Request{
			ToolName: "bash",
			Level:    permissions.LevelExecute,
			Summary:  "git status",
			Metadata: map[string]string{"command": "git status --short"},
		},
		reply: make(chan permissionResult, 1),
	}

	view := model.View()
	for _, want := range []string{"Bash Permission Request", "Command:", "git status --short"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in view, got %q", want, view)
		}
	}
}

func TestModelShowsCompactOverlay(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("hello")
	session.AddAssistantMessage("world")
	session.AddSystemContext("[Previous conversation context was compressed to save tokens]")

	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m := updated.(appModel)
	view := m.View()
	if !strings.Contains(view, "Compact / Summary") || !strings.Contains(view, "Latest marker:") {
		t.Fatalf("expected compact overlay in view, got %q", view)
	}
}

func TestContextOverlayShowsBreakdown(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("inspect")
	session.AddAssistantMessage("ok")
	session.AddToolResult("1", "file_read", nil, "README")
	session.AddSystemContext("hidden system")

	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m := updated.(appModel)
	view := m.View()
	for _, want := range []string{"Context Detail", "Hidden messages: 1", "Tool breakdown:", "- file_read: 1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in view, got %q", want, view)
		}
	}
}

func TestModelSidebarShowsWorkspaceSummary(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("hello")
	session.AddAssistantMessage("world")
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		SkillStats:     SkillStatsViewData{Total: 7, UserInvocable: 5},
		ContextBudget: ContextBudgetViewData{
			Model:                   "claude-sonnet-4",
			ContextWindowTokens:     200000,
			CompressionSoftTokens:   180000,
			CompressionTargetTokens: 120000,
		},
		LoadSandboxView: func() SandboxViewData {
			return SandboxViewData{ExecutionEnv: "host"}
		},
		LoadMCPView: func() MCPViewData {
			return MCPViewData{Servers: []MCPServerViewData{{Name: "ide", Transport: "stdio"}}}
		},
		LoadTeamsView: func() TeamsViewData {
			return TeamsViewData{Teams: []TeamViewData{{Name: "alpha", Source: "swarm"}}}
		},
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.width = 140
	model.output.Width = 80
	model.sidebar.Width = 32
	model.refreshViews()

	view := model.View()
	for _, want := range []string{"Context", "Skills: 7", "Mode: default", "Ctx used:", "MCP/Teams: 1/1", "Survey: idle"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in view, got %q", want, view)
		}
	}
}

func TestTaskOverlayUsesShellSpecificDetailRendering(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.activeTasks["bash"] = taskSnapshot{
		Name:      "bash",
		Status:    "running",
		Progress:  0.5,
		Message:   "git status",
		UpdatedAt: time.Now(),
		Metadata: map[string]string{
			"command": "git status",
			"access":  "read-only",
			"risk":    "git state mutation",
		},
	}
	model.toggleOverlay(overlayTasks, "tasks")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(appModel)
	view := m.View()
	for _, want := range []string{"Shell Task Detail", "Command: git status", "Access: read-only", "Risk: git state mutation"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in detail view, got %q", want, view)
		}
	}
}

func TestPermissionDialogUsesMCPSpecificRendering(t *testing.T) {
	view := renderPermissionDialog(permissions.Request{
		ToolName: "mcp__filesystem__read_file",
		Level:    permissions.LevelExecute,
		Summary:  "filesystem/read_file",
		Metadata: map[string]string{
			"server":   "filesystem",
			"mcp_tool": "read_file",
			"uri":      "file://doc.txt",
		},
	})
	for _, want := range []string{"MCP Permission Request", "Server: filesystem", "Tool: read_file", "URI: file://doc.txt"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in mcp permission view, got %q", want, view)
		}
	}
}

func TestPermissionDialogUsesTeamSpecificRendering(t *testing.T) {
	view := renderPermissionDialog(permissions.Request{
		ToolName: "team_create",
		Level:    permissions.LevelWrite,
		Summary:  "create team alpha",
		Metadata: map[string]string{
			"team_name": "alpha",
			"operation": "create",
		},
	})
	for _, want := range []string{"Team Permission Request", "Team: alpha", "Operation: create"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in team permission view, got %q", want, view)
		}
	}
}

func TestModelShowsMCPOverlayWhenToggled(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		LoadMCPView: func() MCPViewData {
			return MCPViewData{Servers: []MCPServerViewData{{Name: "ide", Transport: "stdio", Target: "uvx mcp-server"}}}
		},
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})

	model.toggleOverlay(overlayMCP, "mcp")
	m := model
	view := m.View()
	for _, want := range []string{"MCP Settings", "- ide [stdio]", "uvx mcp-server"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in mcp overlay, got %q", want, view)
		}
	}
}

func TestModelDoesNotAutoOpenFeedbackSurveyAfterResult(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})

	updated, _ := model.Update(resultMsg{result: engine.Result{
		Output:  "done",
		Session: session,
	}})
	m := updated.(appModel)
	if m.overlay == overlaySurvey {
		t.Fatalf("expected survey overlay to stay hidden after successful result, got %q", m.overlay)
	}
	if strings.Contains(m.View(), "How helpful was the latest response?") {
		t.Fatalf("expected survey prompt to stay hidden, got %q", m.View())
	}
}

func TestSurveyRecordsDigitResponse(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.ensureSurveyOpen()
	model.overlay = overlaySurvey

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	m := updated.(appModel)
	view := m.View()
	for _, want := range []string{"Thanks for the feedback.", "Latest rating: 4 (good)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in survey view, got %q", want, view)
		}
	}
}

func TestRenderSandboxOverlayShowsExecutionSummary(t *testing.T) {
	view := renderSandboxOverlay(SandboxViewData{
		Mode:           "default",
		ExecutionEnv:   "docker",
		WorkingDir:     "/tmp/project",
		ApprovalPolicy: "write and execute actions require approval",
		WritableRoots:  []string{"/tmp/project"},
		Notes:          []string{"partial TS parity"},
	})
	for _, want := range []string{"Sandbox / Permissions", "Execution env: docker", "Working dir: /tmp/project", "partial TS parity"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in sandbox overlay, got %q", want, view)
		}
	}
}

func TestRenderNotificationsPanelShowsLatestTenEntries(t *testing.T) {
	notifications := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		notifications = append(notifications, "notice-"+strconv.Itoa(i))
	}

	view := renderNotificationsPanel(notifications)
	if strings.Contains(view, "- notice-0\n") || strings.Contains(view, "- notice-1\n") {
		t.Fatalf("expected oldest notices to be omitted, got %q", view)
	}
	for i := 2; i < 12; i++ {
		want := "notice-" + strconv.Itoa(i)
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in notices panel, got %q", want, view)
		}
	}
}

func TestContextOverlayShowsSkillStatsAndBudget(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("inspect")
	session.AddAssistantMessage("ok")
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		SkillStats:     SkillStatsViewData{Total: 9, UserInvocable: 6, Dynamic: 2, Conditional: 1},
		ContextBudget: ContextBudgetViewData{
			Model:                   "claude-sonnet-4",
			ContextWindowTokens:     200000,
			CompressionSoftTokens:   180000,
			CompressionTargetTokens: 120000,
		},
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m := updated.(appModel)
	view := m.View()
	for _, want := range []string{"Skills loaded: 9", "User invocable skills: 6", "Permission mode: default", "Model: claude-sonnet-4", "Compression soft limit: 180000", "Remaining before compress:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in context overlay, got %q", want, view)
		}
	}
}

func TestRenderTranscriptTruncatesVerboseToolEchoAfterTwoLines(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("run ls")
	session.AddToolResult("1", "bash", nil, "line1\nline2\nline3\nline4")
	session.AddAssistantMessage("line1\nline2\nline3\nline4")

	output := renderTranscript(session, nil, stylesForTheme("dark"), 80, "")
	for _, want := range []string{"line1", "line2", "... output truncated ..."} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected truncated tool echo to contain %q, got %q", want, output)
		}
	}
	if !strings.Contains(output, "🔧 bash") {
		t.Fatalf("expected truncated tool echo, got %q", output)
	}
	if strings.Contains(output, "line3") || strings.Contains(output, "line4") {
		t.Fatalf("expected long tool echo to be hidden, got %q", output)
	}
}

func TestPushNotificationKeepsMoreThanFiveEntries(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	for i := 0; i < 8; i++ {
		model.pushNotification("notice-" + strconv.Itoa(i))
	}
	if len(model.notifications) != 8 {
		t.Fatalf("expected notifications to retain 8 entries, got %d", len(model.notifications))
	}
}

func TestRefreshViewsClampsSidebarOffsetAfterContentShrink(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.sidebar.Width = 32
	model.sidebar.Height = 6
	for i := 0; i < 30; i++ {
		model.pushNotification("notice-" + strconv.Itoa(i))
	}
	model.refreshViews()
	model.sidebar.LineDown(100)
	model.notifications = []string{"short"}
	model.refreshViews()

	if model.sidebar.PastBottom() {
		t.Fatalf("expected sidebar offset to be clamped, got y=%d", model.sidebar.YOffset)
	}
}

func TestModelInputCharLimitRaisedToTenThousand(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	if model.input.CharLimit != 10000 {
		t.Fatalf("expected char limit 10000, got %d", model.input.CharLimit)
	}
}

func TestSkillPromptExecutionDoesNotAppendGeneratedPromptAsUserMessage(t *testing.T) {
	session := state.NewSession(t.TempDir())
	registry := testRegistry{
		commands: []Command{{Name: "/find-skills"}},
		execute: func(context.Context, string, []string) (string, error) {
			return "__SKILL_PROMPT__internal generated prompt", nil
		},
	}
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Registry:       registry,
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			t.Fatal("expected generated runner to be used")
			return engine.Result{}, nil
		},
		GeneratedRunner: func(ctx context.Context, current *state.Session, prompt string) (engine.Result, error) {
			if prompt != "internal generated prompt" {
				t.Fatalf("unexpected generated prompt %q", prompt)
			}
			current.AddAssistantMessage("skill response")
			return engine.Result{Output: "skill response", Session: current}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.input.SetValue("/find-skills 这个skill的作用是什么")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected generated prompt command")
	}
	msg := cmd()
	updated, _ = updated.(appModel).Update(msg)
	m := updated.(appModel)

	userCount := 0
	for _, message := range m.session.Messages {
		if message.Role == "user" {
			userCount++
		}
	}
	if userCount != 1 {
		t.Fatalf("expected only one visible user message, got %#v", m.session.Messages)
	}
	if got := m.session.LastUserMessage(); got != "/find-skills 这个skill的作用是什么" {
		t.Fatalf("unexpected last user message %q", got)
	}
}

func TestNaturalLanguageAutoRoutesToSkill(t *testing.T) {
	session := state.NewSession(t.TempDir())
	registry := testRegistry{
		execute: func(context.Context, string, []string) (string, error) {
			return "__SKILL_PROMPT__generated skill prompt", nil
		},
		match: func(prompt string) (string, []string, bool) {
			if prompt == "帮我生成图片" {
				return "/shortart-image-generator-openclaw", []string{prompt}, true
			}
			return "", nil, false
		},
	}
	model := newModel(Options{
		Title:          "Claude Codex",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Registry:       registry,
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			t.Fatal("expected generated runner to be used")
			return engine.Result{}, nil
		},
		GeneratedRunner: func(ctx context.Context, current *state.Session, prompt string) (engine.Result, error) {
			if prompt != "generated skill prompt" {
				t.Fatalf("unexpected generated prompt %q", prompt)
			}
			current.AddAssistantMessage("skill executed")
			return engine.Result{Output: "skill executed", Session: current}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.input.SetValue("帮我生成图片")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command for auto-routed skill")
	}
	msg := cmd()
	updated, _ = updated.(appModel).Update(msg)
	m := updated.(appModel)
	if got := m.session.LastUserMessage(); got != "帮我生成图片" {
		t.Fatalf("expected original natural prompt to remain as user message, got %q", got)
	}
	if !strings.Contains(m.View(), "skill executed") {
		t.Fatalf("expected skill execution result in view, got %q", m.View())
	}
}

func TestAltJAndAltKScrollSidebar(t *testing.T) {
	session := state.NewSession(t.TempDir())
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(context.Context, *state.Session, string) (engine.Result, error) {
			return engine.Result{Session: session}, nil
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.sidebar.Width = 32
	model.sidebar.Height = 6
	for i := 0; i < 20; i++ {
		model.pushNotification("notice-" + strconv.Itoa(i))
	}
	model.refreshViews()

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}, Alt: true})
	m := updated.(appModel)
	if m.sidebar.YOffset == 0 {
		t.Fatal("expected alt+j to scroll sidebar")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}, Alt: true})
	m = updated.(appModel)
	if m.sidebar.YOffset != 0 {
		t.Fatalf("expected alt+k to scroll sidebar back up, got %d", m.sidebar.YOffset)
	}
}

func TestEscCancelsActiveRequest(t *testing.T) {
	session := state.NewSession(t.TempDir())
	var cancelled atomic.Bool
	started := make(chan struct{}, 1)
	model := newModel(Options{
		Title:          "Claude Go",
		WorkingDir:     session.WorkingDir,
		Theme:          "dark",
		Session:        session,
		PermissionMode: "default",
		Runner: func(ctx context.Context, current *state.Session, prompt string) (engine.Result, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			cancelled.Store(true)
			return engine.Result{}, ctx.Err()
		},
		Input:  strings.NewReader(""),
		Output: new(bytes.Buffer),
	})
	model.input.SetValue("cancel me")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected request command after enter")
	}
	m := updated.(appModel)
	if !m.busy {
		t.Fatal("expected model to be busy after enter")
	}
	go cmd()
	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected request runner to start")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(appModel)
	cancelledRequestID := m.requestID - 1
	for i := 0; i < 20 && !cancelled.Load(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !cancelled.Load() {
		t.Fatal("expected esc to cancel active request")
	}
	if m.lastStatus != "cancelled" {
		t.Fatalf("expected cancelled status immediately, got %q", m.lastStatus)
	}
	if m.busy {
		t.Fatal("expected busy=false immediately after esc cancel")
	}

	updated, _ = m.Update(resultMsg{err: context.Canceled, requestID: cancelledRequestID})
	m = updated.(appModel)
	if m.busy {
		t.Fatal("expected busy=false after cancelled result")
	}
	if m.lastStatus != "cancelled" {
		t.Fatalf("expected cancelled status, got %q", m.lastStatus)
	}
	if m.errText != "" {
		t.Fatalf("expected no error text after cancellation, got %q", m.errText)
	}
}
