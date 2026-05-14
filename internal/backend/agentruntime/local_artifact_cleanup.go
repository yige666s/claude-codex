package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const generatedArtifactStagingDir = "generated-artifacts"

type LocalArtifactPruneResult struct {
	Checked int
	Deleted int
	Skipped int
	Errors  int
}

func (r *Runtime) PruneLocalUploadedArtifacts(ctx context.Context, olderThan time.Duration) (LocalArtifactPruneResult, error) {
	var result LocalArtifactPruneResult
	if r == nil || r.artifacts == nil || r.artifacts.Store == nil || olderThan <= 0 || strings.TrimSpace(r.config.UserWorkspaceRoot) == "" {
		return result, nil
	}
	lister, ok := r.artifacts.Store.(uploadedArtifactLister)
	if !ok {
		return result, nil
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	artifacts, err := lister.ListUploadedArtifactsBefore(ctx, cutoff)
	if err != nil {
		return result, err
	}
	var errs []error
	for _, artifact := range artifacts {
		result.Checked++
		deleted, err := r.pruneLocalUploadedArtifact(ctx, artifact, cutoff)
		if err != nil {
			result.Errors++
			errs = append(errs, err)
			continue
		}
		if deleted {
			result.Deleted++
		} else {
			result.Skipped++
		}
	}
	return result, errors.Join(errs...)
}

func (r *Runtime) pruneLocalUploadedArtifact(ctx context.Context, artifact *Artifact, cutoff time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if artifact == nil || normalizeAssetKind(artifact.Kind) != AssetKindArtifact || strings.TrimSpace(artifact.ObjectKey) == "" {
		return false, nil
	}
	if artifact.DeletedAt != nil || !artifact.CreatedAt.IsZero() && !artifact.CreatedAt.Before(cutoff) {
		return false, nil
	}
	userID := strings.TrimSpace(artifact.UserID)
	filename := filepath.Base(strings.TrimSpace(artifact.Filename))
	if userID == "" || filename == "" || filename == "." || filename == string(filepath.Separator) {
		return false, nil
	}
	dir := filepath.Join(r.userWorkspace(userID), generatedArtifactStagingDir)
	path := filepath.Join(dir, filename)
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("refuse to prune artifact outside staging directory: %s", filename)
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.IsDir() || info.ModTime().After(cutoff) {
		return false, nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	_ = os.Remove(dir)
	return true, nil
}
