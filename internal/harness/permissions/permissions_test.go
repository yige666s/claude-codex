package permissions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/hooks"
)

func TestRuleValueParsingAndSerialization(t *testing.T) {
	tests := []struct {
		name string
		rule string
		want RuleValue
	}{
		{
			name: "legacy alias without content",
			rule: "Task",
			want: RuleValue{ToolName: "AgentTool"},
		},
		{
			name: "escaped content",
			rule: `Bash(python -c "print\\(1\\)" \\*literal)`,
			want: RuleValue{ToolName: "Bash", RuleContent: `python -c "print(1)" \*literal`},
		},
		{
			name: "wildcard content collapses to tool rule",
			rule: "Bash(*)",
			want: RuleValue{ToolName: "Bash"},
		},
		{
			name: "malformed missing closing paren treated as tool name",
			rule: "Bash(npm install",
			want: RuleValue{ToolName: "Bash(npm install"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RuleValueFromString(tt.rule)
			if got != tt.want {
				t.Fatalf("RuleValueFromString(%q) = %+v, want %+v", tt.rule, got, tt.want)
			}
		})
	}

	value := RuleValue{ToolName: "Bash", RuleContent: `python -c "print(1)"`}
	serialized := RuleValueToString(value)
	if serialized != `Bash(python -c "print\(1\)")` {
		t.Fatalf("unexpected serialized rule: %q", serialized)
	}
	if roundTrip := RuleValueFromString(serialized); roundTrip != value {
		t.Fatalf("round trip mismatch: got %+v want %+v", roundTrip, value)
	}
}

func TestShellRuleParsingAndMatching(t *testing.T) {
	prefix := ParseShellPermissionRule("git:*")
	if prefix.Type != ShellRulePrefix || prefix.Prefix != "git" {
		t.Fatalf("unexpected prefix rule: %+v", prefix)
	}
	if !MatchesRule(prefix, "git status", false) {
		t.Fatal("expected prefix rule to match simple git command")
	}
	if MatchesRule(prefix, "git status && git diff", true) {
		t.Fatal("prefix rule should not match compound commands")
	}

	wildcard := ParseShellPermissionRule("git *")
	if wildcard.Type != ShellRuleWildcard {
		t.Fatalf("unexpected wildcard rule: %+v", wildcard)
	}
	if !MatchesRule(wildcard, "git", false) {
		t.Fatal("single trailing wildcard should make git args optional")
	}
	if !MatchWildcardPattern(`echo \*`, "echo *", false) {
		t.Fatal("escaped wildcard should match literal asterisk")
	}
	if MatchWildcardPattern("npm * run *", "npm run", false) {
		t.Fatal("multiple wildcards should not make trailing arguments optional")
	}
}

func TestShellRuleMatchingNormalizesWrappersEnvAndRedirections(t *testing.T) {
	tests := []struct {
		name       string
		behavior   Behavior
		rule       string
		command    string
		isCompound bool
		want       bool
	}{
		{
			name:     "deny strips arbitrary leading env var",
			behavior: BehaviorDeny,
			rule:     "rm:*",
			command:  "TARGET=tmp rm -rf tmp",
			want:     true,
		},
		{
			name:     "allow strips safe env wrapper and redirection",
			behavior: BehaviorAllow,
			rule:     "npm run:*",
			command:  "NODE_ENV=test timeout 10 npm run build > out.log",
			want:     true,
		},
		{
			name:     "allow does not strip arbitrary env var",
			behavior: BehaviorAllow,
			rule:     "npm run:*",
			command:  "TARGET=tmp npm run build",
			want:     false,
		},
		{
			name:       "allow prefix does not match compound command",
			behavior:   BehaviorAllow,
			rule:       "npm run:*",
			command:    "npm run build && rm -rf tmp",
			isCompound: true,
			want:       false,
		},
		{
			name:       "deny prefix still catches compound command",
			behavior:   BehaviorDeny,
			rule:       "rm:*",
			command:    "rm -rf tmp && echo done",
			isCompound: true,
			want:       true,
		},
		{
			name:     "deny prefix catches xargs target command",
			behavior: BehaviorDeny,
			rule:     "rm:*",
			command:  "xargs rm -rf tmp",
			want:     true,
		},
		{
			name:     "exact rule ignores output redirection",
			behavior: BehaviorAllow,
			rule:     "python script.py",
			command:  "python script.py > out.log 2>>err.log",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesRuleForBehavior(ParseShellPermissionRule(tt.rule), tt.command, tt.behavior, tt.isCompound)
			if got != tt.want {
				t.Fatalf("MatchesRuleForBehavior(%q, %q, %q) = %v, want %v", tt.rule, tt.command, tt.behavior, got, tt.want)
			}
		})
	}
}

