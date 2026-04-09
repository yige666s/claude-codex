package permissions

// Behavior values for permission decisions.
type Behavior string

const (
	BehaviorAllow       Behavior = "allow"
	BehaviorDeny        Behavior = "deny"
	BehaviorAsk         Behavior = "ask"
	BehaviorPassthrough Behavior = "passthrough"
)

// RuleSource identifies where a permission rule was defined.
type RuleSource string

const (
	SourceUserSettings    RuleSource = "userSettings"
	SourceProjectSettings RuleSource = "projectSettings"
	SourceLocalSettings   RuleSource = "localSettings"
	SourceFlagSettings    RuleSource = "flagSettings"
	SourcePolicySettings  RuleSource = "policySettings"
	SourceCLIArg          RuleSource = "cliArg"
	SourceCommand         RuleSource = "command"
	SourceSession         RuleSource = "session"
)

// RuleValue is the parsed representation of a permission rule string like "Bash(npm:*)".
type RuleValue struct {
	ToolName    string
	RuleContent string // empty = tool-wide rule
}

// Rule is a fully typed permission rule with source and behavior.
type Rule struct {
	Source   RuleSource
	Behavior Behavior
	Value    RuleValue
}

// DecisionReasonType identifies the kind of reason for a permission decision.
type DecisionReasonType string

const (
	ReasonRule              DecisionReasonType = "rule"
	ReasonMode              DecisionReasonType = "mode"
	ReasonSubcommandResults DecisionReasonType = "subcommandResults"
	ReasonHook              DecisionReasonType = "hook"
	ReasonAsyncAgent        DecisionReasonType = "asyncAgent"
	ReasonSandboxOverride   DecisionReasonType = "sandboxOverride"
	ReasonClassifier        DecisionReasonType = "classifier"
	ReasonWorkingDir        DecisionReasonType = "workingDir"
	ReasonSafetyCheck       DecisionReasonType = "safetyCheck"
	ReasonOther             DecisionReasonType = "other"
)

// DecisionReason explains why a permission decision was made.
type DecisionReason struct {
	Type               DecisionReasonType
	Rule               *Rule
	PermissionMode     Mode
	HookName           string
	HookSource         string
	Reason             string
	ClassifierApprovable bool
}

// PermissionResult is the result of a permission check.
type PermissionResult struct {
	Behavior Behavior
	Message  string
	// For ask/passthrough: suggested rule updates
	Suggestions []PermissionUpdate
	// For allow: the reason (optional)
	DecisionReason *DecisionReason
	// Set when the ask was triggered by a bash misparsing security check
	IsBashSecurityCheckForMisparsing bool
	// Blocked path (for path constraint violations)
	BlockedPath string
}

func Allow() PermissionResult                            { return PermissionResult{Behavior: BehaviorAllow} }
func Deny(msg string) PermissionResult                  { return PermissionResult{Behavior: BehaviorDeny, Message: msg} }
func Ask(msg string) PermissionResult                   { return PermissionResult{Behavior: BehaviorAsk, Message: msg} }
func Passthrough(msg string) PermissionResult           { return PermissionResult{Behavior: BehaviorPassthrough, Message: msg} }
func AskMisparsing(msg string) PermissionResult {
	return PermissionResult{Behavior: BehaviorAsk, Message: msg, IsBashSecurityCheckForMisparsing: true}
}

// RulesBySource maps RuleSource to a slice of rule strings (e.g. "Bash(npm:*)").
type RulesBySource map[RuleSource][]string

// PermissionUpdateType is the kind of mutation to apply to a ToolContext.
type PermissionUpdateType string

const (
	UpdateAddRules         PermissionUpdateType = "addRules"
	UpdateReplaceRules     PermissionUpdateType = "replaceRules"
	UpdateRemoveRules      PermissionUpdateType = "removeRules"
	UpdateSetMode          PermissionUpdateType = "setMode"
	UpdateAddDirectories   PermissionUpdateType = "addDirectories"
	UpdateRemoveDirectories PermissionUpdateType = "removeDirectories"
)

// PermissionUpdate describes a change to apply to a ToolContext.
type PermissionUpdate struct {
	Type        PermissionUpdateType
	Destination RuleSource
	Rules       []RuleValue
	Behavior    Behavior
	Mode        Mode
	Directories []string
}

// ToolContext holds the runtime permission rules and mode for a session.
type ToolContext struct {
	PermissionMode    Mode
	AlwaysAllowRules  RulesBySource
	AlwaysDenyRules   RulesBySource
	AlwaysAskRules    RulesBySource
	WorkingDirectories map[string]string // path → source
}

func NewToolContext(mode Mode) *ToolContext {
	return &ToolContext{
		PermissionMode:    mode,
		AlwaysAllowRules:  make(RulesBySource),
		AlwaysDenyRules:   make(RulesBySource),
		AlwaysAskRules:    make(RulesBySource),
		WorkingDirectories: make(map[string]string),
	}
}

// AllAllowRules returns all allow rule strings across all sources.
func (c *ToolContext) AllAllowRules() []string { return flattenRules(c.AlwaysAllowRules) }

// AllDenyRules returns all deny rule strings across all sources.
func (c *ToolContext) AllDenyRules() []string { return flattenRules(c.AlwaysDenyRules) }

// AllAskRules returns all ask rule strings across all sources.
func (c *ToolContext) AllAskRules() []string { return flattenRules(c.AlwaysAskRules) }

func flattenRules(m RulesBySource) []string {
	var out []string
	for _, rules := range m {
		out = append(out, rules...)
	}
	return out
}

// ApplyUpdate applies a PermissionUpdate to the context, returning a new context.
func (c *ToolContext) ApplyUpdate(u PermissionUpdate) *ToolContext {
	next := &ToolContext{
		PermissionMode:    c.PermissionMode,
		AlwaysAllowRules:  cloneRulesBySource(c.AlwaysAllowRules),
		AlwaysDenyRules:   cloneRulesBySource(c.AlwaysDenyRules),
		AlwaysAskRules:    cloneRulesBySource(c.AlwaysAskRules),
		WorkingDirectories: cloneStringMap(c.WorkingDirectories),
	}

	target := func() RulesBySource {
		switch u.Behavior {
		case BehaviorAllow:
			return next.AlwaysAllowRules
		case BehaviorDeny:
			return next.AlwaysDenyRules
		default:
			return next.AlwaysAskRules
		}
	}

	ruleStrings := make([]string, len(u.Rules))
	for i, r := range u.Rules {
		ruleStrings[i] = RuleValueToString(r)
	}

	switch u.Type {
	case UpdateSetMode:
		next.PermissionMode = u.Mode
	case UpdateAddRules:
		t := target()
		t[u.Destination] = append(t[u.Destination], ruleStrings...)
	case UpdateReplaceRules:
		t := target()
		t[u.Destination] = ruleStrings
	case UpdateRemoveRules:
		t := target()
		existing := t[u.Destination]
		filtered := existing[:0]
		remove := make(map[string]bool, len(ruleStrings))
		for _, r := range ruleStrings {
			remove[r] = true
		}
		for _, r := range existing {
			if !remove[r] {
				filtered = append(filtered, r)
			}
		}
		t[u.Destination] = filtered
	case UpdateAddDirectories:
		for _, d := range u.Directories {
			next.WorkingDirectories[d] = string(u.Destination)
		}
	case UpdateRemoveDirectories:
		for _, d := range u.Directories {
			delete(next.WorkingDirectories, d)
		}
	}

	return next
}

func cloneRulesBySource(m RulesBySource) RulesBySource {
	out := make(RulesBySource, len(m))
	for k, v := range m {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
