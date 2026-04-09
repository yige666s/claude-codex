package cli

import (
	"bytes"
	"context"

	"github.com/ding/claude-code/claude-go/internal/harness/skills"
	"github.com/ding/claude-code/claude-go/internal/ui/tui"
)

// CombinedRegistryAdapter combines slash commands and skills into a single registry
type CombinedRegistryAdapter struct {
	slashRegistry *Registry
	skillRegistry *SkillCommandRegistry
	context       slashContext
}

// NewCombinedRegistryAdapter creates a new combined adapter
func NewCombinedRegistryAdapter(slashRegistry *Registry, skillManager *skills.SkillManager, workingDir string, context slashContext) *CombinedRegistryAdapter {
	return &CombinedRegistryAdapter{
		slashRegistry: slashRegistry,
		skillRegistry: NewSkillCommandRegistry(skillManager, workingDir),
		context:       context,
	}
}

// List returns all commands (slash commands + skills) as TUI Command structs
func (a *CombinedRegistryAdapter) List() []tui.Command {
	// Get slash commands
	cliCommands := a.slashRegistry.List()
	tuiCommands := make([]tui.Command, 0, len(cliCommands))

	for _, cmd := range cliCommands {
		tuiCommands = append(tuiCommands, tui.Command{
			Name:        cmd.Name,
			Aliases:     cmd.Aliases,
			Description: cmd.Description,
			Usage:       cmd.Usage,
		})
	}

	// Get skill commands
	skillCommands := a.skillRegistry.List()
	for _, cmd := range skillCommands {
		tuiCommands = append(tuiCommands, tui.Command{
			Name:        cmd.Name,
			Description: cmd.Description,
			Usage:       cmd.Usage,
		})
	}

	return tuiCommands
}

// Execute runs a command (slash command or skill) and captures its output
func (a *CombinedRegistryAdapter) Execute(ctx context.Context, name string, args []string) (string, error) {
	// Create a buffer to capture output
	var buf bytes.Buffer

	// Temporarily replace the output stream
	originalOut := a.context.streams.Out
	a.context.streams.Out = &buf
	defer func() {
		a.context.streams.Out = originalOut
	}()

	// Try slash commands first
	if _, ok := a.slashRegistry.Get(name); ok {
		err := a.slashRegistry.Execute(ctx, name, args, a.context)
		return buf.String(), err
	}

	// Try skills
	if _, ok := a.skillRegistry.Get(name); ok {
		err := a.skillRegistry.Execute(ctx, name, args, a.context)
		return buf.String(), err
	}

	// Not found in either registry
	return "", a.slashRegistry.Execute(ctx, name, args, a.context)
}
