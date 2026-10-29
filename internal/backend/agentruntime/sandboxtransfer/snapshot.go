package sandboxtransfer

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	HardMaxFiles     = 512
	HardMaxBytes     = 8 << 20
	HardMaxFileBytes = 4 << 20
)

type Limits struct {
	MaxFiles     int
	MaxBytes     int64
	MaxFileBytes int64
}

func (l Limits) validate() error {
	if l.MaxFiles < 1 || l.MaxFiles > HardMaxFiles || l.MaxBytes < 1 || l.MaxBytes > HardMaxBytes || l.MaxFileBytes < 1 || l.MaxFileBytes > HardMaxFileBytes || l.MaxFileBytes > l.MaxBytes {
		return errors.New("invalid task snapshot limits")
	}
	return nil
}
func DevLimits() Limits { return Limits{MaxFiles: 256, MaxBytes: 8 << 20, MaxFileBytes: 4 << 20} }

type Builder struct {
	WorkspaceRoot string
	SkillRoots    []string
	// ExcludedWorkspaceDirs are top-level generated outputs, not per-task inputs.
	// Callers must configure them explicitly; each name is validated.
	ExcludedWorkspaceDirs []string
	Limits                Limits
}

func (b Builder) Build(ctx context.Context, workspace, skillRoot string) ([]byte, error) {
	if err := b.Limits.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("snapshot context required")
	}
	if err := withinConfiguredRoot(workspace, b.WorkspaceRoot, false); err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	workRoot, err := openConstrainedRoot(b.WorkspaceRoot, workspace)
	if err != nil {
		return nil, err
	}
	defer workRoot.Close()
	var skillHandle *os.Root
	if withinConfiguredRoot(skillRoot, workspace, true) == nil {
		rel, relErr := filepath.Rel(workspace, skillRoot)
		if relErr != nil {
			return nil, relErr
		}
		skillHandle, err = workRoot.OpenRoot(rel)
	} else {
		for _, allowed := range b.SkillRoots {
			if withinConfiguredRoot(skillRoot, allowed, false) == nil {
				skillHandle, err = openConstrainedRoot(allowed, skillRoot)
				break
			}
		}
	}
	if skillHandle == nil || err != nil {
		return nil, errors.New("skill path outside configured roots")
	}
	defer skillHandle.Close()
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	count := 0
	var total int64
	if err = walkRoot(ctx, writer, workRoot, "workspace", b.Limits, &count, &total, nil); err != nil {
		return nil, err
	}
	if err = walkRoot(ctx, writer, skillHandle, "skill", b.Limits, &count, &total, nil); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	// Tar padding is limited separately from actual file content.
	if buf.Len() > int(b.Limits.MaxBytes)+b.Limits.MaxFiles*4096+2048 {
		return nil, errors.New("task snapshot archive exceeds limit")
	}
	return buf.Bytes(), nil
}

// BuildWorkspace transfers per-task data without repackaging published Skills.
func (b Builder) BuildWorkspace(ctx context.Context, workspace string) ([]byte, error) {
	if err := b.Limits.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("snapshot context required")
	}
	if err := withinConfiguredRoot(workspace, b.WorkspaceRoot, false); err != nil {
		return nil, err
	}
	root, err := openConstrainedRoot(b.WorkspaceRoot, workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	excluded, err := workspaceExclusions(b.ExcludedWorkspaceDirs)
	if err != nil {
		return nil, err
	}
	return buildArchive(ctx, root, "workspace", b.Limits, excluded)
}

// BuildSkill runs at publication, not on the request hot path. Only configured
// Skill roots may be published; a workspace path cannot assert release trust.
func (b Builder) BuildSkill(ctx context.Context, skillRoot string) ([]byte, error) {
	if err := b.Limits.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("snapshot context required")
	}
	for _, allowed := range b.SkillRoots {
		if withinConfiguredRoot(skillRoot, allowed, false) == nil {
			root, err := openConstrainedRoot(allowed, skillRoot)
			if err != nil {
				return nil, err
			}
			defer root.Close()
			info, err := root.Lstat("SKILL.md")
			if err != nil || !info.Mode().IsRegular() {
				return nil, errors.New("skill entrypoint missing or invalid")
			}
			return buildArchive(ctx, root, "skill", b.Limits, nil)
		}
	}
	return nil, errors.New("skill path outside configured roots")
}

func buildArchive(ctx context.Context, root *os.Root, prefix string, lim Limits, excluded map[string]bool) ([]byte, error) {
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	count := 0
	var total int64
	if err := walkRoot(ctx, writer, root, prefix, lim, &count, &total, excluded); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > int(lim.MaxBytes)+lim.MaxFiles*4096+2048 {
		return nil, errors.New("snapshot archive exceeds limit")
	}
	return buf.Bytes(), nil
}

func workspaceExclusions(names []string) (map[string]bool, error) {
	if len(names) > 16 {
		return nil, errors.New("too many workspace exclusions")
	}
	excluded := make(map[string]bool, len(names))
	for _, name := range names {
		if len(name) == 0 || len(name) > 64 || name == "." || name == ".." || strings.ContainsAny(name, string([]byte{0, 47, 92, 58})) || excluded[name] {
			return nil, errors.New("invalid workspace exclusion")
		}
		for _, r := range name {
			if r < 32 || r == 127 {
				return nil, errors.New("invalid workspace exclusion")
			}
		}
		excluded[name] = true
	}
	return excluded, nil
}

