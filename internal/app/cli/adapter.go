package cli

import (
	"bytes"
	"context"

	"github.com/ding/claude-code/claude-go/internal/ui/tui"
)

// RegistryAdapter adapts the CLI Registry to the TUI CommandRegistry interface
type RegistryAdapter struct {
	registry *Registry
	context  slashContext
}

// NewRegistryAdapter creates a new adapter
func NewRegistryAdapter(registry *Registry, context slashContext) *RegistryAdapter {
	return &RegistryAdapter{
		registry: registry,
		context:  context,
	}
}

// List returns all commands as TUI Command structs
func (a *RegistryAdapter) List() []tui.Command {
	cliCommands := a.registry.List()
	tuiCommands := make([]tui.Command, 0, len(cliCommands))

	for _, cmd := range cliCommands {
		tuiCommands = append(tuiCommands, tui.Command{
			Name:        cmd.Name,
			Aliases:     cmd.Aliases,
			Description: cmd.Description,
			Usage:       cmd.Usage,
		})
	}

	return tuiCommands
}

// Execute runs a slash command and captures its output
func (a *RegistryAdapter) Execute(ctx context.Context, name string, args []string) (string, error) {
	// Create a buffer to capture output
	var buf bytes.Buffer

	// Temporarily replace the output stream
	originalOut := a.context.streams.Out
	a.context.streams.Out = &buf
	defer func() {
		a.context.streams.Out = originalOut
	}()

	// Execute the command
	err := a.registry.Execute(ctx, name, args, a.context)

	// Return captured output
	return buf.String(), err
}
