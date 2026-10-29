package run

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/backend/sandboxartifact"
	"claude-codex/internal/backend/sandboxbroker"
	"claude-codex/internal/backend/sandboxpolicy"
	"claude-codex/internal/backend/sandboxtransfer"
	"claude-codex/internal/harness/skills"
)

// Optional canary-only wiring. Endpoints/profiles remain in the active DB
// revision; credentials live in mode-0600 files, never in that DB row.
func initializeRemoteSandbox(ctx context.Context, cfg startupconfig.Config, manager *skills.SkillManager, published agentruntime.SkillCatalog, policy sandboxpolicy.Repository) (agentruntime.RemoteSkillExecutor, http.Handler, error) {
	users := startupconfig.SplitCSV(os.Getenv("AGENT_API_SANDBOX_CANARY_USER_IDS"))
	if len(users) == 0 {
		return nil, nil, nil
	}
	if policy == nil || cfg.UserWorkspaceRoot == "" {
		return nil, nil, errors.New("remote sandbox requires SQL policy and per-user workspaces")
	}
	skillIDs := startupconfig.SplitCSV(os.Getenv("AGENT_API_SANDBOX_SKILL_IDS"))
	tokenPath := strings.TrimSpace(os.Getenv("AGENT_API_SANDBOX_NODE_KEY_FILE"))
	artifactTokenPath := strings.TrimSpace(os.Getenv("AGENT_API_SANDBOX_ARTIFACT_KEY_FILE"))
	artifactDir := strings.TrimSpace(os.Getenv("AGENT_API_SANDBOX_ARTIFACT_DIR"))
	credentialRef := strings.TrimSpace(os.Getenv("AGENT_API_SANDBOX_NODE_CREDENTIAL_REF"))
	if len(skillIDs) == 0 || artifactDir == "" || credentialRef == "" {
		return nil, nil, errors.New("remote sandbox publication and credential references required")
	}
	roots, err := remotePublishedSkillRoots(manager, published, skillIDs)
	if err != nil {
		return nil, nil, err
	}
	if _, err := readPrivateSandboxKey(tokenPath); err != nil {
		return nil, nil, err
	}
	artifactKey, err := readPrivateSandboxKey(artifactTokenPath)
	if err != nil {
		return nil, nil, err
	}
	allowedRoots := startupconfig.SplitCSV(cfg.SkillDirs)
	builder := sandboxtransfer.Builder{WorkspaceRoot: cfg.UserWorkspaceRoot, SkillRoots: allowedRoots,
		ExcludedWorkspaceDirs: startupconfig.SplitCSV(os.Getenv("AGENT_API_SANDBOX_WORKSPACE_EXCLUDE_DIRS")), Limits: sandboxtransfer.DevLimits()}
	store := sandboxartifact.Store{Dir: artifactDir, Builder: builder}
	for _, id := range skillIDs {
		if _, err := store.Publish(ctx, id, roots[id]); err != nil {
			return nil, nil, fmt.Errorf("publish sandbox skill %q: %w", id, err)
		}
	}
	allowed := make(map[string]bool, len(users))
	for _, id := range users {
		if id == "" {
			return nil, nil, errors.New("empty canary user id")
		}
		allowed[id] = true
	}
	executor := &sandboxbroker.Executor{Policy: policy, SkillCatalog: sandboxbroker.PublishedCatalog{Store: store, Roots: roots}, Artifacts: store, Snapshot: builder, Node: sandboxbroker.DevelopmentNodeClient{}, AllowedUserIDs: allowed,
		ResolveCredential: func(ctx context.Context, ref string) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if ref != credentialRef {
				return "", errors.New("sandbox credential ref rejected")
			}
			value, err := readPrivateSandboxKey(tokenPath)
			return string(value), err
		},
	}
	handler, err := sandboxartifact.Handler(store, string(artifactKey))
	if err != nil {
		return nil, nil, err
	}
	return executor, handler, nil
}
func readPrivateSandboxKey(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("sandbox secret path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("sandbox secret must be private and regular")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) < 32 || len(raw) > 4096 {
		return nil, errors.New("invalid sandbox credential length")
	}
	return raw, nil
}

// A Node allowlist is not equivalent to the SQL/Admin publication state.
// Reject a mismatched deployment before serving chat traffic, rather than
// allowing an unknown-skill error on the first real user request.
func remotePublishedSkillRoots(manager *skills.SkillManager, published agentruntime.SkillCatalog, ids []string) (map[string]string, error) {
	if manager == nil || published == nil {
		return nil, errors.New("remote sandbox skill registry unavailable")
	}
	roots := make(map[string]string, len(ids))
	for _, id := range ids {
		skill, ok := manager.GetSkill(id)
		if !ok || skill == nil || skill.Name != id || skill.Source != skills.SourceFile || filepath.Base(skill.SkillRoot) != id {
			return nil, fmt.Errorf("sandbox skill %q has no direct local publication", id)
		}
		_, ok = published.GetSkill(id)
		if !ok {
			return nil, fmt.Errorf("sandbox skill %q must be published in Admin before enabling Node routing", id)
		}
		roots[id] = skill.SkillRoot
	}
	return roots, nil
}
