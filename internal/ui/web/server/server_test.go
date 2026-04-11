package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
	"claude-codex/internal/harness/websandbox"
)

func TestEffectiveSkillWorkingDirPrefersSkillRoot(t *testing.T) {
	skill := &skills.SkillDefinition{SkillRoot: "/tmp/skill-root"}
	if got := effectiveSkillWorkingDir(skill, "/tmp/project"); got != "/tmp/skill-root" {
		t.Fatalf("expected skill root, got %q", got)
	}
	if got := effectiveSkillWorkingDir(&skills.SkillDefinition{}, "/tmp/project"); got != "/tmp/project" {
		t.Fatalf("expected fallback dir, got %q", got)
	}
}

func TestEngineForDirUsesRegistryBuilder(t *testing.T) {
	var gotScope websandbox.Scope
	srv := &Server{
		apiKey: "test",
		model:  "claude-test",
		registryBuilder: func(scope websandbox.Scope) *toolkit.Registry {
			gotScope = scope
			return toolkit.NewRegistry()
		},
	}
	_ = srv.engineForScope(websandbox.Scope{
		RootDir:     "/tmp/skill-root",
		SkillName:   "demo",
		SkillScoped: true,
		AllowedEnv:  []string{"SHORTART_API_KEY"},
	})
	if gotScope.RootDir != "/tmp/skill-root" {
		t.Fatalf("expected registry builder to receive skill root, got %#v", gotScope)
	}
	if gotScope.SkillName != "demo" || !gotScope.SkillScoped {
		t.Fatalf("expected scope metadata to be forwarded, got %#v", gotScope)
	}
}

func TestCollectSkillScriptsListsRunnableFilesUnderScripts(t *testing.T) {
	skillDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts", "text-to-image"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "text-to-image", "impl.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatalf("write python script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte("echo hi\n"), 0o644); err != nil {
		t.Fatalf("write shell script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "notes.txt"), []byte("ignore\n"), 0o644); err != nil {
		t.Fatalf("write txt file: %v", err)
	}

	got := collectSkillScripts(skillDir)
	if len(got) != 2 {
		t.Fatalf("expected 2 runnable scripts, got %#v", got)
	}
	if got[0] != "scripts/run.sh" && got[1] != "scripts/run.sh" {
		t.Fatalf("expected scripts/run.sh in %#v", got)
	}
	if got[0] != "scripts/text-to-image/impl.py" && got[1] != "scripts/text-to-image/impl.py" {
		t.Fatalf("expected scripts/text-to-image/impl.py in %#v", got)
	}
}

func TestScopeForSkillIncludesEnvAllowlist(t *testing.T) {
	srv := &Server{}
	session := &state.Session{ID: "sess-1"}
	skill := &skills.SkillDefinition{
		Name:       "shortart",
		SkillRoot:  "/tmp/skill",
		AllowedEnv: []string{"SHORTART_API_KEY"},
		PrimaryEnv: "SHORTART_API_KEY",
	}
	scope := srv.scopeForSkill(session, skill, "/tmp/skill")
	if scope.SessionID != "sess-1" || scope.SkillName != "shortart" || !scope.SkillScoped {
		t.Fatalf("unexpected scope %#v", scope)
	}
	if len(scope.AllowedEnv) != 1 || scope.AllowedEnv[0] != "SHORTART_API_KEY" {
		t.Fatalf("expected env allowlist in scope, got %#v", scope)
	}
}

func TestBuildTrustedPromptIncludesExternalDataAndKnownScripts(t *testing.T) {
	scope := websandbox.Scope{
		SkillName:   "demo",
		SkillScoped: true,
		AllowedEnv:  []string{"SHORTART_API_KEY"},
	}
	prompt := websandbox.BuildTrustedPrompt(scope, "trusted", "user data", []string{"scripts/run.py"})
	for _, want := range []string{"<system_instruction>", "<trusted_skill_instruction>", "<external_data>", "scripts/run.py", "SHORTART_API_KEY"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got %q", want, prompt)
		}
	}
}
