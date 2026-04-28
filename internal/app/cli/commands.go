package cli

import (
	"fmt"
	"sort"
	"strings"

	"claude-codex/internal/harness/skills"
)

// CommandType represents the type of a command.
type CommandType string

const (
	CommandTypeBuiltin CommandType = "builtin"
	CommandTypeLocal   CommandType = "local"
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
	Name                        string            `json:"name"`
	Description                 string            `json:"description"`
	Usage                       string            `json:"usage,omitempty"`
	Type                        CommandType       `json:"type"`
	Source                      CommandSource     `json:"source,omitempty"`
	Kind                        CommandKind       `json:"kind,omitempty"`
	LoadedFrom                  string            `json:"loadedFrom,omitempty"`
	Aliases                     []string          `json:"aliases,omitempty"`
	Hidden                      bool              `json:"hidden,omitempty"`
	UserInvocable               bool              `json:"userInvocable,omitempty"`
	HasUserSpecifiedDescription bool              `json:"hasUserSpecifiedDescription,omitempty"`
	WhenToUse                   string            `json:"whenToUse,omitempty"`
	DisableModelInvocation      bool              `json:"disableModelInvocation,omitempty"`
	AllowedTools                []string          `json:"allowedTools,omitempty"`
	Model                       string            `json:"model,omitempty"`
	Handler                     CommandDefHandler `json:"-"`
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
	cmd.Name = normalizeCommandDefName(cmd.Name)
	if cmd.Name == "" {
		return fmt.Errorf("command name cannot be empty")
	}

	if _, exists := r.commands[cmd.Name]; exists {
		return fmt.Errorf("command %s already registered", cmd.Name)
	}

	aliases := normalizeCommandDefAliases(cmd.Aliases)
	for _, alias := range aliases {
		alias = normalizeCommandDefName(alias)
		if alias == "" {
			continue
		}
		if _, exists := r.commands[alias]; exists {
			return fmt.Errorf("alias %s conflicts with registered command", alias)
		}
		if existingCmd, exists := r.aliases[alias]; exists {
			return fmt.Errorf("alias %s already registered for command %s", alias, existingCmd)
		}
	}

	cmd.Aliases = aliases
	r.commands[cmd.Name] = cmd
	for _, alias := range aliases {
		r.aliases[alias] = cmd.Name
	}

	return nil
}

// Find finds a command definition by name or alias.
func (r *CommandDefRegistry) Find(name string) *CommandDef {
	name = normalizeCommandDefName(name)
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

func normalizeCommandDefName(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), "/")
}

// BuildCommandMatrix returns the unified command view used by help, command
// lookup, and model-facing skill command filtering. It mirrors the TypeScript
// command layer's shape: built-in slash commands and prompt-backed skills live
// in the same registry, but keep their type/source metadata for callers that
// need stricter filtering.
func BuildCommandMatrix(slashRegistry *Registry, skillManager *skills.SkillManager, workingDir string) (*CommandDefRegistry, error) {
	matrix := NewCommandDefRegistry()
	if slashRegistry != nil {
		for _, cmd := range slashRegistry.List() {
			if err := matrix.Register(commandDefFromSlashCommand(cmd)); err != nil {
				return nil, err
			}
		}
	}
	if skillManager != nil {
		for _, cmd := range NewSkillCommandRegistry(skillManager, workingDir).List() {
			if matrix.Has(cmd.Name) {
				continue
			}
			def := commandDefFromSkillCommand(cmd, skillManager)
			if err := matrix.Register(def); err != nil {
				return nil, err
			}
		}
	}
	return matrix, nil
}

func commandDefFromSlashCommand(cmd *Command) *CommandDef {
	return &CommandDef{
		Name:        cmd.Name,
		Description: cmd.Description,
		Usage:       cmd.Usage,
		Type:        CommandTypeBuiltin,
		Source:      CommandSourceBuiltin,
		Aliases:     normalizeCommandDefAliases(cmd.Aliases),
	}
}

