package vcr

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"claude-codex/internal/public/fsutil"
)

type Service struct {
	rootDir string
	record  bool
	ci      bool
}

func NewService(rootDir string, record bool, ci bool) *Service {
	return &Service{rootDir: rootDir, record: record, ci: ci}
}

func (s *Service) WithFixture(ctx context.Context, input any, fixtureName string, fn func(context.Context) (any, error)) (any, error) {
	filename, err := s.fixturePath(input, fixtureName)
	if err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(filename); err == nil {
		var cached any
		if err := json.Unmarshal(data, &cached); err == nil {
			return cached, nil
		}
	}

	if s.ci && !s.record {
		return nil, fmt.Errorf("fixture missing: %s", filename)
	}
	result, err := fn(ctx)
	if err != nil {
		return nil, err
	}
	if s.record {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := fsutil.WriteFileAtomic(filename, data, 0o644); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Service) fixturePath(input any, fixtureName string) (string, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum(payload)
	return filepath.Join(s.rootDir, "fixtures", fixtureName+"-"+hex.EncodeToString(sum[:])[:12]+".json"), nil
}
