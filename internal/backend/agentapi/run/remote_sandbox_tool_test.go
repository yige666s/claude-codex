package run

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"claude-codex/internal/backend/agentruntime"
)

func TestScopedBashRemoteRunnerFailsClosed(t *testing.T) {
	scope := agentruntime.Scope{UserID: "alice", SessionID: "session", SkillName: "demo", SkillRoot: "/skill", SkillScoped: true,
		AllowedTools: []string{"Bash(echo *)"}, SkillShellSandbox: agentruntime.SkillShellSandboxConfig{Runner: "remote"}}
	tool := buildSandboxBashRuntime(agentruntime.SkillShellSandboxConfig{}, "/work", scope)
	if tool == nil {
		t.Fatal("remote Bash tool silently unavailable")
	}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo test"}`))
	if err == nil || !strings.Contains(err.Error(), "refusing local fallback") {
		t.Fatalf("remote runner fell back or succeeded: %v", err)
	}
}