func TestBashApprovalSuggestionsFollowCommandShape(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "two word subcommand prefix",
			command: `git commit -m "x"`,
			want:    "git commit:*",
		},
		{
			name:    "heredoc uses prefix before heredoc",
			command: "python3 - <<'PY'\nprint('hi')\nPY",
			want:    "python3 -:*",
		},
		{
			name:    "unsafe env falls back to exact",
			command: "TARGET=tmp npm run build",
			want:    "TARGET=tmp npm run build",
		},
		{
			name:    "bare shell does not suggest broad shell prefix",
			command: `bash -c "rm -rf tmp"`,
			want:    `bash -c "rm -rf tmp"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updates := approvalSuggestionsForRequest(Request{
				ToolName: "Bash",
				Level:    LevelExecute,
				Metadata: map[string]string{"command": tt.command},
			})
			if len(updates) != 1 || len(updates[0].Rules) != 1 {
				t.Fatalf("expected one suggestion, got %#v", updates)
			}
			if got := updates[0].Rules[0].RuleContent; got != tt.want {
				t.Fatalf("suggested rule = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckerAuthorizeModesAndCaching(t *testing.T) {
	if got := PermissionModeFromString("bypassPermissions"); got != ModeBypass {
		t.Fatalf("expected TS external bypassPermissions alias to map to bypass, got %q", got)
	}
	if got := PermissionModeFromString("surprise"); got != ModeDefault {
		t.Fatalf("unknown permission mode should fall back to default, got %q", got)
	}
	if got := ToExternalPermissionMode(ModeAuto); got != string(ModeDefault) {
		t.Fatalf("auto should externalize as default, got %q", got)
	}
	if !IsDefaultMode("") || PermissionModeShortTitle(ModePlan) != "Plan" {
		t.Fatal("unexpected permission mode metadata result")
	}

	var calls int
	checker := NewChecker(ModeDefault, nil, nil, WithRequestHandler(func(_ context.Context, request Request) error {
		calls++
		if request.ToolName != "bash" || request.Level != LevelExecute {
			t.Fatalf("unexpected request: %+v", request)
		}
		return nil
	}))

	if err := checker.Authorize(context.Background(), "bash", LevelExecute); err != nil {
		t.Fatalf("first authorization failed: %v", err)
	}
	if err := checker.Authorize(context.Background(), "bash", LevelExecute); err != nil {
		t.Fatalf("cached authorization failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one handler call, got %d", calls)
	}

	planChecker := NewChecker(ModePlan, nil, nil)
	if err := planChecker.Authorize(context.Background(), "edit", LevelRead); err != nil {
		t.Fatalf("plan mode should allow reads: %v", err)
	}
	if err := planChecker.Authorize(context.Background(), "edit", LevelWrite); err == nil || !strings.Contains(err.Error(), "blocked in plan mode") {
		t.Fatalf("expected plan mode write denial, got %v", err)
	}

	autoChecker := NewChecker(ModeAuto, nil, nil)
	if err := autoChecker.Authorize(context.Background(), "bash", LevelExecute); err == nil || !strings.Contains(err.Error(), "blocked in auto mode") {
		t.Fatalf("expected auto mode execute denial, got %v", err)
	}

	bypassChecker := NewChecker(ModeBypass, nil, nil)
	if err := bypassChecker.Authorize(context.Background(), "edit", LevelWrite); err != nil {
		t.Fatalf("bypass mode should allow writes: %v", err)
	}
}

func TestCheckerUsesRulesClassifierAndApprovalUpdates(t *testing.T) {
	ctx := NewToolContext(ModeDefault)
	ctx.AlwaysDenyRules[SourceProjectSettings] = []string{"Bash(rm:*)"}
	ctx.AlwaysAskRules[SourceProjectSettings] = []string{"Bash(npm publish:*)"}
	ctx.AlwaysAllowRules[SourceProjectSettings] = []string{"Bash(git status)"}

	checker := NewChecker(ModeDefault, nil, nil, WithToolContext(ctx))
	if err := checker.AuthorizeRequest(context.Background(), Request{
		ToolName: "Bash",
		Level:    LevelExecute,
		Metadata: map[string]string{"command": "git status"},
	}); err != nil {
		t.Fatalf("allow rule should authorize command: %v", err)
	}
	if err := checker.AuthorizeRequest(context.Background(), Request{
		ToolName: "Bash",
		Level:    LevelExecute,
		Metadata: map[string]string{"command": "rm -rf tmp"},
	}); err == nil || !strings.Contains(err.Error(), "denied by permission rule") {
		t.Fatalf("deny rule should block command, got %v", err)
	}

	var prompted int
	checker = NewChecker(
		ModeDefault,
		nil,
		nil,
		WithToolContext(ctx),
		WithDecisionHandler(func(_ context.Context, request Request) (Decision, error) {
			prompted++
			if !strings.EqualFold(request.ToolName, "bash") || len(request.Suggestions) == 0 {
				t.Fatalf("expected Bash request with suggestions, got %+v", request)
			}
			return Decision{
				Behavior: BehaviorAllow,
				Remember: true,
				Updates:  request.Suggestions,
			}, nil
		}),
	)
	if err := checker.AuthorizeRequest(context.Background(), Request{
		ToolName: "bash",
		Level:    LevelExecute,
		Metadata: map[string]string{"command": "npm publish --dry-run"},
	}); err != nil {
		t.Fatalf("ask rule should be resolved by decision handler: %v", err)
	}
	if prompted != 1 {
		t.Fatalf("expected one prompt, got %d", prompted)
	}
	if got := ctx.AlwaysAllowRules[SourceLocalSettings]; len(got) != 1 || got[0] != "Bash(npm publish:*)" {
		t.Fatalf("expected approval updates to be applied, got %#v", got)
	}
}

func TestCheckerAutoModeClassifier(t *testing.T) {
	var captured ClassifierRequest
	classifier := ClassifierFunc(func(_ context.Context, request ClassifierRequest) (ClassifierResult, error) {
		captured = request
		return ClassifierResult{
			Behavior:   BehaviorAllow,
			Classifier: "auto-mode",
			Confidence: "high",
			Reason:     "read-only git inspection",
		}, nil
	})

	checker := NewChecker(ModeAuto, nil, nil, WithClassifier(classifier))
	err := checker.AuthorizeRequest(context.Background(), Request{
		ToolName: "Bash",
		Level:    LevelExecute,
		Summary:  "git status",
		Metadata: map[string]string{"command": "git status"},
	})
	if err != nil {
		t.Fatalf("classifier allow should authorize auto mode request: %v", err)
	}
	if captured.ToolName != "Bash" || captured.Metadata["command"] != "git status" {
		t.Fatalf("classifier received unexpected request: %+v", captured)
	}

	checker = NewChecker(ModeAuto, nil, nil, WithClassifier(ClassifierFunc(func(context.Context, ClassifierRequest) (ClassifierResult, error) {
		return ClassifierResult{Behavior: BehaviorDeny, Reason: "destructive command", Classifier: "auto-mode"}, nil
	})))
	err = checker.AuthorizeRequest(context.Background(), Request{
		ToolName: "Bash",
		Level:    LevelExecute,
		Metadata: map[string]string{"command": "rm -rf tmp"},
	})
	if err == nil || !strings.Contains(err.Error(), "denied by auto-mode classifier") {
		t.Fatalf("classifier deny should block request, got %v", err)
	}

	checker = NewChecker(
		ModeAuto,
		nil,
		nil,
		WithClassifier(ClassifierFunc(func(context.Context, ClassifierRequest) (ClassifierResult, error) {
			return ClassifierResult{Behavior: BehaviorAsk, Reason: "needs confirmation", Classifier: "auto-mode"}, nil
		})),
		WithDecisionHandler(func(context.Context, Request) (Decision, error) {
			return Decision{Behavior: BehaviorAllow}, nil
		}),
	)
	if err := checker.AuthorizeRequest(context.Background(), Request{
		ToolName: "Bash",
		Level:    LevelExecute,
		Metadata: map[string]string{"command": "npm publish"},
	}); err != nil {
		t.Fatalf("classifier ask should fall through to approval handler: %v", err)
	}
}

func TestCheckerPersistsApprovalUpdates(t *testing.T) {
	ctx := NewToolContext(ModeDefault)
	var persisted []PermissionUpdate
	checker := NewChecker(
		ModeDefault,
		nil,
		nil,
		WithToolContext(ctx),
		WithUpdatePersister(func(_ context.Context, updates []PermissionUpdate) error {
			persisted = append(persisted, updates...)
			return nil
		}),
		WithDecisionHandler(func(context.Context, Request) (Decision, error) {
			return Decision{
				Behavior: BehaviorAllow,
				Updates: []PermissionUpdate{{
					Type:        UpdateAddRules,
					Destination: SourceLocalSettings,
					Behavior:    BehaviorAllow,
					Rules:       []RuleValue{{ToolName: "Bash", RuleContent: "git status"}},
				}},
			}, nil
		}),
	)

	if err := checker.AuthorizeRequest(context.Background(), Request{
		ToolName: "Bash",
		Level:    LevelExecute,
		Metadata: map[string]string{"command": "git status"},
	}); err != nil {
		t.Fatalf("authorization failed: %v", err)
	}
	if len(persisted) != 1 || persisted[0].Destination != SourceLocalSettings {
		t.Fatalf("expected approval update to be persisted, got %#v", persisted)
	}
	if got := ctx.AlwaysAllowRules[SourceLocalSettings]; len(got) != 1 || got[0] != "Bash(git status)" {
		t.Fatalf("expected context update, got %#v", got)
	}
}

func TestCheckerDecisionResolver(t *testing.T) {
	var handlerCalled bool
	checker := NewChecker(
		ModeDefault,
		nil,
		nil,
		WithDecisionResolver(DecisionResolverFunc(func(context.Context, Request) (Decision, bool, error) {
			return Decision{Behavior: BehaviorAllow}, true, nil
		})),
		WithDecisionHandler(func(context.Context, Request) (Decision, error) {
			handlerCalled = true
			return Decision{Behavior: BehaviorDeny}, nil
		}),
	)

	if err := checker.AuthorizeRequest(context.Background(), Request{ToolName: "Bash", Level: LevelExecute}); err != nil {
		t.Fatalf("resolver should authorize request: %v", err)
	}
	if handlerCalled {
		t.Fatal("decision handler should not run after resolver handled request")
	}
}

func TestChannelDecisionResolver(t *testing.T) {
	requests := make(chan DecisionEnvelope, 1)
	resolver := ChannelDecisionResolver{Requests: requests}
	done := make(chan struct{})
	go func() {
		defer close(done)
		envelope := <-requests
		if envelope.Request.ToolName != "Bash" {
			envelope.Reply <- DecisionResponse{Handled: true, Err: errors.New("unexpected tool")}
			return
		}
		envelope.Reply <- DecisionResponse{
			Handled:  true,
			Decision: Decision{Behavior: BehaviorAllow, Reason: "remote approved"},
		}
	}()

	decision, ok, err := resolver.ResolvePermission(context.Background(), Request{ToolName: "Bash", Level: LevelExecute})
	if err != nil {
		t.Fatalf("resolver failed: %v", err)
	}
	if !ok || decision.Behavior != BehaviorAllow || decision.Reason != "remote approved" {
		t.Fatalf("unexpected decision: ok=%v decision=%+v", ok, decision)
	}
	<-done
}

func TestTextClassifierParsesLLMResponses(t *testing.T) {
	classifier := NewTextClassifier(TextClassifierFunc(func(_ context.Context, prompt string) (string, error) {
		if !strings.Contains(prompt, "Bash") || !strings.Contains(prompt, "git status") {
			t.Fatalf("prompt missing request context: %s", prompt)
		}
		return `{"behavior":"allow","reason":"safe read-only command","confidence":"high"}`, nil
	}))

	result, err := classifier.ClassifyPermission(context.Background(), ClassifierRequest{
		ToolName: "Bash",
		Level:    LevelExecute,
		Summary:  "git status",
		Metadata: map[string]string{"command": "git status"},
	})
	if err != nil {
		t.Fatalf("text classifier failed: %v", err)
	}
	if result.Behavior != BehaviorAllow || result.Confidence != "high" || result.Classifier != "auto-mode" {
		t.Fatalf("unexpected classifier result: %+v", result)
	}

	if _, err := ParseClassifierResponse("not json"); err == nil {
		t.Fatal("expected invalid classifier response to fail")
	}
	if _, err := NewTextClassifier(TextClassifierFunc(func(context.Context, string) (string, error) {
		return "", errors.New("boom")
	})).ClassifyPermission(context.Background(), ClassifierRequest{}); err == nil {
		t.Fatal("expected classifier client error to propagate")
	}
}

func TestToolContextApplyUpdate(t *testing.T) {
	ctx := NewToolContext(ModeDefault)
	ctx.AlwaysAllowRules[SourceUserSettings] = []string{"Read"}

	next := ctx.ApplyUpdate(PermissionUpdate{
		Type:        UpdateAddRules,
		Destination: SourceLocalSettings,
		Behavior:    BehaviorAllow,
		Rules:       []RuleValue{{ToolName: "Bash", RuleContent: "git status"}},
	})
	if len(ctx.AlwaysAllowRules[SourceLocalSettings]) != 0 {
		t.Fatal("ApplyUpdate mutated original context")
	}
	if got := next.AlwaysAllowRules[SourceLocalSettings]; len(got) != 1 || got[0] != "Bash(git status)" {
		t.Fatalf("unexpected add rules result: %#v", got)
	}

	next = next.ApplyUpdate(PermissionUpdate{
		Type:        UpdateReplaceRules,
		Destination: SourceLocalSettings,
		Behavior:    BehaviorAllow,
		Rules:       []RuleValue{{ToolName: "Bash", RuleContent: "git diff"}},
	})
	if got := next.AlwaysAllowRules[SourceLocalSettings]; len(got) != 1 || got[0] != "Bash(git diff)" {
		t.Fatalf("unexpected replace result: %#v", got)
	}

	next = next.ApplyUpdate(PermissionUpdate{
		Type:        UpdateRemoveRules,
		Destination: SourceLocalSettings,
		Behavior:    BehaviorAllow,
		Rules:       []RuleValue{{ToolName: "Bash", RuleContent: "git diff"}},
	})
	if got := next.AlwaysAllowRules[SourceLocalSettings]; len(got) != 0 {
		t.Fatalf("unexpected remove result: %#v", got)
	}

	next = next.ApplyUpdate(PermissionUpdate{
		Type:        UpdateAddDirectories,
		Destination: SourceProjectSettings,
		Directories: []string{"/tmp/project"},
	})
	if next.WorkingDirectories["/tmp/project"] != string(SourceProjectSettings) {
		t.Fatalf("expected working directory source to be recorded, got %q", next.WorkingDirectories["/tmp/project"])
	}

	next = next.ApplyUpdate(PermissionUpdate{Type: UpdateSetMode, Mode: ModePlan})
	if next.PermissionMode != ModePlan {
		t.Fatalf("expected mode %q, got %q", ModePlan, next.PermissionMode)
	}
}

func TestPermissionRuleHelpers(t *testing.T) {
	ctx := NewToolContext(ModeDefault)
	ctx.AlwaysAllowRules[SourceUserSettings] = []string{"Read", "Bash(git status)", "mcp__notes__*"}
	ctx.AlwaysDenyRules[SourceProjectSettings] = []string{"AgentTool(review)", "Bash(rm -rf /)"}
	ctx.AlwaysAskRules[SourceSession] = []string{"Write"}

	legacy := GetLegacyToolNames("TaskOutputTool")
	if len(legacy) != 2 || legacy[0] != "AgentOutputTool" || legacy[1] != "BashOutputTool" {
		t.Fatalf("unexpected legacy aliases: %#v", legacy)
	}

	if got := GetAllowRules(ctx); len(got) != 3 || got[0].Value.ToolName != "Read" {
		t.Fatalf("unexpected allow rules: %#v", got)
	}
	if rule, ok := ToolAlwaysAllowedRule(ctx, "mcp__notes__search"); !ok || rule.Value.ToolName != "mcp__notes__*" {
		t.Fatalf("expected MCP server wildcard allow rule, got %#v %v", rule, ok)
	}
	if rule, ok := GetAskRuleForTool(ctx, "Write"); !ok || rule.Source != SourceSession {
		t.Fatalf("expected ask rule for Write, got %#v %v", rule, ok)
	}
	if rule, ok := GetDenyRuleForAgent(ctx, "AgentTool", "review"); !ok || rule.Value.RuleContent != "review" {
		t.Fatalf("expected agent deny rule, got %#v %v", rule, ok)
	}

	agents := []AgentDescriptor{{AgentType: "review"}, {AgentType: "explore"}}
	filtered := FilterDeniedAgents(agents, ctx, "AgentTool")
	if len(filtered) != 1 || filtered[0].AgentType != "explore" {
		t.Fatalf("unexpected filtered agents: %#v", filtered)
	}

	byContent := GetRuleByContentsForToolName(ctx, "Bash", BehaviorDeny)
	if _, ok := byContent["rm -rf /"]; !ok {
		t.Fatalf("expected Bash deny rule by content, got %#v", byContent)
	}
}

func TestPermissionUpdateHelpers(t *testing.T) {
	updates := []PermissionUpdate{
		{
			Type:        UpdateSetMode,
			Destination: SourceSession,
			Mode:        ModePlan,
		},
		{
			Type:        UpdateAddRules,
			Destination: SourceUserSettings,
			Behavior:    BehaviorAllow,
			Rules:       []RuleValue{{ToolName: "Bash", RuleContent: "git status"}},
		},
		{
			Type:        UpdateAddDirectories,
			Destination: SourceSession,
			Directories: []string{"/tmp/work"},
		},
	}

	if !HasRules(updates) {
		t.Fatal("expected HasRules to detect addRules updates")
	}
	rules := ExtractRules(updates)
	if len(rules) != 1 || rules[0].RuleContent != "git status" {
		t.Fatalf("unexpected extracted rules: %#v", rules)
	}
	if !SupportsPersistence(SourceLocalSettings) || SupportsPersistence(SourceSession) {
		t.Fatal("unexpected persistence support result")
	}
	if !SupportsPermissionUpdate(SourceCLIArg) || SupportsPermissionUpdate(SourceCommand) {
		t.Fatal("unexpected update destination support result")
	}

	ctx := NewToolContext(ModeDefault).ApplyUpdates(updates)
	if ctx.PermissionMode != ModePlan {
		t.Fatalf("expected mode %q, got %q", ModePlan, ctx.PermissionMode)
	}
	if got := ctx.AlwaysAllowRules[SourceUserSettings]; len(got) != 1 || got[0] != "Bash(git status)" {
		t.Fatalf("unexpected allow rules after batch apply: %#v", got)
	}
	if ctx.WorkingDirectories["/tmp/work"] != string(SourceSession) {
		t.Fatalf("expected working directory to be added, got %#v", ctx.WorkingDirectories)
	}
}

func TestPermissionModeCycleAndAutoTransition(t *testing.T) {
	ctx := NewToolContext(ModeDefault)
	ctx.AlwaysAllowRules[SourceUserSettings] = []string{"Bash(python:*)", "Bash(git status)", "Read"}
	ctx.AlwaysAllowRules[SourcePolicySettings] = []string{"Bash(node:*)"}

	nextMode, nextCtx := CyclePermissionMode(ctx, ModeCycleOptions{
		BypassAvailable: true,
		AutoAvailable:   true,
	})
	if nextMode != ModePlan || nextCtx.PermissionMode != ModePlan {
		t.Fatalf("default should cycle to plan, got %q / %q", nextMode, nextCtx.PermissionMode)
	}

	nextMode, nextCtx = CyclePermissionMode(nextCtx, ModeCycleOptions{
		BypassAvailable: true,
		AutoAvailable:   true,
	})
	if nextMode != ModeBypass || nextCtx.PermissionMode != ModeBypass {
		t.Fatalf("plan should cycle to bypass when available, got %q / %q", nextMode, nextCtx.PermissionMode)
	}

	nextMode, nextCtx = CyclePermissionMode(nextCtx, ModeCycleOptions{
		BypassAvailable: true,
		AutoAvailable:   true,
	})
	if nextMode != ModeAuto || nextCtx.PermissionMode != ModeAuto {
		t.Fatalf("bypass should cycle to auto when available, got %q / %q", nextMode, nextCtx.PermissionMode)
	}
	if got := nextCtx.AlwaysAllowRules[SourceUserSettings]; len(got) != 2 || got[0] != "Bash(git status)" || got[1] != "Read" {
		t.Fatalf("expected dangerous user Bash rule to be stripped, got %#v", got)
	}
	if got := nextCtx.AlwaysAllowRules[SourcePolicySettings]; len(got) != 1 || got[0] != "Bash(node:*)" {
		t.Fatalf("policy rule should not be stripped by update-only auto transition, got %#v", got)
	}
	if got := nextCtx.StrippedDangerousRules[SourceUserSettings]; len(got) != 1 || got[0] != "Bash(python:*)" {
		t.Fatalf("expected stripped user rule to be stashed, got %#v", got)
	}
	if !IsAutoModeActive() {
		t.Fatal("expected auto mode activation flag")
	}

	restored := TransitionPermissionMode(ModeAuto, ModeDefault, nextCtx)
	if restored.PermissionMode != ModeDefault {
		t.Fatalf("expected restored mode %q, got %q", ModeDefault, restored.PermissionMode)
	}
	if got := restored.AlwaysAllowRules[SourceUserSettings]; len(got) != 3 {
		t.Fatalf("expected dangerous rule to be restored, got %#v", got)
	}
	if restored.StrippedDangerousRules != nil {
		t.Fatalf("expected stripped rule stash to be cleared, got %#v", restored.StrippedDangerousRules)
	}
	if IsAutoModeActive() {
		t.Fatal("expected auto mode activation flag to be cleared")
	}
}

func TestAutoModeAndDangerTrackingHelpers(t *testing.T) {
	ResetAutoModeStateForTesting()
	t.Cleanup(ResetAutoModeStateForTesting)

	SetAutoModeActive(true)
	SetAutoModeFlagCli(true)
	SetAutoModeCircuitBroken(true)
	if !IsAutoModeActive() || !GetAutoModeFlagCli() || !IsAutoModeCircuitBroken() {
		t.Fatal("expected auto mode flags to round-trip")
	}

	state := NewDenialTrackingState()
	for i := 0; i < 2; i++ {
		state = RecordDenial(state)
	}
	if ShouldFallbackToPrompting(state) {
		t.Fatal("should not fallback before thresholds are reached")
	}
	state = RecordDenial(state)
	if !ShouldFallbackToPrompting(state) {
		t.Fatal("expected fallback after max consecutive denials")
	}
	state = RecordSuccess(state)
	if state.ConsecutiveDenials != 0 || state.TotalDenials != 3 {
		t.Fatalf("unexpected success reset state: %+v", state)
	}

	if !IsDangerousBashPermission("python:*") {
		t.Fatal("expected python:* to be treated as dangerous")
	}
	if IsDangerousBashPermission("python script.py") {
		t.Fatal("exact command should not be treated as dangerous broad permission")
	}
	if !IsAutoModeAllowlistedTool("TodoWrite") || IsAutoModeAllowlistedTool("Bash") {
		t.Fatal("unexpected allowlist results")
	}
}

func TestHookDecisionResolver(t *testing.T) {
	registry := hooks.NewRegistry()
	err := registry.Register(permissionHook{
		result: &hooks.HookResult{
			Continue: true,
			PermissionDecision: &hooks.PermissionDecision{
				Behavior: "allow",
				Reason:   "hook approved",
				UpdatedPermissions: []hooks.PermissionUpdate{{
					Tool:     "Bash(git status)",
					Behavior: "allow",
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("register hook: %v", err)
	}

	resolver := HookDecisionResolver{
		Executor:   hooks.NewExecutor(registry),
		WorkingDir: "/tmp/work",
		SessionID:  "session-1",
	}
	decision, ok, err := resolver.ResolvePermission(context.Background(), Request{
		ToolName: "Bash",
		Level:    LevelExecute,
		Summary:  "git status",
		Metadata: map[string]string{"command": "git status"},
	})
	if err != nil {
		t.Fatalf("resolver failed: %v", err)
	}
	if !ok || decision.Behavior != BehaviorAllow || decision.Reason != "hook approved" {
		t.Fatalf("unexpected hook decision: ok=%v decision=%+v", ok, decision)
	}
	if len(decision.Updates) != 1 || decision.Updates[0].Rules[0].RuleContent != "git status" {
		t.Fatalf("expected hook permission update to be converted, got %#v", decision.Updates)
	}
}

type permissionHook struct {
	result *hooks.HookResult
}

func (h permissionHook) Name() string { return "permission-hook" }

func (h permissionHook) Event() hooks.HookEvent { return hooks.EventPermissionRequest }

func (h permissionHook) Execute(context.Context, *hooks.HookInput) (*hooks.HookResult, error) {
	return h.result, nil
}

func (h permissionHook) IsAsync() bool { return false }

func (h permissionHook) Timeout() time.Duration { return time.Second }
