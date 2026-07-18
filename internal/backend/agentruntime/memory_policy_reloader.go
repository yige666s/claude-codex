package agentruntime

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type MemoryPolicyFileReloaderConfig struct {
	Path            string
	ExpectedVersion string
	ReloadInterval  time.Duration
	StrictEval      bool
	InitialPolicy   MemoryPolicy
	Logger          *slog.Logger
}

type MemoryPolicyFileReloader struct {
	path            string
	expectedVersion string
	reloadInterval  time.Duration
	strictEval      bool
	logger          *slog.Logger

	mu           sync.RWMutex
	policy       MemoryPolicy
	fingerprint  string
	lastLoadedAt time.Time
}

func NewMemoryPolicyFileReloader(config MemoryPolicyFileReloaderConfig) (*MemoryPolicyFileReloader, error) {
	path := strings.TrimSpace(config.Path)
	policy := normalizeMemoryPolicy(config.InitialPolicy)
	fingerprint := ""
	if path != "" {
		loaded, gotFingerprint, err := loadMemoryPolicyFileWithFingerprint(path, config.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if err := ValidateMemoryPolicyForStartup(loaded, config.StrictEval); err != nil {
			return nil, err
		}
		policy = loaded
		fingerprint = gotFingerprint
	} else if err := ValidateMemoryPolicyForStartup(policy, config.StrictEval); err != nil {
		return nil, err
	}
	if fingerprint == "" {
		fingerprint = "builtin:" + policy.Version
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryPolicyFileReloader{
		path:            path,
		expectedVersion: strings.TrimSpace(config.ExpectedVersion),
		reloadInterval:  config.ReloadInterval,
		strictEval:      config.StrictEval,
		logger:          logger.With(slog.String("component", "memory_policy_reloader")),
		policy:          policy,
		fingerprint:     fingerprint,
		lastLoadedAt:    time.Now().UTC(),
	}, nil
}

func (r *MemoryPolicyFileReloader) MemoryPolicy() MemoryPolicy {
	if r == nil {
		return DefaultMemoryPolicy()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return normalizeMemoryPolicy(r.policy)
}

func (r *MemoryPolicyFileReloader) LastLoadedAt() time.Time {
	if r == nil {
		return time.Time{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastLoadedAt
}

func (r *MemoryPolicyFileReloader) Run(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(r.path) == "" || r.reloadInterval <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	ticker := time.NewTicker(r.reloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			changed, err := r.ReloadIfChanged(ctx)
			if err != nil {
				if r.logger != nil {
					r.logger.WarnContext(ctx, "memory policy reload skipped", slog.String("path", r.path), slog.Any("error", err))
				}
				continue
			}
			if changed && r.logger != nil {
				r.logger.InfoContext(ctx, "memory policy reloaded", slog.String("path", r.path), slog.String("version", r.MemoryPolicy().Version))
			}
		}
	}
}

func (r *MemoryPolicyFileReloader) ReloadIfChanged(ctx context.Context) (bool, error) {
	if r == nil || strings.TrimSpace(r.path) == "" {
		return false, nil
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}
	policy, fingerprint, err := loadMemoryPolicyFileWithFingerprint(r.path, r.expectedVersion)
	if err != nil {
		return false, err
	}
	r.mu.RLock()
	unchanged := fingerprint == r.fingerprint
	r.mu.RUnlock()
	if unchanged {
		return false, nil
	}
	if err := ValidateMemoryPolicyForStartup(policy, r.strictEval); err != nil {
		return false, err
	}
	r.mu.Lock()
	r.policy = policy
	r.fingerprint = fingerprint
	r.lastLoadedAt = time.Now().UTC()
	r.mu.Unlock()
	return true, nil
}
