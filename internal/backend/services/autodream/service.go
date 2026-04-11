package autodream

import (
	"os"
	"path/filepath"
	"time"

	"claude-codex/internal/harness/state"
)

type Config struct {
	MinHours     time.Duration
	MinSessions  int
	ScanThrottle time.Duration
}

func DefaultConfig() Config {
	return Config{
		MinHours:     24 * time.Hour,
		MinSessions:  5,
		ScanThrottle: 10 * time.Minute,
	}
}

type Service struct {
	cfg        Config
	lastScanAt time.Time
}

func NewService(cfg Config) *Service {
	if cfg.MinHours <= 0 {
		cfg = DefaultConfig()
	}
	return &Service{cfg: cfg}
}

func (s *Service) ShouldRun(lastConsolidatedAt time.Time, sessions []*state.Session) bool {
	now := time.Now()
	if now.Sub(lastConsolidatedAt) < s.cfg.MinHours {
		return false
	}
	if !s.lastScanAt.IsZero() && now.Sub(s.lastScanAt) < s.cfg.ScanThrottle {
		return false
	}
	s.lastScanAt = now
	return len(sessions) >= s.cfg.MinSessions
}

func (s *Service) Prompt(sessionIDs []string) string {
	result := "Consolidate memory from recent sessions:\n"
	for _, id := range sessionIDs {
		result += "- " + id + "\n"
	}
	return result
}

func LockPath(baseDir string) string {
	return filepath.Join(baseDir, ".autodream.lock")
}

func TryAcquireLock(baseDir string) (bool, error) {
	path := LockPath(baseDir)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return false, nil
		}
		return false, err
	}
	_ = file.Close()
	return true, nil
}

func ReleaseLock(baseDir string) error {
	err := os.Remove(LockPath(baseDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
