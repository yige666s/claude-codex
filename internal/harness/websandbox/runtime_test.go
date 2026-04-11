package websandbox

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type stubRunner struct {
	requests []ExecRequest
	results  []ExecResult
	errs     []error
}

func (s *stubRunner) Run(ctx context.Context, request ExecRequest) (ExecResult, error) {
	s.requests = append(s.requests, request)
	idx := len(s.requests) - 1
	var result ExecResult
	if idx < len(s.results) {
		result = s.results[idx]
	}
	var err error
	if idx < len(s.errs) {
		err = s.errs[idx]
	}
	return result, err
}

func TestParseActionAllowsSkillScriptExecution(t *testing.T) {
	scope := Scope{RootDir: "/tmp/skill", SkillScoped: true, AllowedEnv: []string{"SHORTART_API_KEY"}}
	action, err := ParseAction(scope, `SHORTART_API_KEY=test python3 scripts/text-to-image/impl.py "hello world"`)
	if err != nil {
		t.Fatalf("expected parse success, got %v", err)
	}
	if action.Type != ActionExecuteScript || action.Binary != "python3" {
		t.Fatalf("unexpected action %#v", action)
	}
	if got := action.Args[0]; got != "/workspace/skill/scripts/text-to-image/impl.py" {
		t.Fatalf("unexpected mapped script path %q", got)
	}
}

func TestParseActionRejectsNonScriptCommand(t *testing.T) {
	scope := Scope{RootDir: "/tmp/skill", SkillScoped: true}
	if _, err := ParseAction(scope, `echo hi`); err == nil {
		t.Fatal("expected echo to be denied")
	}
}

func TestParseActionRejectsShellOperators(t *testing.T) {
	scope := Scope{RootDir: "/tmp/skill", SkillScoped: true}
	if _, err := ParseAction(scope, `find scripts -name "*.py" | head -20`); err == nil {
		t.Fatal("expected shell operator to be denied")
	}
}

func TestParseActionAllowsLegacyCdPrefixForSkillRoot(t *testing.T) {
	scope := Scope{RootDir: "/tmp/skill", SkillScoped: true, AllowedEnv: []string{"SHORTART_API_KEY"}}
	action, err := ParseAction(scope, `cd /tmp/skill && SHORTART_API_KEY=test python3 scripts/run.py`)
	if err != nil {
		t.Fatalf("expected legacy cd prefix to be normalized, got %v", err)
	}
	if action.Type != ActionExecuteScript || action.Binary != "python3" {
		t.Fatalf("unexpected action %#v", action)
	}
}

func TestParseActionRejectsLegacyCdPrefixOutsideSkillRoot(t *testing.T) {
	scope := Scope{RootDir: "/tmp/skill", SkillScoped: true}
	if _, err := ParseAction(scope, `cd /tmp/other && python3 scripts/run.py`); err == nil {
		t.Fatal("expected cd outside skill root to be denied")
	}
}

func TestRuntimeExecutesViaDockerWithScopedEnv(t *testing.T) {
	runner := &stubRunner{
		results: []ExecResult{
			{},
			{Stdout: "/workspace/output/result.txt\n"},
		},
		errs: []error{
			nil,
			nil,
		},
	}
	runtime := NewRuntime(Scope{
		RootDir:        "/tmp/skill",
		SessionID:      "sess1",
		SkillName:      "demo",
		SkillScoped:    true,
		AllowedEnv:     []string{"SHORTART_API_KEY"},
		AllowedDomains: []string{"api.shortart.ai"},
	}, RuntimeOptions{
		Runner:         runner,
		Image:          "demo-image",
		Timeout:        30 * time.Second,
		NetworkEnabled: true,
		OutputBaseDir:  t.TempDir(),
	})
	output, err := runtime.ExecuteCommand(context.Background(), `SHORTART_API_KEY=test python3 scripts/run.py`)
	if err != nil {
		t.Fatalf("expected runtime success, got %v", err)
	}
	if len(runner.requests) != 2 {
		t.Fatalf("expected image inspect + docker run, got %d requests", len(runner.requests))
	}
	runReq := runner.requests[1]
	if runReq.Name != "docker" || len(runReq.Args) == 0 || runReq.Args[0] != "run" {
		t.Fatalf("expected docker run request, got %#v", runReq)
	}
	if filepath.IsAbs(output) == false {
		t.Fatalf("expected output path rewritten to host path, got %q", output)
	}
}

func TestRuntimeBuildsImageWhenMissingAndAutoBuildEnabled(t *testing.T) {
	runner := &stubRunner{
		errs: []error{
			errors.New("missing"),
			nil,
			nil,
		},
	}
	runtime := NewRuntime(Scope{RootDir: "/tmp/skill", SkillScoped: true}, RuntimeOptions{
		Runner:         runner,
		AutoBuildImage: true,
		OutputBaseDir:  t.TempDir(),
	})
	runtime.docker.image = "claude-codex-websandbox:test"
	_, err := runtime.ExecuteCommand(context.Background(), `python3 scripts/run.py`)
	if err != nil {
		t.Fatalf("expected image autobuild path to succeed, got %v", err)
	}
	if len(runner.requests) < 3 || runner.requests[1].Args[0] != "build" {
		t.Fatalf("expected docker build to be invoked, got %#v", runner.requests)
	}
}

func TestRuntimeRejectsUndeclaredEnv(t *testing.T) {
	runtime := NewRuntime(Scope{
		RootDir:     "/tmp/skill",
		SkillScoped: true,
		AllowedEnv:  []string{"SHORTART_API_KEY"},
	}, RuntimeOptions{Runner: &stubRunner{}, OutputBaseDir: t.TempDir()})
	if _, err := runtime.ExecuteCommand(context.Background(), `OTHER_KEY=test python3 scripts/run.py`); err == nil {
		t.Fatal("expected undeclared env to be denied")
	}
}

func TestRuntimeIncludesDockerBuildLogsOnFailure(t *testing.T) {
	runner := &stubRunner{
		results: []ExecResult{
			{},
			{Stderr: "permission denied while trying to connect to docker.sock"},
		},
		errs: []error{
			errors.New("missing image"),
			errors.New("build failed"),
		},
	}
	runtime := NewRuntime(Scope{RootDir: "/tmp/skill", SkillScoped: true}, RuntimeOptions{
		Runner:         runner,
		AutoBuildImage: true,
		OutputBaseDir:  t.TempDir(),
	})
	_, err := runtime.ExecuteCommand(context.Background(), `python3 scripts/run.py`)
	if err == nil {
		t.Fatal("expected runtime error")
	}
	if !strings.Contains(err.Error(), "permission denied while trying to connect to docker.sock") {
		t.Fatalf("expected docker build stderr in error, got %v", err)
	}
}
