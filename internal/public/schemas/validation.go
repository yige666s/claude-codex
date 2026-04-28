package schemas

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Validator provides validation functionality
type Validator struct {
	errors []ValidationError
}

// NewValidator creates a new validator
func NewValidator() *Validator {
	return &Validator{
		errors: make([]ValidationError, 0),
	}
}

// AddError adds a validation error
func (v *Validator) AddError(path, message string) {
	v.errors = append(v.errors, ValidationError{
		Path:    path,
		Message: message,
	})
}

// AddErrorWithSuggestion adds a validation error with suggestion
func (v *Validator) AddErrorWithSuggestion(path, message, suggestion string) {
	v.errors = append(v.errors, ValidationError{
		Path:       path,
		Message:    message,
		Suggestion: suggestion,
	})
}

// HasErrors returns true if there are validation errors
func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

// Errors returns all validation errors
func (v *Validator) Errors() []ValidationError {
	return v.errors
}

// Result returns the validation result
func (v *Validator) Result() ValidationResult {
	return ValidationResult{
		Valid:  !v.HasErrors(),
		Errors: v.errors,
	}
}

// ValidateSettings validates settings configuration
func ValidateSettings(settings *Settings) ValidationResult {
	validator := NewValidator()

	// Validate permissions
	if settings.Permissions != nil {
		validatePermissions(settings.Permissions, validator)
	}

	// Validate hooks
	if settings.Hooks != nil {
		validateHooks(settings.Hooks, validator)
	}

	// Validate MCP servers
	if settings.MCPServers != nil {
		validateMCPServers(settings.MCPServers, validator)
	}

	// Validate marketplaces
	if settings.Marketplaces != nil {
		validateMarketplaces(settings.Marketplaces, validator)
	}

	// Validate sandbox
	if settings.Sandbox != nil {
		validateSandbox(settings.Sandbox, validator)
	}

	// Validate keybindings
	if settings.Keybindings != nil {
		validateKeybindings(settings.Keybindings, validator)
	}

	return validator.Result()
}

// validatePermissions validates permission configuration
func validatePermissions(perms *Permissions, validator *Validator) {
	// Validate allow rules
	for i, rule := range perms.Allow {
		if err := ValidatePermissionRule(string(rule)); err != nil {
			validator.AddError(
				fmt.Sprintf("permissions.allow[%d]", i),
				err.Error(),
			)
		}
	}

	// Validate deny rules
	for i, rule := range perms.Deny {
		if err := ValidatePermissionRule(string(rule)); err != nil {
			validator.AddError(
				fmt.Sprintf("permissions.deny[%d]", i),
				err.Error(),
			)
		}
	}

	// Validate ask rules
	for i, rule := range perms.Ask {
		if err := ValidatePermissionRule(string(rule)); err != nil {
			validator.AddError(
				fmt.Sprintf("permissions.ask[%d]", i),
				err.Error(),
			)
		}
	}

	// Validate default mode
	if perms.DefaultMode != "" {
		validModes := []PermissionMode{
			PermissionModeDefault,
			PermissionModePlan,
			PermissionModeAcceptEdits,
			PermissionModeDontAsk,
			PermissionModeAsk,
			PermissionModeAllow,
			PermissionModeAuto,
			PermissionModeYolo,
			PermissionModeBypass,
		}
		valid := false
		for _, mode := range validModes {
			if perms.DefaultMode == mode {
				valid = true
				break
			}
		}
		if !valid {
			validator.AddError(
				"permissions.defaultMode",
				fmt.Sprintf("invalid permission mode: %s", perms.DefaultMode),
			)
		}
	}

	if perms.DisableBypassPermissionsMode != "" && perms.DisableBypassPermissionsMode != "disable" {
		validator.AddError("permissions.disableBypassPermissionsMode", "must be \"disable\"")
	}
	if perms.DisableAutoMode != "" && perms.DisableAutoMode != "disable" {
		validator.AddError("permissions.disableAutoMode", "must be \"disable\"")
	}
}

// validateHooks validates hook configuration
func validateHooks(hooks map[HookEvent][]HookMatcher, validator *Validator) {
	for event, matchers := range hooks {
		for i, matcher := range matchers {
			path := fmt.Sprintf("hooks.%s[%d]", event, i)

			if matcher.Hook == nil {
				validator.AddError(path, "hook is required")
				continue
			}

			if err := matcher.Hook.Validate(); err != nil {
				validator.AddError(path, err.Error())
			}

			// Validate if condition
			if ifCond := matcher.Hook.GetIf(); ifCond != nil {
				if err := ValidatePermissionRule(*ifCond); err != nil {
					validator.AddError(
						path+".if",
						"invalid if condition: "+err.Error(),
					)
				}
			}
		}
	}
}

