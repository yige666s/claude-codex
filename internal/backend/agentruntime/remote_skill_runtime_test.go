package agentruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/skills"
)

type remoteSkillTestExecutor struct {
	calls  []RemoteSkillRequest
	output string
	err    error
}

func (e *remoteSkillTestExecutor) ExecuteSkillCommand(ctx context.Context, req RemoteSkillRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	e.calls = append(e.calls, req)
	return e.output, e.err
}

func TestRemoteSkillShellFailsClosedWithoutExecutor(t *testing.T) {
	req := RemoteSkillRequest{UserID: "alice", SessionID: "session", SkillName: "demo"}
	runtime := NewRemoteSkillShellRuntime(nil, context.Background(), req, skills.ShellBash, []string{"Bash(echo *)"}, 1024)
	if err := runtime.ValidateCommand("echo test"); err == nil || !strings.Contains(err.Error(), "refusing local fallback") {
		t.Fatalf("missing executor unexpectedly allowed: %v", err)
	}
	if _, err := runtime.ExecuteCommand(context.Background(), "echo test"); err == nil {
		t.Fatal("missing remote executor did not fail closed")
	}
}

func TestRemoteSkillPromptCommandRoutesWithIdentityAndCancellation(t *testing.T) {
	fake := &remoteSkillTestExecutor{output: "remote-result"}
	req := RemoteSkillRequest{UserID: "alice", SessionID: "session", SkillName: "demo", WorkingDir: "/work", SkillRoot: "/skill"}
	parent, cancel := context.WithCancel(context.Background())
	runtime := NewRemoteSkillShellRuntime(fake, parent, req, skills.ShellBash, []string{"Bash(echo *)"}, 1024)
	output, err := skills.ExecuteShellCommandsInPromptWithTimeout("```!\necho test\n```", skills.ShellBash, "/skill", nil, []string{"Bash(echo *)"}, runtime, time.Second)
	if err != nil || strings.TrimSpace(output) != "remote-result" {
		t.Fatalf("prompt shell output=%q err=%v", output, err)
	}
	if len(fake.calls) != 1 || fake.calls[0].UserID != "alice" || fake.calls[0].SessionID != "session" || fake.calls[0].Command != "echo test" {
		t.Fatalf("remote calls=%+v", fake.calls)
	}
	cancel()
	if _, err = runtime.ExecuteCommand(context.Background(), "echo again"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled parent context not honored: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("canceled request reached remote: %+v", fake.calls)
	}
}

func TestRemotePolicyCannotBeOverriddenBySkillLocalRunner(t *testing.T) {
	base := SkillShellSandboxConfig{Runner: "remote", Network: "none", Image: "pinned"}
	override := SkillShellSandboxConfig{Runner: "local", Network: "host", Image: "mutable", PidsLimit: 1024}
	got := applySkillSandboxPolicy(base, override)
	if got.Runner != "remote" || got.Network != "none" || got.Image != "pinned" || got.PidsLimit == 1024 {
		t.Fatalf("skill metadata changed remote policy: %+v", got)
	}
}

func TestRuntimeSkillPromptPrefersConfiguredRemoteOverLegacyFrontmatter(t *testing.T) {
	fake := &remoteSkillTestExecutor{output: "ok"}
	r := NewRuntime(RuntimeConfig{SkillShellSandbox: SkillShellSandboxConfig{Runner: "remote"}, RemoteSkillExecutor: fake}, nil, nil, nil, nil)
	skill := &skills.SkillDefinition{Name: "documents", Shell: skills.ShellBash, AllowedTools: []string{"Bash(echo *)"}}
	policy := SkillRuntimePolicy{AllowedTools: []string{"Bash(echo *)"}, Sandbox: SkillShellSandboxConfig{Runner: "local", Network: "host"}}
	runtime := r.skillShellRuntime(context.Background(), "alice", "session", "/work", "/skill", skill, policy)
	if runtime == nil {
		t.Fatal("remote runtime missing")
	}
	out, err := runtime.ExecuteCommand(context.Background(), "echo test")
	if err != nil || out != "ok" || len(fake.calls) != 1 {
		t.Fatalf("remote call=%q, %v, %+v", out, err, fake.calls)
	}
}
