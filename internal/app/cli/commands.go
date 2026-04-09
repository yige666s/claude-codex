package cli

import (
	"fmt"
	"sort"
	"strings"
)

// CommandType represents the type of a command.
type CommandType string

const (
	CommandTypeBuiltin CommandType = "builtin"
	CommandTypePrompt  CommandType = "prompt"
	CommandTypeMCP     CommandType = "mcp"
)

// CommandSource represents where a command comes from.
type CommandSource string

const (
	CommandSourceBuiltin CommandSource = "builtin"
	CommandSourcePlugin  CommandSource = "plugin"
	CommandSourceMCP     CommandSource = "mcp"
	CommandSourceBundled CommandSource = "bundled"
	CommandSourceUser    CommandSource = "user"
	CommandSourceGlobal  CommandSource = "global"
	CommandSourceProject CommandSource = "project"
)

// CommandKind represents the kind of prompt command.
type CommandKind string

const (
	CommandKindWorkflow CommandKind = "workflow"
	CommandKindSkill    CommandKind = "skill"
	CommandKindPrompt   CommandKind = "prompt"
)

// CommandDef represents a CLI command definition.
type CommandDef struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Type        CommandType   `json:"type"`
	Source      CommandSource `json:"source,omitempty"`
	Kind        CommandKind   `json:"kind,omitempty"`
	Aliases     []string      `json:"aliases,omitempty"`
	Hidden      bool          `json:"hidden,omitempty"`
	Handler     CommandDefHandler `json:"-"`
}

// CommandDefHandler is the function signature for command handlers.
type CommandDefHandler func(args []string) error

// CommandDefRegistry manages all available command definitions.
type CommandDefRegistry struct {
	commands map[string]*CommandDef
	aliases  map[string]string // alias -> command name
}

// NewCommandDefRegistry creates a new command definition registry.
func NewCommandDefRegistry() *CommandDefRegistry {
	return &CommandDefRegistry{
		commands: make(map[string]*CommandDef),
		aliases:  make(map[string]string),
	}
}

// Register registers a new command definition.
func (r *CommandDefRegistry) Register(cmd *CommandDef) error {
	if cmd.Name == "" {
		return fmt.Errorf("command name cannot be empty")
	}

	if _, exists := r.commands[cmd.Name]; exists {
		return fmt.Errorf("command %s already registered", cmd.Name)
	}

	r.commands[cmd.Name] = cmd

	// Register aliases
	for _, alias := range cmd.Aliases {
		if existingCmd, exists := r.aliases[alias]; exists {
			return fmt.Errorf("alias %s already registered for command %s", alias, existingCmd)
		}
		r.aliases[alias] = cmd.Name
	}

	return nil
}

// Find finds a command definition by name or alias.
func (r *CommandDefRegistry) Find(name string) *CommandDef {
	// Try direct lookup
	if cmd, exists := r.commands[name]; exists {
		return cmd
	}

	// Try alias lookup
	if cmdName, exists := r.aliases[name]; exists {
		return r.commands[cmdName]
	}

	return nil
}

// Get gets a command definition by name or alias, returns error if not found.
func (r *CommandDefRegistry) Get(name string) (*CommandDef, error) {
	cmd := r.Find(name)
	if cmd == nil {
		return nil, fmt.Errorf("command %s not found", name)
	}
	return cmd, nil
}

// Has checks if a command definition exists.
func (r *CommandDefRegistry) Has(name string) bool {
	return r.Find(name) != nil
}

// List returns all registered command definitions.
func (r *CommandDefRegistry) List() []*CommandDef {
	commands := make([]*CommandDef, 0, len(r.commands))
	for _, cmd := range r.commands {
		commands = append(commands, cmd)
	}

	// Sort by name
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands
}

// ListVisible returns all non-hidden command definitions.
func (r *CommandDefRegistry) ListVisible() []*CommandDef {
	commands := r.List()
	visible := make([]*CommandDef, 0, len(commands))
	for _, cmd := range commands {
		if !cmd.Hidden {
			visible = append(visible, cmd)
		}
	}
	return visible
}

// ListBySource returns command definitions from a specific source.
func (r *CommandDefRegistry) ListBySource(source CommandSource) []*CommandDef {
	commands := make([]*CommandDef, 0)
	for _, cmd := range r.commands {
		if cmd.Source == source {
			commands = append(commands, cmd)
		}
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands
}

// ListByType returns command definitions of a specific type.
func (r *CommandDefRegistry) ListByType(cmdType CommandType) []*CommandDef {
	commands := make([]*CommandDef, 0)
	for _, cmd := range r.commands {
		if cmd.Type == cmdType {
			commands = append(commands, cmd)
		}
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands
}

// Unregister removes a command definition from the registry.
func (r *CommandDefRegistry) Unregister(name string) error {
	cmd := r.Find(name)
	if cmd == nil {
		return fmt.Errorf("command %s not found", name)
	}

	// Remove aliases
	for _, alias := range cmd.Aliases {
		delete(r.aliases, alias)
	}

	delete(r.commands, cmd.Name)
	return nil
}

// FormatDescriptionWithSource formats a command definition's description with its source annotation.
func FormatDescriptionWithSource(cmd *CommandDef) string {
	if cmd.Type != CommandTypePrompt {
		return cmd.Description
	}

	if cmd.Kind == CommandKindWorkflow {
		return fmt.Sprintf("%s (workflow)", cmd.Description)
	}

	switch cmd.Source {
	case CommandSourcePlugin:
		return fmt.Sprintf("%s (plugin)", cmd.Description)
	case CommandSourceBuiltin, CommandSourceMCP:
		return cmd.Description
	case CommandSourceBundled:
		return fmt.Sprintf("%s (bundled)", cmd.Description)
	default:
		return fmt.Sprintf("%s (%s)", cmd.Description, getSettingSourceName(cmd.Source))
	}
}

// getSettingSourceName returns a human-readable name for a command source.
func getSettingSourceName(source CommandSource) string {
	switch source {
	case CommandSourceUser:
		return "user"
	case CommandSourceGlobal:
		return "global"
	case CommandSourceProject:
		return "project"
	default:
		return string(source)
	}
}

// GetCommandName returns the display name for a command definition.
func GetCommandName(cmd *CommandDef) string {
	if cmd.Name != "" {
		return cmd.Name
	}
	return "unnamed"
}

// ParseCommandLine parses a command line into command name and arguments.
func ParseCommandLine(line string) (string, []string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}

	// Remove leading slash if present
	if strings.HasPrefix(line, "/") {
		line = line[1:]
	}

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "", nil
	}

	return parts[0], parts[1:]
}
