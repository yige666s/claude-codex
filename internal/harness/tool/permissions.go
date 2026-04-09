package tool

import (
	"sync"
)

// PermissionMode defines the permission mode for tool execution.
type PermissionMode string

const (
	PermissionModeDefault PermissionMode = "default"
	PermissionModeAuto    PermissionMode = "auto"
	PermissionModeBypass  PermissionMode = "bypass"
	PermissionModePlan    PermissionMode = "plan"
)

// AdditionalWorkingDirectory represents an additional working directory with permissions.
type AdditionalWorkingDirectory struct {
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

// ToolPermissionRulesBySource maps source names to permission rules.
type ToolPermissionRulesBySource map[string][]PermissionRule

// PermissionRule represents a single permission rule.
type PermissionRule struct {
	Pattern     string `json:"pattern"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

// ToolPermissionContext contains permission-related context for tool execution.
// This is immutable once created (deep immutable in TypeScript).
type ToolPermissionContext struct {
	mu sync.RWMutex

	Mode                              PermissionMode
	AdditionalWorkingDirectories      map[string]AdditionalWorkingDirectory
	AlwaysAllowRules                  ToolPermissionRulesBySource
	AlwaysDenyRules                   ToolPermissionRulesBySource
	AlwaysAskRules                    ToolPermissionRulesBySource
	IsBypassPermissionsModeAvailable  bool
	IsAutoModeAvailable               bool
	StrippedDangerousRules            ToolPermissionRulesBySource
	ShouldAvoidPermissionPrompts      bool
	AwaitAutomatedChecksBeforeDialog  bool
	PrePlanMode                       *PermissionMode
}

// NewToolPermissionContext creates a new ToolPermissionContext with default values.
func NewToolPermissionContext() *ToolPermissionContext {
	return &ToolPermissionContext{
		Mode:                             PermissionModeDefault,
		AdditionalWorkingDirectories:     make(map[string]AdditionalWorkingDirectory),
		AlwaysAllowRules:                 make(ToolPermissionRulesBySource),
		AlwaysDenyRules:                  make(ToolPermissionRulesBySource),
		AlwaysAskRules:                   make(ToolPermissionRulesBySource),
		IsBypassPermissionsModeAvailable: false,
	}
}

// GetEmptyToolPermissionContext returns an empty permission context.
func GetEmptyToolPermissionContext() *ToolPermissionContext {
	return NewToolPermissionContext()
}

// GetMode returns the current permission mode (thread-safe).
func (p *ToolPermissionContext) GetMode() PermissionMode {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Mode
}

// SetMode sets the permission mode (thread-safe).
func (p *ToolPermissionContext) SetMode(mode PermissionMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Mode = mode
}

// GetAdditionalWorkingDirectory returns an additional working directory by path.
func (p *ToolPermissionContext) GetAdditionalWorkingDirectory(path string) (AdditionalWorkingDirectory, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	dir, ok := p.AdditionalWorkingDirectories[path]
	return dir, ok
}

// AddAdditionalWorkingDirectory adds an additional working directory.
func (p *ToolPermissionContext) AddAdditionalWorkingDirectory(path string, dir AdditionalWorkingDirectory) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.AdditionalWorkingDirectories[path] = dir
}

// RemoveAdditionalWorkingDirectory removes an additional working directory.
func (p *ToolPermissionContext) RemoveAdditionalWorkingDirectory(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.AdditionalWorkingDirectories, path)
}

// GetAllAdditionalWorkingDirectories returns a copy of all additional working directories.
func (p *ToolPermissionContext) GetAllAdditionalWorkingDirectories() map[string]AdditionalWorkingDirectory {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[string]AdditionalWorkingDirectory, len(p.AdditionalWorkingDirectories))
	for k, v := range p.AdditionalWorkingDirectories {
		result[k] = v
	}
	return result
}

// GetAlwaysAllowRules returns a copy of the always-allow rules.
func (p *ToolPermissionContext) GetAlwaysAllowRules() ToolPermissionRulesBySource {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.copyRules(p.AlwaysAllowRules)
}

// GetAlwaysDenyRules returns a copy of the always-deny rules.
func (p *ToolPermissionContext) GetAlwaysDenyRules() ToolPermissionRulesBySource {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.copyRules(p.AlwaysDenyRules)
}

// GetAlwaysAskRules returns a copy of the always-ask rules.
func (p *ToolPermissionContext) GetAlwaysAskRules() ToolPermissionRulesBySource {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.copyRules(p.AlwaysAskRules)
}

// AddAlwaysAllowRule adds an always-allow rule for a source.
func (p *ToolPermissionContext) AddAlwaysAllowRule(source string, rule PermissionRule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.AlwaysAllowRules[source] = append(p.AlwaysAllowRules[source], rule)
}

// AddAlwaysDenyRule adds an always-deny rule for a source.
func (p *ToolPermissionContext) AddAlwaysDenyRule(source string, rule PermissionRule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.AlwaysDenyRules[source] = append(p.AlwaysDenyRules[source], rule)
}

// AddAlwaysAskRule adds an always-ask rule for a source.
func (p *ToolPermissionContext) AddAlwaysAskRule(source string, rule PermissionRule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.AlwaysAskRules[source] = append(p.AlwaysAskRules[source], rule)
}

// IsBypassAvailable returns whether bypass permissions mode is available.
func (p *ToolPermissionContext) IsBypassAvailable() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.IsBypassPermissionsModeAvailable
}

// SetBypassAvailable sets whether bypass permissions mode is available.
func (p *ToolPermissionContext) SetBypassAvailable(available bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.IsBypassPermissionsModeAvailable = available
}

// IsAutoAvailable returns whether auto mode is available.
func (p *ToolPermissionContext) IsAutoAvailable() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.IsAutoModeAvailable
}

// SetAutoAvailable sets whether auto mode is available.
func (p *ToolPermissionContext) SetAutoAvailable(available bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.IsAutoModeAvailable = available
}

// ShouldAvoidPrompts returns whether permission prompts should be auto-denied.
func (p *ToolPermissionContext) ShouldAvoidPrompts() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.ShouldAvoidPermissionPrompts
}

// SetShouldAvoidPrompts sets whether permission prompts should be auto-denied.
func (p *ToolPermissionContext) SetShouldAvoidPrompts(avoid bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ShouldAvoidPermissionPrompts = avoid
}

// ShouldAwaitAutomatedChecks returns whether automated checks should be awaited before dialog.
func (p *ToolPermissionContext) ShouldAwaitAutomatedChecks() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.AwaitAutomatedChecksBeforeDialog
}

// SetAwaitAutomatedChecks sets whether automated checks should be awaited before dialog.
func (p *ToolPermissionContext) SetAwaitAutomatedChecks(await bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.AwaitAutomatedChecksBeforeDialog = await
}

// GetPrePlanMode returns the permission mode before plan mode entry.
func (p *ToolPermissionContext) GetPrePlanMode() *PermissionMode {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.PrePlanMode
}

// SetPrePlanMode sets the permission mode before plan mode entry.
func (p *ToolPermissionContext) SetPrePlanMode(mode *PermissionMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.PrePlanMode = mode
}

// Clone creates a deep copy of the ToolPermissionContext.
func (p *ToolPermissionContext) Clone() *ToolPermissionContext {
	p.mu.RLock()
	defer p.mu.RUnlock()

	clone := &ToolPermissionContext{
		Mode:                              p.Mode,
		AdditionalWorkingDirectories:      make(map[string]AdditionalWorkingDirectory, len(p.AdditionalWorkingDirectories)),
		AlwaysAllowRules:                  p.copyRules(p.AlwaysAllowRules),
		AlwaysDenyRules:                   p.copyRules(p.AlwaysDenyRules),
		AlwaysAskRules:                    p.copyRules(p.AlwaysAskRules),
		IsBypassPermissionsModeAvailable:  p.IsBypassPermissionsModeAvailable,
		IsAutoModeAvailable:               p.IsAutoModeAvailable,
		StrippedDangerousRules:            p.copyRules(p.StrippedDangerousRules),
		ShouldAvoidPermissionPrompts:      p.ShouldAvoidPermissionPrompts,
		AwaitAutomatedChecksBeforeDialog:  p.AwaitAutomatedChecksBeforeDialog,
	}

	for k, v := range p.AdditionalWorkingDirectories {
		clone.AdditionalWorkingDirectories[k] = v
	}

	if p.PrePlanMode != nil {
		mode := *p.PrePlanMode
		clone.PrePlanMode = &mode
	}

	return clone
}

// copyRules creates a deep copy of permission rules.
func (p *ToolPermissionContext) copyRules(rules ToolPermissionRulesBySource) ToolPermissionRulesBySource {
	if rules == nil {
		return make(ToolPermissionRulesBySource)
	}
	result := make(ToolPermissionRulesBySource, len(rules))
	for source, ruleList := range rules {
		result[source] = make([]PermissionRule, len(ruleList))
		copy(result[source], ruleList)
	}
	return result
}
