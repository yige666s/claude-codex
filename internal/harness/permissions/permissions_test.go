package permissions

import (
	"context"
	"strings"
	"testing"
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

func TestCheckerAuthorizeModesAndCaching(t *testing.T) {
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