func commandDefFromSkillCommand(cmd *Command, skillManager *skills.SkillManager) *CommandDef {
	def := &CommandDef{
		Name:          cmd.Name,
		Description:   cmd.Description,
		Usage:         cmd.Usage,
		Type:          CommandTypePrompt,
		Source:        CommandSourceUser,
		Kind:          CommandKindSkill,
		UserInvocable: true,
	}
	if skillManager == nil {
		return def
	}
	skillName := normalizeCommandDefName(cmd.Name)
	skill, ok := skillManager.GetSkill(skillName)
	if !ok {
		if skillName == "skills" {
			def.Type = CommandTypeBuiltin
			def.Source = CommandSourceBuiltin
			def.Kind = ""
		}
		return def
	}
	def.Description = skill.Description
	def.Usage = skill.ArgumentHint
	def.Aliases = normalizeCommandDefAliases(skill.Aliases)
	def.Hidden = skill.IsHidden
	def.Source = commandSourceFromSkillSource(skill.Source)
	def.LoadedFrom = skill.LoadedFrom
	def.HasUserSpecifiedDescription = skill.HasUserSpecifiedDescription
	def.WhenToUse = skill.WhenToUse
	def.DisableModelInvocation = skill.DisableModelInvocation
	def.AllowedTools = append([]string(nil), skill.AllowedTools...)
	def.Model = skill.Model
	def.UserInvocable = skill.UserInvocable
	return def
}

func normalizeCommandDefAliases(aliases []string) []string {
	if len(aliases) == 0 {
		return nil
	}
	out := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		alias = normalizeCommandDefName(alias)
		if alias != "" {
			out = append(out, alias)
		}
	}
	return out
}

func commandSourceFromSkillSource(source skills.SkillSource) CommandSource {
	switch source {
	case skills.SourceBundled:
		return CommandSourceBundled
	case skills.SourcePlugin:
		return CommandSourcePlugin
	case skills.SourceMCP:
		return CommandSourceMCP
	default:
		return CommandSourceUser
	}
}

// GetSkillToolCommands filters the command matrix to prompt commands the model
// may invoke through the Skill tool.
func GetSkillToolCommands(matrix *CommandDefRegistry) []*CommandDef {
	if matrix == nil {
		return nil
	}
	out := make([]*CommandDef, 0)
	for _, cmd := range matrix.List() {
		if !isModelInvocablePromptCommand(cmd) {
			continue
		}
		if isAlwaysListedPromptCommand(cmd) || cmd.HasUserSpecifiedDescription || cmd.WhenToUse != "" {
			out = append(out, cmd)
		}
	}
	return out
}

// GetSlashCommandToolSkills filters the command matrix to prompt-backed skills
// suitable for slash-command skill discovery.
func GetSlashCommandToolSkills(matrix *CommandDefRegistry) []*CommandDef {
	if matrix == nil {
		return nil
	}
	out := make([]*CommandDef, 0)
	for _, cmd := range matrix.List() {
		if cmd.Type != CommandTypePrompt || cmd.Source == CommandSourceBuiltin {
			continue
		}
		if !cmd.HasUserSpecifiedDescription && cmd.WhenToUse == "" {
			continue
		}
		if cmd.LoadedFrom == string(skills.LoadedFromSkills) ||
			cmd.LoadedFrom == string(skills.LoadedFromPlugin) ||
			cmd.LoadedFrom == string(skills.LoadedFromBundled) ||
			cmd.DisableModelInvocation {
			out = append(out, cmd)
		}
	}
	return out
}

func isModelInvocablePromptCommand(cmd *CommandDef) bool {
	return cmd.Type == CommandTypePrompt &&
		!cmd.DisableModelInvocation &&
		cmd.Source != CommandSourceBuiltin
}

func isAlwaysListedPromptCommand(cmd *CommandDef) bool {
	switch cmd.LoadedFrom {
	case string(skills.LoadedFromBundled), string(skills.LoadedFromSkills), string(skills.LoadedFromCommands):
		return true
	default:
		return false
	}
}