// validateMCPServers validates MCP server configuration
func validateMCPServers(servers map[string]any, validator *Validator) {
	for name, config := range servers {
		path := fmt.Sprintf("mcpServers.%s", name)

		if config == nil {
			validator.AddError(path, "server config is required")
			continue
		}
	}
}

// validateMarketplaces validates marketplace configuration
func validateMarketplaces(marketplaces map[string]MarketplaceSource, validator *Validator) {
	officialNames := []string{"anthropic", "claude", "claude-code"}

	for name, source := range marketplaces {
		path := fmt.Sprintf("marketplaces.%s", name)

		// Check for official marketplace name protection
		for _, official := range officialNames {
			if strings.EqualFold(name, official) {
				// Must be from anthropics GitHub org
				if source.Type != MarketplaceSourceGitHub ||
					!strings.EqualFold(source.Owner, "anthropics") {
					validator.AddError(
						path,
						fmt.Sprintf("marketplace name '%s' is reserved for official sources", name),
					)
				}
			}
		}

		// Validate source type
		switch source.Type {
		case MarketplaceSourceGitHub:
			if source.Owner == "" || source.Repo == "" {
				validator.AddError(path, "owner and repo are required for github source")
			}
		case MarketplaceSourceGit:
			if source.URL == "" {
				validator.AddError(path, "url is required for git source")
			}
		case MarketplaceSourceLocal:
			if source.Path == "" {
				validator.AddError(path, "path is required for local source")
			}
		default:
			validator.AddError(path, fmt.Sprintf("invalid source type: %s", source.Type))
		}

		// Check for non-ASCII characters (homograph attack prevention)
		if !isASCII(name) {
			validator.AddError(
				path,
				"marketplace name must contain only ASCII characters",
			)
		}
	}
}

// validateSandbox validates sandbox configuration
func validateSandbox(sandbox *SandboxSettings, validator *Validator) {
	// Validate network config
	if sandbox.Network != nil {
		if sandbox.Network.HTTPProxyPort != nil {
			port := *sandbox.Network.HTTPProxyPort
			if port < 1 || port > 65535 {
				validator.AddError(
					"sandbox.network.httpProxyPort",
					"port must be between 1 and 65535",
				)
			}
		}
	}

	// Validate filesystem config
	if sandbox.Filesystem != nil {
		// Check for conflicting rules
		for _, allowPath := range sandbox.Filesystem.AllowWrite {
			for _, denyPath := range sandbox.Filesystem.DenyWrite {
				if allowPath == denyPath {
					validator.AddError(
						"sandbox.filesystem",
						fmt.Sprintf("path '%s' is both allowed and denied for write", allowPath),
					)
				}
			}
		}
	}
}

// validateKeybindings validates keybinding configuration
func validateKeybindings(keybindings []Keybinding, validator *Validator) {
	seen := make(map[string]bool)

	for i, kb := range keybindings {
		path := fmt.Sprintf("keybindings[%d]", i)

		if kb.Context == "" {
			validator.AddError(path+".context", "context is required")
		}

		if kb.Key == "" {
			validator.AddError(path+".key", "key is required")
		}

		if kb.Action == "" {
			validator.AddError(path+".action", "action is required")
		}

		// Check for duplicate keybindings
		key := string(kb.Context) + ":" + kb.Key
		if seen[key] {
			validator.AddError(
				path,
				fmt.Sprintf("duplicate keybinding for %s in context %s", kb.Key, kb.Context),
			)
		}
		seen[key] = true
	}
}

// ValidateSettingsJSON validates settings from JSON bytes
func ValidateSettingsJSON(data []byte) ValidationResult {
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return ValidationResult{
			Valid: false,
			Errors: []ValidationError{
				{
					Path:    "root",
					Message: "invalid JSON: " + err.Error(),
				},
			},
		}
	}

	return ValidateSettings(&settings)
}

// FormatValidationErrors formats validation errors for display
func FormatValidationErrors(errors []ValidationError) string {
	if len(errors) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d validation error(s):\n\n", len(errors)))

	for i, err := range errors {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, err.Error()))
		if err.DocLink != "" {
			sb.WriteString(fmt.Sprintf("   See: %s\n", err.DocLink))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// isASCII checks if a string contains only ASCII characters
func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
