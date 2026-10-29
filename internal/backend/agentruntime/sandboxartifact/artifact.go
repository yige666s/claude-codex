package sandboxartifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

// Ref identifies immutable bytes; a human-facing version label is never used
// for execution. A digest does not assert that the Skill is trusted.
type Ref struct {
	ID     string `json:"skill_id"`
	Digest string `json:"digest"`
}

func (r Ref) Validate() error {
	if len(r.ID) < 1 || len(r.ID) > 64 || r.ID[0] < 'a' || r.ID[0] > 'z' {
		return errors.New("invalid skill id")
	}
	for _, c := range r.ID {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return errors.New("invalid skill id")
		}
	}
	if !strings.HasPrefix(r.Digest, "sha256:") || len(r.Digest) != 71 {
		return errors.New("invalid skill digest")
	}
	raw, err := hex.DecodeString(r.Digest[7:])
	if err != nil || hex.EncodeToString(raw) != r.Digest[7:] {
		return errors.New("invalid skill digest")
	}
	return nil
}
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Store keeps published content-addressed archives and a mutable discovery
// pointer. Existing digest-addressed archives remain available after updates.
type Store struct {
	Dir     string
	Builder sandboxtransfer.Builder
}

func (s Store) Publish(ctx context.Context, id, skillRoot string) (Ref, error) {
	ref := Ref{ID: id, Digest: "sha256:" + strings.Repeat("0", 64)}
	if err := ref.Validate(); err != nil {
		return Ref{}, err
	}
	data, err := s.Builder.BuildSkill(ctx, skillRoot)
	if err != nil {
		return Ref{}, err
	}
	ref.Digest = Digest(data)
	if err := s.ensureDirs(); err != nil {
		return Ref{}, err
	}
	object := filepath.Join(s.Dir, "objects", ref.Digest[7:]+".tar")
	if err := writeImmutable(object, data); err != nil {
		return Ref{}, err
	}
	current, err := json.Marshal(ref)
	if err != nil {
		return Ref{}, err
	}
	versions := filepath.Join(s.Dir, "refs", id)
	if err := os.MkdirAll(versions, 0700); err != nil {
		return Ref{}, err
	}
	if info, err := os.Lstat(versions); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Ref{}, errors.New("skill version directory invalid")
	}
	if err := writeImmutable(filepath.Join(versions, ref.Digest[7:]+".json"), current); err != nil {
		return Ref{}, err
	}
	if err := writeAtomic(filepath.Join(s.Dir, "refs", id+".json"), current); err != nil {
		return Ref{}, err
	}
	return ref, nil
}
func (s Store) Resolve(id string) (Ref, error) {
	check := Ref{ID: id, Digest: "sha256:" + strings.Repeat("0", 64)}
	if err := check.Validate(); err != nil {
		return Ref{}, err
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, "refs", id+".json"))
	if err != nil {
		return Ref{}, err
	}
	var ref Ref
	if err := json.Unmarshal(data, &ref); err != nil {
		return Ref{}, err
	}
	if err := ref.Validate(); err != nil || ref.ID != id {
		return Ref{}, errors.New("invalid published skill reference")
	}
	if err := s.checkPublication(ref); err != nil {
		return Ref{}, err
	}
	return ref, nil
}
func (s Store) Get(ref Ref) ([]byte, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if err := s.checkPublication(ref); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Dir, "objects", ref.Digest[7:]+".tar")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("invalid skill artifact")
	}
	lim := s.Builder.Limits
	if info.Size() > lim.MaxBytes+int64(lim.MaxFiles*4096+2048) {
		return nil, errors.New("skill artifact too large")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if Digest(data) != ref.Digest {
		return nil, errors.New("skill artifact digest mismatch")
	}
	return data, nil
}
func (s Store) checkPublication(ref Ref) error {
	manifest := filepath.Join(s.Dir, "refs", ref.ID, ref.Digest[7:]+".json")
	manifestInfo, err := os.Lstat(manifest)
	if err != nil {
		return err
	}
	if !manifestInfo.Mode().IsRegular() {
		return errors.New("invalid skill publication")
	}
	published, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	var bound Ref
	if err := json.Unmarshal(published, &bound); err != nil || bound != ref {
		return errors.New("skill reference not published")
	}
	return nil
}
func (s Store) ensureDirs() error {
	if s.Dir == "" {
		return errors.New("artifact store not configured")
	}
	for _, dir := range []string{s.Dir, filepath.Join(s.Dir, "objects"), filepath.Join(s.Dir, "refs")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("artifact store path is not a real directory")
		}
	}
	return nil
}

// writeImmutable publishes via a hard link so concurrent publishers cannot
// replace an existing digest after validating it. The temporary inode is
// already fully written and synced before it becomes visible.
func writeImmutable(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".staging-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Link(f.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("invalid existing artifact")
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(existing) != len(data) || Digest(existing) != Digest(data) {
		return errors.New("existing artifact changed")
	}
	return nil
}
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".staging-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