func withinConfiguredRoot(target, base string, allowBase bool) error {
	if target == "" || base == "" {
		return errors.New("configured path missing")
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || (!allowBase && rel == ".") {
		return errors.New("path escapes configured root")
	}
	// The root and every component must be an actual directory, not a symlink.
	if err := rejectSymlinkAncestors(base); err != nil {
		return err
	}
	info, err := os.Lstat(base)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("configured root is not a real directory")
	}
	current := base
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			info, err = os.Lstat(current)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("snapshot root path contains symlink or non-directory")
			}
		}
	}
	return nil
}

// rejectSymlinkAncestors checks the configured root itself and each parent;
// checking only the final directory would miss an aliased sensitive mount.
func rejectSymlinkAncestors(absolute string) error {
	volume := filepath.VolumeName(absolute)
	current := volume + string(filepath.Separator)
	rest := strings.TrimPrefix(absolute, current)
	for _, part := range strings.Split(rest, string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("configured root ancestor is symlink")
		}
	}
	return nil
}

// openConstrainedRoot anchors target resolution under an already opened
// configured root so a renamed/symlinked target cannot escape after checks.
func openConstrainedRoot(base, target string) (*os.Root, error) {
	if err := withinConfiguredRoot(target, base, true); err != nil {
		return nil, err
	}
	before, err := os.Lstat(base)
	if err != nil {
		return nil, err
	}
	parent, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	opened, err := parent.Stat(".")
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, opened) {
		return nil, errors.New("configured root changed while opening")
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return nil, err
	}
	child, err := parent.OpenRoot(rel)
	if err != nil {
		return nil, err
	}
	return child, nil
}

func walkRoot(ctx context.Context, w *tar.Writer, root *os.Root, prefix string, lim Limits, count *int, total *int64, excluded map[string]bool) error {
	var visit func(string) error
	visit = func(rel string) error {
		if prefix == "workspace" && rel != "." && !strings.ContainsRune(rel, filepath.Separator) && excluded[rel] {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		archiveName := prefix
		if rel != "." {
			archiveName += "/" + filepath.ToSlash(rel)
		}
		if _, err := validArchivePath(archiveName); err != nil {
			return err
		}
		info, err := root.Lstat(rel)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("task snapshot rejects special file %q", archiveName)
		}
		if err = checkHardlink(info); err != nil {
			return fmt.Errorf("%s: %w", archiveName, err)
		}
		*count++
		if *count > lim.MaxFiles {
			return errors.New("task snapshot file count exceeded")
		}
		header := &tar.Header{Name: archiveName, Uid: 65534, Gid: 65534}
		if info.IsDir() {
			header.Typeflag = tar.TypeDir
			header.Name += "/"
			header.Mode = 0755
		} else {
			if info.Size() > lim.MaxFileBytes || info.Size() < 0 {
				return errors.New("task snapshot file too large")
			}
			*total += info.Size()
			if *total > lim.MaxBytes {
				return errors.New("task snapshot byte budget exceeded")
			}
			header.Typeflag = tar.TypeReg
			header.Mode = 0644
			if info.Mode()&0111 != 0 {
				header.Mode = 0755
			}
			header.Size = info.Size()
		}
		if err = w.WriteHeader(header); err != nil {
			return err
		}
		file, err := root.Open(rel)
		if err != nil {
			return err
		}
		opened, err := file.Stat()
		if err != nil {
			file.Close()
			return err
		}
		if !os.SameFile(info, opened) {
			file.Close()
			return errors.New("task snapshot path changed while reading")
		}
		if !info.IsDir() {
			_, err = io.CopyN(w, file, info.Size())
			closeErr := file.Close()
			if err != nil {
				return err
			}
			return closeErr
		}
		entries, err := file.ReadDir(-1)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			child := entry.Name()
			if child == "" || child == "." || child == ".." || strings.ContainsAny(child, "/\\") {
				return errors.New("invalid task snapshot directory entry")
			}
			if rel != "." {
				child = filepath.Join(rel, child)
			}
			if err = visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(".")
}

func validArchivePath(name string) (string, error) {
	cleaned := strings.TrimSuffix(name, "/")
	if cleaned == "" || len(cleaned) > 240 || strings.ContainsAny(cleaned, "\\:\x00") || strings.HasPrefix(cleaned, "/") || path.Clean(cleaned) != cleaned {
		return "", errors.New("invalid task snapshot path")
	}
	for _, r := range cleaned {
		if r < 32 || r == 127 {
			return "", errors.New("task snapshot path contains control character")
		}
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "." || part == ".." || part == "" {
			return "", errors.New("invalid task snapshot component")
		}
	}
	if cleaned != "skill" && cleaned != "workspace" && !strings.HasPrefix(cleaned, "skill/") && !strings.HasPrefix(cleaned, "workspace/") {
		return "", errors.New("task snapshot has unapproved root")
	}
	if strings.Count(cleaned, "/") > 16 {
		return "", errors.New("task snapshot path too deep")
	}
	return cleaned, nil
}
