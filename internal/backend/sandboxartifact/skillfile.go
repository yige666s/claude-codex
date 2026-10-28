package sandboxartifact

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
)

// SkillFile reads the exact SKILL.md from a verified, immutable publication.
// Store.Get validates both the ID/digest binding and archive bytes first.
func (s Store) SkillFile(ref Ref) ([]byte, error) {
	data, err := s.Get(ref)
	if err != nil {
		return nil, err
	}
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name != "skill/SKILL.md" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, errors.New("invalid published skill entrypoint")
		}
		if hdr.Size < 0 || hdr.Size > s.Builder.Limits.MaxFileBytes {
			return nil, errors.New("published skill entrypoint too large")
		}
		return io.ReadAll(io.LimitReader(reader, hdr.Size+1))
	}
	return nil, errors.New("published skill entrypoint missing")
}
