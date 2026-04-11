package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// CommandHandler is the function signature for slash command handlers
type CommandHandler func(context.Context, []string, slashContext) error

// Command represents a registered slash command
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
	Handler     CommandHandler
}

// Registry manages slash command registration
type Registry struct {
	commands map[string]*Command
	aliases  map[string]string // alias -> primary name
}

// NewRegistry creates a new command registry
func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]*Command),
		aliases:  make(map[string]string),
	}
}

// Register adds a command to the registry
func (r *Registry) Register(cmd *Command) error {
	if cmd.Name == "" {
		return fmt.Errorf("command name cannot be empty")
	}
	if !strings.HasPrefix(cmd.Name, "/") {
		cmd.Name = "/" + cmd.Name
	}

	if _, exists := r.commands[cmd.Name]; exists {
		return fmt.Errorf("command %s already registered", cmd.Name)
	}

	r.commands[cmd.Name] = cmd

	// Register aliases
	for _, alias := range cmd.Aliases {
		if !strings.HasPrefix(alias, "/") {
			alias = "/" + alias
		}
		if _, exists := r.aliases[alias]; exists {
			return fmt.Errorf("alias %s already registered", alias)
		}
		r.aliases[alias] = cmd.Name
	}

	return nil
}

// Get retrieves a command by name or alias
func (r *Registry) Get(name string) (*Command, bool) {
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}

	// Check primary name first
	if cmd, ok := r.commands[name]; ok {
		return cmd, true
	}

	// Check aliases
	if primary, ok := r.aliases[name]; ok {
		return r.commands[primary], true
	}

	return nil, false
}

// List returns all registered commands sorted by name
func (r *Registry) List() []*Command {
	commands := make([]*Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		commands = append(commands, cmd)
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands
}

// GenerateHelp generates help text for all commands
func (r *Registry) GenerateHelp() string {
	return generateHelpForCommands(r.List())
}

func generateHelpForCommands(commands []*Command) string {
	var b strings.Builder
	b.WriteString("Available slash commands:\n\n")

	for _, cmd := range commands {
		// Command name and aliases
		names := []string{cmd.Name}
		if len(cmd.Aliases) > 0 {
			names = append(names, cmd.Aliases...)
		}
		b.WriteString(strings.Join(names, ", "))

		// Usage
		if cmd.Usage != "" {
			b.WriteString(" ")
			b.WriteString(cmd.Usage)
		}
		b.WriteString("\n")

		// Description
		if cmd.Description != "" {
			b.WriteString("                   ")
			b.WriteString(cmd.Description)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// Execute runs a command by name with the given arguments
func (r *Registry) Execute(ctx context.Context, name string, args []string, slash slashContext) error {
	cmd, ok := r.Get(name)
	if !ok {
		return fmt.Errorf("unknown slash command %s", name)
	}

	return cmd.Handler(ctx, args, slash)
}
