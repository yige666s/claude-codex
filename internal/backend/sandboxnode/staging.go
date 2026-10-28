package sandboxnode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"claude-codex/internal/backend/sandboxartifact"
	"claude-codex/internal/backend/sandboxtransfer"
)

// Stager is an offline Node-side input primitive. It deliberately cannot
// execute a command or mount a host path into a container. A future signed
// Node protocol must authenticate task identity and enforce admission first.
type Stager struct {
	Root   string
	Cache  *sandboxartifact.Cache
	Limits sandboxtransfer.Limits
}

type StagedTask struct {
	Skill         sandboxartifact.Ref
	SkillPath     string
	WorkspacePath string
	root          string
	once          sync.Once
	releaseErr    error
}

// Stage accepts only an archive with workspace/; the Skill always comes from
// the separately authenticated content-addressed cache. Partial extraction is
// discarded, never exposed to an executor.
func (s Stager) Stage(ctx context.Context, ref sandboxartifact.Ref, workspace []byte) (*StagedTask, error) {
	if s.Cache == nil || ctx == nil || ctx.Err() != nil {
		return nil, errors.New("node task staging unavailable")
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if err := validPrivateRoot(s.Root); err != nil {
		return nil, err
	}
	skillPath, err := s.Cache.Ensure(ctx, ref)
	if err != nil {
		return nil, err
	}
	taskDir, err := os.MkdirTemp(s.Root, "task-")
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*StagedTask, error) { _ = os.RemoveAll(taskDir); return nil, err }
	if err := os.Chmod(taskDir, 0700); err != nil {
		return fail(err)
	}
	inputDir := filepath.Join(taskDir, "inputs")
	if err := os.Mkdir(inputDir, 0700); err != nil {
		return fail(err)
	}
	if err := sandboxtransfer.ExtractWorkspace(workspace, inputDir, s.Limits); err != nil {
		return fail(err)
	}
	// Expose only the direct bind source to UID 65534 inside the container.
	// Parent task directory stays 0700, and the future OCI driver must bind
	// workspace at /input with readonly, never as a writable host directory.
	if err := makeInputReadable(filepath.Join(inputDir, "workspace")); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return &StagedTask{Skill: ref, SkillPath: skillPath, WorkspacePath: filepath.Join(inputDir, "workspace"), root: taskDir}, nil
}

// Release deletes this task's private input. This is not an output collection
// or durable orphan-reconciliation mechanism; neither may be skipped before
// admitting real jobs.
func (t *StagedTask) Release() error {
	if t == nil {
		return nil
	}
	t.once.Do(func() { t.releaseErr = os.RemoveAll(t.root) })
	return t.releaseErr
}
func validPrivateRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) {
		return errors.New("private node task root must be absolute")
	}
	clean := filepath.Clean(root)
	real, err := filepath.EvalSymlinks(clean)
	if err != nil || real != clean {
		return errors.New("private node task root may not contain symlinks")
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("private node task root must be owner-only")
	}
	return nil
}

func makeInputReadable(root string) error {
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
			return errors.New("invalid private input file")
		}
		if info.Mode()&0111 != 0 {
			return os.Chmod(path, 0555)
		}
		return os.Chmod(path, 0444)
	})
}

// RemoveOrphanedInputs is only safe under the exclusive process lock after
// all labeled containers have been forcibly removed and verified absent.
// An unfamiliar entry blocks admission rather than being removed.
func (s Stager) RemoveOrphanedInputs() (int, error) {
	if err := validPrivateRoot(s.Root); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return 0, err
	}
	var candidates []string
	for _, entry := range entries {
		name := entry.Name()
		if name == ".node.lock" {
			continue
		}
		if !strings.HasPrefix(name, "task-") || len(name) > 64 || len(name) < 8 {
			return 0, errors.New("unexpected Node scratch entry")
		}
		for _, r := range name[5:] {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				return 0, errors.New("invalid Node scratch name")
			}
		}
		info, err := os.Lstat(filepath.Join(s.Root, name))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return 0, errors.New("invalid Node scratch directory")
		}
		candidates = append(candidates, name)
	}
	for _, name := range candidates {
		if err := os.RemoveAll(filepath.Join(s.Root, name)); err != nil {
			return 0, err
		}
	}
	return len(candidates), nil
}
