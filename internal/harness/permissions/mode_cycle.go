package permissions

// ModeCycleOptions describes which optional modes may be selected by cycling.
type ModeCycleOptions struct {
	BypassAvailable bool
	AutoAvailable   bool
}

// GetNextPermissionMode returns the next mode for UI-style mode cycling.
func GetNextPermissionMode(mode Mode, options ModeCycleOptions) Mode {
	switch mode {
	case ModeDefault:
		return ModePlan
	case ModePlan:
		if options.BypassAvailable {
			return ModeBypass
		}
		if options.AutoAvailable {
			return ModeAuto
		}
		return ModeDefault
	case ModeBypass:
		if options.AutoAvailable {
			return ModeAuto
		}
		return ModeDefault
	case ModeAuto:
		return ModeDefault
	default:
		return ModeDefault
	}
}

// CyclePermissionMode computes the next mode and returns a transitioned context.
func CyclePermissionMode(ctx *ToolContext, options ModeCycleOptions) (Mode, *ToolContext) {
	if ctx == nil {
		ctx = NewToolContext(ModeDefault)
	}
	nextMode := GetNextPermissionMode(ctx.PermissionMode, options)
	return nextMode, TransitionPermissionMode(ctx.PermissionMode, nextMode, ctx)
}

// TransitionPermissionMode prepares a permission context for a mode change.
func TransitionPermissionMode(fromMode, toMode Mode, ctx *ToolContext) *ToolContext {
	if ctx == nil {
		ctx = NewToolContext(ModeDefault)
	}
	if fromMode == toMode {
		return ctx
	}

	next := ctx.ApplyUpdate(PermissionUpdate{Type: UpdateSetMode, Mode: toMode})
	fromUsesAuto := fromMode == ModeAuto
	toUsesAuto := toMode == ModeAuto

	switch {
	case toUsesAuto && !fromUsesAuto:
		SetAutoModeActive(true)
		return StripDangerousPermissionsForAutoMode(next)
	case fromUsesAuto && !toUsesAuto:
		SetAutoModeActive(false)
		return RestoreDangerousPermissions(next)
	default:
		return next
	}
}

// StripDangerousPermissionsForAutoMode removes update-destination allow rules
// that would broadly bypass auto-mode Bash classification.
func StripDangerousPermissionsForAutoMode(ctx *ToolContext) *ToolContext {
	if ctx == nil {
		return nil
	}

	next := ctx
	stripped := make(RulesBySource)
	for source, ruleStrings := range ctx.AlwaysAllowRules {
		if !SupportsPermissionUpdate(source) {
			continue
		}
		for _, ruleString := range ruleStrings {
			ruleValue := RuleValueFromString(ruleString)
			if ruleValue.ToolName == "Bash" && IsDangerousBashPermission(ruleValue.RuleContent) {
				stripped[source] = append(stripped[source], ruleString)
			}
		}
	}
	if len(stripped) == 0 {
		next = ctx.ApplyUpdate(PermissionUpdate{})
		if next.StrippedDangerousRules == nil {
			next.StrippedDangerousRules = make(RulesBySource)
		}
		return next
	}

	for source, ruleStrings := range stripped {
		rules := make([]RuleValue, 0, len(ruleStrings))
		for _, ruleString := range ruleStrings {
			rules = append(rules, RuleValueFromString(ruleString))
		}
		next = next.ApplyUpdate(PermissionUpdate{
			Type:        UpdateRemoveRules,
			Destination: source,
			Behavior:    BehaviorAllow,
			Rules:       rules,
		})
	}
	next.StrippedDangerousRules = cloneRulesBySource(stripped)
	return next
}

// RestoreDangerousPermissions re-adds rules stashed by auto-mode stripping.
func RestoreDangerousPermissions(ctx *ToolContext) *ToolContext {
	if ctx == nil || len(ctx.StrippedDangerousRules) == 0 {
		return ctx
	}
	next := ctx
	for source, ruleStrings := range ctx.StrippedDangerousRules {
		rules := make([]RuleValue, 0, len(ruleStrings))
		for _, ruleString := range ruleStrings {
			rules = append(rules, RuleValueFromString(ruleString))
		}
		next = next.ApplyUpdate(PermissionUpdate{
			Type:        UpdateAddRules,
			Destination: source,
			Behavior:    BehaviorAllow,
			Rules:       rules,
		})
	}
	next.StrippedDangerousRules = nil
	return next
}
