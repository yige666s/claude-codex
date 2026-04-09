package permissions

import "testing"

func TestDetectUnreachableRulesFlagsAskShadowing(t *testing.T) {
	ctx := NewToolContext(ModeDefault)
	ctx.AlwaysAllowRules[SourceLocalSettings] = []string{"Bash(ls:*)"}
	ctx.AlwaysAskRules[SourceProjectSettings] = []string{"Bash"}

	got := DetectUnreachableRules(ctx, DetectUnreachableRulesOptions{})

	if len(got) != 1 {
		t.Fatalf("DetectUnreachableRules() len = %d, want 1", len(got))
	}
	if got[0].ShadowType != ShadowTypeAsk {
		t.Fatalf("ShadowType = %q, want %q", got[0].ShadowType, ShadowTypeAsk)
	}
	if got[0].ShadowedBy.Source != SourceProjectSettings {
		t.Fatalf("ShadowedBy.Source = %q, want %q", got[0].ShadowedBy.Source, SourceProjectSettings)
	}
}

func TestDetectUnreachableRulesFlagsDenyShadowing(t *testing.T) {
	ctx := NewToolContext(ModeDefault)
	ctx.AlwaysAllowRules[SourceLocalSettings] = []string{"Bash(ls:*)"}
	ctx.AlwaysDenyRules[SourcePolicySettings] = []string{"Bash"}

	got := DetectUnreachableRules(ctx, DetectUnreachableRulesOptions{})

	if len(got) != 1 {
		t.Fatalf("DetectUnreachableRules() len = %d, want 1", len(got))
	}
	if got[0].ShadowType != ShadowTypeDeny {
		t.Fatalf("ShadowType = %q, want %q", got[0].ShadowType, ShadowTypeDeny)
	}
}

func TestDetectUnreachableRulesSkipsPersonalBashAskWhenSandboxAutoAllowEnabled(t *testing.T) {
	ctx := NewToolContext(ModeDefault)
	ctx.AlwaysAllowRules[SourceLocalSettings] = []string{"Bash(ls:*)"}
	ctx.AlwaysAskRules[SourceUserSettings] = []string{"Bash"}

	got := DetectUnreachableRules(ctx, DetectUnreachableRulesOptions{SandboxAutoAllowEnabled: true})

	if len(got) != 0 {
		t.Fatalf("DetectUnreachableRules() len = %d, want 0", len(got))
	}
}

func TestIsSharedSettingSource(t *testing.T) {
	cases := []struct {
		source RuleSource
		want   bool
	}{
		{SourceProjectSettings, true},
		{SourcePolicySettings, true},
		{SourceCommand, true},
		{SourceUserSettings, false},
		{SourceLocalSettings, false},
		{SourceCLIArg, false},
	}

	for _, tc := range cases {
		if got := IsSharedSettingSource(tc.source); got != tc.want {
			t.Fatalf("IsSharedSettingSource(%q) = %v, want %v", tc.source, got, tc.want)
		}
	}
}
