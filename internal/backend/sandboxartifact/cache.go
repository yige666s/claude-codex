package sandboxartifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"claude-codex/internal/backend/sandboxtransfer"
)

type Fetcher interface {
	Fetch(context.Context, Ref) ([]byte, error)
}
type FetchFunc func(context.Context, Ref) ([]byte, error)

func (f FetchFunc) Fetch(ctx context.Context, ref Ref) ([]byte, error) { return f(ctx, ref) }

// Cache is private to the Node process, never writable or mounted read-write
// inside a sandbox. The returned path must only be mounted read-only.
type Cache struct {
	Dir    string
	Limits sandboxtransfer.Limits
	Source Fetcher
	// MaxEntries bounds persistent cache growth. Eviction is deliberately
	// deferred until task leases can pin in-use artifacts safely.
	MaxEntries int
	mu         sync.Mutex
}

func (c *Cache) Ensure(ctx context.Context, ref Ref) (string, error) {
	if c == nil || c.Source == nil || c.Dir == "" {
		return "", errors.New("skill cache not configured")
	}
	if err := ref.Validate(); err != nil {
		return "", err
	}
	if ctx == nil {
		return "", errors.New("skill cache context required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		return "", err
	}
	resolved, resolveErr := filepath.EvalSymlinks(c.Dir)
	info, err := os.Lstat(c.Dir)
	if resolveErr != nil || resolved != filepath.Clean(c.Dir) || err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("skill cache root is not a real directory")
	}
	destination := filepath.Join(c.Dir, ref.Digest[7:])
	if _, err := os.Lstat(destination); err == nil {
		path := filepath.Join(destination, "skill")
		// Check local bytes as well as metadata: a damaged or replaced cache
		// entry must not silently become a different Skill at the same digest.
		builder := sandboxtransfer.Builder{SkillRoots: []string{c.Dir}, Limits: c.Limits}
		existing, err := builder.BuildSkill(ctx, path)
		if err != nil || Digest(existing) != ref.Digest {
			return "", errors.New("cached skill failed integrity check")
		}
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	maxEntries := c.MaxEntries
	if maxEntries == 0 {
		maxEntries = 64
	}
	if maxEntries < 0 || maxEntries > 1024 {
		return "", errors.New("invalid skill cache capacity")
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return "", err
	}
	if len(entries) >= maxEntries {
		return "", errors.New("skill cache capacity reached")
	}
	archive, err := c.Source.Fetch(ctx, ref)
	if err != nil {
		return "", err
	}
	if len(archive) > int(c.Limits.MaxBytes)+c.Limits.MaxFiles*4096+2048 {
		return "", errors.New("downloaded skill exceeds cache limits")
	}
	if Digest(archive) != ref.Digest {
		return "", errors.New("downloaded skill digest mismatch")
	}
	stage, err := os.MkdirTemp(c.Dir, ".skill-staging-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if err := sandboxtransfer.ExtractSkill(archive, stage, c.Limits); err != nil {
		return "", err
	}
	path := filepath.Join(stage, "skill")
	builder := sandboxtransfer.Builder{SkillRoots: []string{c.Dir}, Limits: c.Limits}
	unpacked, err := builder.BuildSkill(ctx, path)
	if err != nil || Digest(unpacked) != ref.Digest {
		return "", errors.New("unpacked skill digest mismatch")
	}
	if err := makeReadOnly(path); err != nil {
		return "", err
	}
	if err := os.Chmod(stage, 0700); err != nil {
		return "", err
	}
	if err := os.Rename(stage, destination); err != nil {
		return "", err
	}
	return filepath.Join(destination, "skill"), nil
}
func makeReadOnly(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, 0755)
		}
		if !info.Mode().IsRegular() {
			return errors.New("nonregular file in skill cache")
		}
		// Preserve executable scripts but don't turn every artifact into code.
		// The OCI driver must still mount this tree read-only.
		if info.Mode()&0111 != 0 {
			return os.Chmod(path, 0555)
		}
		return os.Chmod(path, 0444)
	})
}
