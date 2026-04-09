package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ding/claude-code/claude-go/internal/harness/engine"
	"github.com/ding/claude-code/claude-go/internal/harness/state"
)

func TestModelThemeToggleAndVimMode(t *testing.T) {
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
	if m.vimMode != NormalMode {
		t.Fatalf("expected normal mode after esc, got %s", m.vimMode)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(appModel)
	if m.vimMode != InsertMode {
		t.Fatalf("expected insert mode after i, got %s", m.vimMode)
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
