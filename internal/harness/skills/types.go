package skills

import (
	"time"

	"claude-codex/internal/harness/websandbox"
)

// SkillSource represents where a skill was loaded from
type SkillSource string

const (
	SourceBundled SkillSource = "bundled" // Built-in skills
	SourceFile    SkillSource = "file"    // User/project skills from filesystem
	SourceMCP     SkillSource = "mcp"     // Skills from MCP servers
	SourcePlugin  SkillSource = "plugin"  // Skills from plugins
	SourceManaged SkillSource = "managed" // Skills from managed policy
)

// ExecutionContext defines how a skill should be executed
type ExecutionContext string

const (
	ContextInline ExecutionContext = "inline" // Execute in current context
	ContextFork   ExecutionContext = "fork"   // Execute in forked agent
)

type FrontmatterShell string

const (
	ShellBash       FrontmatterShell = "bash"
	ShellPowerShell FrontmatterShell = "powershell"
)

// SkillDefinition represents a skill that can be invoked
type SkillDefinition struct {
	// Core identification
	Name        string   // Skill name (used for invocation)
	DisplayName string   // Optional display name
	Aliases     []string // Alternative names

	// Documentation
	Description                 string // Skill description
	HasUserSpecifiedDescription bool   // Whether description was explicitly provided
	WhenToUse                   string // When to use this skill
	ArgumentHint                string // Hint for arguments

	// Execution
	AllowedTools           []string         // Tools this skill can use
	Model                  string           // Model to use (empty = inherit)
	DisableModelInvocation bool             // Skip model invocation
	ExecutionContext       ExecutionContext // inline or fork
	Agent                  string           // Agent type for fork context
	Effort                 *int             // Effort level (1-5 or custom)
	Shell                  FrontmatterShell // Optional shell for !-block execution
	AllowedEnv             []string         // Environment variables allowed for sandboxed execution
	PrimaryEnv             string           // Primary environment variable for the skill

	// Visibility and permissions
	UserInvocable bool        // Can be invoked by user
	IsHidden      bool        // Hidden from autocomplete
	Source        SkillSource // Where skill was loaded from
	LoadedFrom    string      // Specific source path

	// Content
	Content       string            // Markdown content (loaded lazily)
	ContentLength int               // Content length in bytes
	ArgumentNames []string          // Named arguments
	Files         map[string]string // Reference files (for bundled skills)

	// Metadata
	Version      string    // Skill version
	SkillRoot    string    // Base directory for skill files
	Paths        []string  // Path patterns for conditional activation
	LoadedAt     time.Time // When skill was loaded
	FileIdentity string    // Canonical file path (for deduplication)

	// Hooks and configuration
	Hooks map[string]interface{} // Hook settings

	// Prompt generation
	GetPrompt PromptGenerator // Function to generate prompt
}

// PromptGenerator generates prompt content for a skill
type PromptGenerator func(args string, context *SkillContext) ([]ContentBlock, error)

// SkillContext provides context for skill execution
type SkillContext struct {
	SessionID    string
	WorkingDir   string
	Environment  map[string]string
	WebSandbox   *websandbox.Runtime
	State        interface{} // Application state
	ToolRegistry interface{} // Tool registry
}

// ContentBlock represents a block of content in a prompt
type ContentBlock struct {
	Type string      // "text", "image", etc.
	Text string      // Text content
	Data interface{} // Additional data
}

// SkillRegistry manages skill registration and lookup
type SkillRegistry struct {
	skills  map[string]*SkillDefinition // name -> skill
	aliases map[string]string           // alias -> name
}

// NewSkillRegistry creates a new skill registry
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills:  make(map[string]*SkillDefinition),
		aliases: make(map[string]string),
	}
}

// Register adds a skill to the registry
func (r *SkillRegistry) Register(skill *SkillDefinition) error {
	if skill.Name == "" {
		return ErrInvalidSkillName
	}

	// Check for name conflicts
	if _, exists := r.skills[skill.Name]; exists {
		return ErrSkillAlreadyExists
	}

	// Register skill
	r.skills[skill.Name] = skill

	// Register aliases
	for _, alias := range skill.Aliases {
		if alias != "" {
			r.aliases[alias] = skill.Name
		}
	}

	return nil
}

// Get retrieves a skill by name or alias
func (r *SkillRegistry) Get(nameOrAlias string) (*SkillDefinition, bool) {
	// Try direct lookup
	if skill, ok := r.skills[nameOrAlias]; ok {
		return skill, true
	}

	// Try alias lookup
	if name, ok := r.aliases[nameOrAlias]; ok {
		return r.skills[name], true
	}

	return nil, false
}

// List returns all registered skills
func (r *SkillRegistry) List() []*SkillDefinition {
	skills := make([]*SkillDefinition, 0, len(r.skills))
	for _, skill := range r.skills {
		skills = append(skills, skill)
	}
	return skills
}

// ListUserInvocable returns skills that can be invoked by users
func (r *SkillRegistry) ListUserInvocable() []*SkillDefinition {
	skills := make([]*SkillDefinition, 0)
	for _, skill := range r.skills {
		if skill.UserInvocable && !skill.IsHidden {
			skills = append(skills, skill)
		}
	}
	return skills
}

// Remove removes a skill from the registry
func (r *SkillRegistry) Remove(name string) bool {
	skill, ok := r.skills[name]
	if !ok {
		return false
	}

	// Remove aliases
	for _, alias := range skill.Aliases {
		delete(r.aliases, alias)
	}

	// Remove skill
	delete(r.skills, name)
	return true
}

// Clear removes all skills from the registry
func (r *SkillRegistry) Clear() {
	r.skills = make(map[string]*SkillDefinition)
	r.aliases = make(map[string]string)
}

// Count returns the number of registered skills
func (r *SkillRegistry) Count() int {
	return len(r.skills)
}

// Errors
var (
	ErrInvalidSkillName    = &SkillError{Message: "invalid skill name"}
	ErrSkillAlreadyExists  = &SkillError{Message: "skill already exists"}
	ErrSkillNotFound       = &SkillError{Message: "skill not found"}
	ErrInvalidSkillContent = &SkillError{Message: "invalid skill content"}
	ErrSkillLoadFailed     = &SkillError{Message: "failed to load skill"}
)

// SkillError represents a skill-related error
type SkillError struct {
	Message string
	Cause   error
}

func (e *SkillError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *SkillError) Unwrap() error {
	return e.Cause
}
