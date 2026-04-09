package tui

import (
	"context"
	"io"

	"github.com/ding/claude-code/claude-go/internal/harness/engine"
	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	"github.com/ding/claude-code/claude-go/internal/harness/state"
	"github.com/ding/claude-code/claude-go/internal/harness/tools"
)

// Command represents a slash command
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
}

// CommandRegistry provides access to registered commands
type CommandRegistry interface {
	List() []Command
	Execute(ctx context.Context, name string, args []string) (output string, err error)
}

// Runner is the non-streaming runner used for non-interactive and fallback cases.
type Runner func(context.Context, *state.Session, string) (engine.Result, error)

// StreamRunner is called when streaming is available. onChunk is invoked for each
// text delta from the model; the final engine.Result is returned when done.
type StreamRunner func(ctx context.Context, session *state.Session, prompt string, onChunk func(string)) (engine.Result, error)

type SaveTheme func(string) error

type Options struct {
	Title            string
	WorkingDir       string
	Theme            string
	Session          *state.Session
	PermissionBroker *PermissionBroker
	Runner           Runner
	StreamRunner     StreamRunner
	SaveTheme        SaveTheme
	Input            io.Reader
	Output           io.Writer
	Err              io.Writer
	Context          context.Context
	Registry         CommandRegistry
	ProgressCh       chan tools.ProgressEvent
}

type VimMode string

const (
	InsertMode VimMode = "INSERT"
	NormalMode VimMode = "NORMAL"
)

type permissionResult struct {
	err error
}

type permissionEnvelope struct {
	request permissions.Request
	reply   chan permissionResult
}
