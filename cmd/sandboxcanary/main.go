package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/backend/sandboxartifact"
	"claude-codex/internal/backend/sandboxbroker"
	"claude-codex/internal/backend/sandboxpolicy"
	"claude-codex/internal/backend/sandboxtransfer"
	"claude-codex/internal/harness/skills"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	mode := flag.String("mode", "", "activate, update-profile, smoke, prompt, or rollback")
	cfgFile := flag.String("node-config", "", "private canary node config JSON")
	root := flag.String("skill-root", "", "approved Skill directory")
	workspace := flag.String("workspace", "", "fake workspace directory")
	storeDir := flag.String("artifact-store", "", "private published Skill store")
	keyFile := flag.String("node-key-file", "", "mode-0600 Node credential")
	previous := flag.Int64("previous-revision", 0, "rollback target")
	nodeEndpoint := flag.String("node-endpoint", "", "Node HTTPS endpoint from the operator policy")
	credentialRef := flag.String("credential-ref", "", "Node credential reference for the policy")
	flag.Parse()
	ctx, stop := context.WithTimeout(context.Background(), 90*time.Second)
	defer stop()
	dsn := os.Getenv("AGENT_API_SQL_DSN")
	if dsn == "" {
		die(errors.New("SQL DSN env required"))
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		die(err)
	}
	defer db.Close()
	store := &sandboxpolicy.Store{DB: db}
	switch *mode {
	case "activate":
		raw, err := os.ReadFile(*cfgFile)
		if err != nil {
			die(err)
		}
		var node struct{ Profile sandboxpolicy.Profile }
		if err := json.Unmarshal(raw, &node); err != nil {
			die(err)
		}
		doc := sandboxpolicy.Document{Enabled: true, DefaultProfile: "isolated", Profiles: map[string]sandboxpolicy.Profile{"isolated": node.Profile}, Routes: []sandboxpolicy.Route{{Origin: "unknown", Profile: "isolated"}}, NodePools: map[string]sandboxpolicy.NodePool{}}
		if *nodeEndpoint == "" || *credentialRef == "" || node.Profile.NodePool == "" {
			die(errors.New("endpoint, pool and credential reference required"))
		}
		doc.NodePools = map[string]sandboxpolicy.NodePool{node.Profile.NodePool: {Endpoint: *nodeEndpoint, CredentialRef: *credentialRef, Environment: "development", MaxConcurrency: 1}}
		if err := doc.Validate(); err != nil {
			die(err)
		}
		current, err := store.Active(ctx)
		if err != nil {
			die(err)
		}
		if current.Document.Enabled {
			die(errors.New("active policy is not disabled; refusing to overwrite"))
		}
		draft, err := store.Draft(ctx, doc, "sandbox-canary-20261028")
		if err != nil {
			die(err)
		}
		published, err := store.Publish(ctx, draft.ID, current.ID, "sandbox-canary-20261028")
		if err != nil {
			die(err)
		}
		fmt.Printf("sandbox policy canary revision=%d previous=%d\n", published.ID, current.ID)
	case "update-profile":
		raw, err := os.ReadFile(*cfgFile)
		if err != nil {
			die(err)
		}
		var node struct{ Profile sandboxpolicy.Profile }
		if err := json.Unmarshal(raw, &node); err != nil {
			die(err)
		}
		active, err := store.Active(ctx)
		if err != nil {
			die(err)
		}
		if !active.Document.Enabled {
			die(errors.New("cannot update disabled sandbox policy"))
		}
		doc := active.Document
		current := doc.Profiles[doc.DefaultProfile]
		if current.NodePool == "" || current.NodePool != node.Profile.NodePool {
			die(errors.New("Node pool change requires independent approval"))
		}
		if err := sandboxpolicy.ValidateFileOCIProfile(node.Profile); err != nil {
			die(err)
		}
		doc.Profiles[doc.DefaultProfile] = node.Profile
		if err := doc.Validate(); err != nil {
			die(err)
		}
		draft, err := store.Draft(ctx, doc, "sandbox-profile-upgrade")
		if err != nil {
			die(err)
		}
		published, err := store.Publish(ctx, draft.ID, active.ID, "sandbox-profile-upgrade")
		if err != nil {
			die(err)
		}
		fmt.Printf("sandbox profile revision=%d previous=%d"+string(rune(10)), published.ID, active.ID)
	case "rollback":
		if *previous < 1 {
			die(errors.New("previous revision required"))
		}
		active, err := store.Active(ctx)
		if err != nil {
			die(err)
		}
		revision, err := store.Rollback(ctx, *previous, active.ID, "sandbox-canary-rollback")
		if err != nil {
			die(err)
		}
		fmt.Printf("sandbox policy rolled back to new disabled revision=%d\n", revision.ID)
	case "smoke", "prompt":
		if err := runBroker(ctx, store, *root, *workspace, *storeDir, *keyFile, *mode == "prompt"); err != nil {
			die(err)
		}
		fmt.Println("Broker to real Node", *mode, "passed with fake workspace")
	default:
		die(errors.New("mode activate, update-profile, rollback, smoke or prompt required"))
	}
}
func runBroker(ctx context.Context, policy *sandboxpolicy.Store, skill, workspace, storeDir, keyPath string, prompt bool) error {
	for _, path := range []string{skill, workspace, storeDir, keyPath} {
		if !filepath.IsAbs(path) {
			return errors.New("absolute canary paths required")
		}
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	key := strings.TrimSpace(string(raw))
	if len(key) < 32 {
		return errors.New("node key too short")
	}
	id := filepath.Base(skill)
	skillRoot := filepath.Dir(skill)
	builder := sandboxtransfer.Builder{WorkspaceRoot: filepath.Dir(workspace), SkillRoots: []string{skillRoot}, Limits: sandboxtransfer.DevLimits()}
	artifacts := sandboxartifact.Store{Dir: storeDir, Builder: builder}
	active, err := policy.Active(ctx)
	if err != nil {
		return err
	}
	profile := active.Document.Profiles[active.Document.DefaultProfile]
	expectedRef := active.Document.NodePools[profile.NodePool].CredentialRef
	broker := &sandboxbroker.Executor{Policy: policy, ResolveCredential: func(_ context.Context, ref string) (string, error) {
		if ref != expectedRef {
			return "", errors.New("credential mismatch")
		}
		return key, nil
	}, Snapshot: builder, SkillCatalog: sandboxbroker.PublishedCatalog{Store: artifacts, Roots: map[string]string{id: skill}}, Artifacts: artifacts, Node: sandboxbroker.DevelopmentNodeClient{}, AllowedUserIDs: map[string]bool{"sandbox-smoke-user": true}}
	request := agentruntime.RemoteSkillRequest{UserID: "sandbox-smoke-user", SessionID: "sandbox-smoke-session", SkillName: id, WorkingDir: workspace, SkillRoot: skill}
	prepared, published, release, err := broker.PrepareSkill(ctx, request)
	if err != nil {
		return err
	}
	defer release()
	if len(published) == 0 {
		return errors.New("published prompt missing")
	}
	if prompt {
		if id != "docx" {
			return errors.New("prompt smoke requires docx Skill")
		}
		loaded, err := skills.NewSkillLoader().LoadSkillsFromDirectory(filepath.Dir(skill), skills.SourceFile)
		if err != nil {
			return err
		}
		var definition *skills.SkillDefinition
		for _, candidate := range loaded {
			if candidate.Name == id {
				definition = candidate
				break
			}
		}
		if definition == nil {
			return errors.New("Skill not loaded")
		}
		runtime := agentruntime.NewRemoteSkillShellRuntime(broker, prepared, request, definition.Shell, definition.AllowedTools, 1<<20)
		args := "# Sandbox verification report" + string(rune(10)) + string(rune(10)) + "This is finalized fake text for the DOCX remote prompt check."
		blocks, err := definition.GetPrompt(args, &skills.SkillContext{SessionID: request.SessionID, WorkingDir: skill, ShellRuntime: runtime, ShellTimeout: 60 * time.Second})
		if err != nil {
			return fmt.Errorf("actual Skill prompt execution failed: %w", err)
		}
		found := false
		for _, block := range blocks {
			if strings.Contains(block.Text, "artifact_file_path: generated-artifacts/") {
				found = true
			}
		}
		if !found {
			return errors.New("actual DOCX prompt did not emit a final artifact path")
		}
		files, err := broker.CollectSkillArtifacts(prepared, request)
		if err != nil {
			return err
		}
		if len(files) != 1 || !strings.HasSuffix(files[0].Filename, ".docx") || !strings.HasPrefix(string(files[0].Content), "PK") {
			return errors.New("actual Skill prompt did not produce a valid DOCX")
		}
		return release()
	}
	request.Command = "cat " + filepath.Join(workspace, "input.txt") + " && printf fake > " + filepath.Join(workspace, "outputs/result.txt")
	output, err := broker.ExecuteSkillCommand(prepared, request)
	if err != nil {
		return err
	}
	if !strings.Contains(output, "fake input") {
		return errors.New("unexpected remote stdout")
	}
	files, err := broker.CollectSkillArtifacts(prepared, request)
	if err != nil {
		return err
	}
	if len(files) != 1 || string(files[0].Content) != "fake" {
		return errors.New("remote artifact mismatch")
	}
	return release()
}
func die(err error) { fmt.Fprintln(os.Stderr, "sandbox canary refused:", err); os.Exit(1) }
