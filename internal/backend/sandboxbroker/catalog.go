package sandboxbroker

import (
	"context"
	"errors"
	"path/filepath"

	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/backend/sandboxartifact"
)

// PublishedCatalog explicitly binds a registered Skill ID to its source root.
// This mapping is provided by the trusted control plane, never by frontmatter
// or a user command. Native provenance is intentionally not inferred here.
type PublishedCatalog struct {
	Store sandboxartifact.Store
	Roots map[string]string
}

func (c PublishedCatalog) Resolve(ctx context.Context, req agentruntime.RemoteSkillRequest) (sandboxartifact.Ref, error) {
	if ctx == nil {
		return sandboxartifact.Ref{}, errors.New("missing catalog context")
	}
	if err := ctx.Err(); err != nil {
		return sandboxartifact.Ref{}, err
	}
	configured, ok := c.Roots[req.SkillName]
	if !ok || configured == "" || req.SkillRoot == "" {
		return sandboxartifact.Ref{}, errors.New("skill is not registered")
	}
	expected, err := filepath.Abs(configured)
	if err != nil {
		return sandboxartifact.Ref{}, err
	}
	actual, err := filepath.Abs(req.SkillRoot)
	if err != nil || filepath.Clean(expected) != filepath.Clean(actual) {
		return sandboxartifact.Ref{}, errors.New("skill root differs from registered release")
	}
	// Resolve reads the already published ref; it never archives the live
	// directory. Per-job pinning will replace this lookup when leases exist.
	return c.Store.Resolve(req.SkillName)
}
