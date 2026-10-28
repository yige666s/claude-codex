package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
)

func TestPublishedPromptAndPolicyMustMatchLoadedSkill(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "SKILL.md")
	content := []byte("---\nname: demo\nallowed-tools:\n  - Bash(echo *)\nversion: 1\n---\nSay hello")
	if err := os.WriteFile(entry, content, 0600); err != nil {
		t.Fatal(err)
	}
	skill, err := skills.NewSkillLoader().LoadSkillFromFile(entry, skills.SourceFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePublishedSkillDefinition(skill, content); err != nil {
		t.Fatal(err)
	}
	changedPrompt := []byte(strings.Replace(string(content), "Say hello", "Run unreviewed code", 1))
	if err := validatePublishedSkillDefinition(skill, changedPrompt); err == nil {
		t.Fatal("changed prompt accepted")
	}
	changedPolicy := []byte(strings.Replace(string(content), "Bash(echo *)", "Bash(*)", 1))
	if err := validatePublishedSkillDefinition(skill, changedPolicy); err == nil {
		t.Fatal("changed allowed tools accepted")
	}
	changedVersion := []byte(strings.Replace(string(content), "version: 1", "version: 2", 1))
	if err := validatePublishedSkillDefinition(skill, changedVersion); err == nil {
		t.Fatal("changed version accepted")
	}
	skill.LoadedFrom = string(skills.LoadedFromMCP)
	if err := validatePublishedSkillDefinition(skill, content); err == nil {
		t.Fatal("unbound skill source accepted")
	}
}

type fakeCollectedRemote struct {
	published []byte
	prepared  int
	collected int
	released  int
}

func (f *fakeCollectedRemote) RemoteSkillEnabled(_ context.Context, user, skill string) bool {
	return user == "alice"
}
func (f *fakeCollectedRemote) PrepareSkill(ctx context.Context, req RemoteSkillRequest) (context.Context, []byte, func() error, error) {
	f.prepared++
	if req.UserID != "alice" {
		return nil, nil, nil, errors.New("unexpected canary user")
	}
	return ctx, f.published, func() error { f.released++; return nil }, nil
}
func (f *fakeCollectedRemote) ExecuteSkillCommand(context.Context, RemoteSkillRequest) (string, error) {
	return "", errors.New("not needed")
}
func (f *fakeCollectedRemote) CollectSkillArtifacts(_ context.Context, req RemoteSkillRequest) ([]RemoteSkillArtifact, error) {
	f.collected++
	if req.UserID != "alice" {
		return nil, errors.New("cross-user collection")
	}
	return []RemoteSkillArtifact{{Filename: "remote-report.txt", Content: []byte("from remote Node")}}, nil
}
func TestRemoteCollectedArtifactRegisteredWithService(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "remote-report")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	text := "---\nname: remote-report\nuser-invocable: true\nmetadata:\n  agentapi:\n    produces_artifacts: true\n---\nProduce a test report"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := skills.NewSkillLoader().LoadSkillsFromDirectory(parent, skills.SourceFile)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("load: %v %d", err, len(loaded))
	}
	remote := &fakeCollectedRemote{published: []byte(text)}
	root := t.TempDir()
	storeRoot := t.TempDir()
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute,
		SkillShellSandbox: SkillShellSandboxConfig{Runner: "docker"}, RemoteSkillExecutor: remote},
		NewFileSessionStore(storeRoot), NewFileMemoryService(storeRoot), fakeSkillCatalog{skills: loaded}, func(Scope) Runner { return echoRunner{} })
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Chat(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "/remote-report fake"}, &collectSink{}); err != nil {
		t.Fatal(err)
	}
	files, err := runtime.ListArtifacts(context.Background(), "alice", session.ID)
	if err != nil || len(files) != 1 || files[0].Filename != "remote-report.txt" {
		t.Fatalf("saved: %+v %v", files, err)
	}
	if remote.prepared != 1 || remote.collected != 1 || remote.released != 1 {
		t.Fatalf("remote lifecycle: %+v", remote)
	}
	if got := runtime.effectiveSkillSandbox(context.Background(), "bob", "remote-report", SkillRuntimePolicy{}).Runner; got == "remote" {
		t.Fatal("noncanary user routed remote")
	}
}

type remoteArtifactToolRunner struct {
	writer ArtifactWriter
	root   string
}

func (r remoteArtifactToolRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(ctx, session, prompt)
}
func (r remoteArtifactToolRunner) RunGeneratedPrompt(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	inputBytes, _ := json.Marshal(map[string]string{"filename": "remote-report.txt", "content_type": "text/plain", "file_path": "generated-artifacts/remote-report.txt"})
	input := json.RawMessage(inputBytes)
	result, err := NewArtifactTool(r.writer, r.root).Execute(ctx, input)
	if err != nil {
		return engine.Result{Session: session}, err
	}
	session.AddToolResult("remote-artifact-test", ArtifactToolName, input, result.Output)
	session.AddAssistantMessage("remote report ready")
	return engine.Result{Output: "remote report ready", Session: session}, nil
}
func TestRemoteArtifactFilePathFlowStoresOnce(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "remote-report")
	if err := os.MkdirAll(skillDir, 0700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: remote-report\nuser-invocable: true\nmetadata:\n  agentapi:\n    produces_artifacts: true\n---\nProduce a report"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := skills.NewSkillLoader().LoadSkillsFromDirectory(filepath.Dir(skillDir), skills.SourceFile)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("skill load: %v", err)
	}
	remote := &fakeCollectedRemote{published: []byte(content)}
	dataDir := t.TempDir()
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: t.TempDir(), TurnTimeout: time.Minute,
		SkillShellSandbox: SkillShellSandboxConfig{Runner: "docker"}, RemoteSkillExecutor: remote},
		NewFileSessionStore(dataDir), NewFileMemoryService(dataDir), fakeSkillCatalog{skills: loaded},
		func(scope Scope) Runner {
			if scope.SkillScoped {
				return remoteArtifactToolRunner{writer: scope.Artifacts, root: scope.WorkingDir}
			}
			return echoRunner{}
		})
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Chat(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "/remote-report fake"}, &collectSink{}); err != nil {
		t.Fatal(err)
	}
	files, err := runtime.ListArtifacts(context.Background(), "alice", session.ID)
	if err != nil || len(files) != 1 || files[0].Filename != "remote-report.txt" {
		t.Fatalf("saved: %+v %v", files, err)
	}
	if remote.collected != 1 {
		t.Fatalf("unexpected collection calls=%d", remote.collected)
	}
}
