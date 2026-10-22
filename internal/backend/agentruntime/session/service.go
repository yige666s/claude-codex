package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-codex/internal/harness/state"
)

// Store is the persistence boundary required by the session module. The
// concrete store can be file-backed, PostgreSQL-backed, or an in-memory test
// double without leaking those details into the runtime facade.
type Store interface {
	Create(ctx context.Context, userID, workingDir string) (*state.Session, error)
	Get(ctx context.Context, userID, sessionID string) (*state.Session, error)
	List(ctx context.Context, userID string) ([]*state.Session, error)
	Save(ctx context.Context, userID string, session *state.Session) error
	Delete(ctx context.Context, userID, sessionID string) error
}

// PageStore is an optional pagination capability implemented by database
// stores. The service falls back to in-memory paging when it is unavailable.
type PageStore interface {
	ListPage(ctx context.Context, userID string, limit, offset int) ([]*state.Session, error)
}

// Config contains only session-owned workspace policy. Agent execution and
// memory settings intentionally do not belong here.
type Config struct {
	UserWorkspaceRoot    string
	DefaultWorkingDir    string
	AllowCustomWorkingDir bool
	WorkspaceForUser     func(userID string) string
}

// Sanitizer hides internal transcript messages before a session is returned.
// It is injected as a policy callback to keep the session package independent
// from Skills and Agent Runtime types.
type Sanitizer func(*state.Session) bool

// DeleteHooks run after the durable session row has been deleted. Hooks are
// deliberately ordered to preserve the existing Runtime behavior.
type DeleteHooks struct {
	DeleteSearchIndex  func(context.Context, string, string) error
	InvalidateContext  func(context.Context, string, string) error
	DeleteLoopTriggers func(context.Context, string, string) error
}

// Service owns session CRUD, workspace selection, transcript sanitization,
// and deletion side effects. Runtime remains a compatibility facade.
type Service struct {
	store    Store
	config   Config
	sanitize Sanitizer
	hooks    DeleteHooks
}

func New(store Store, config Config, sanitize Sanitizer, hooks DeleteHooks) *Service {
	return &Service{store: store, config: config, sanitize: sanitize, hooks: hooks}
}

func (s *Service) requireStore() (Store, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("session store is required")
	}
	return s.store, nil
}

func (s *Service) Create(ctx context.Context, userID, workingDir string) (*state.Session, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	if s.config.UserWorkspaceRoot != "" {
		if s.config.WorkspaceForUser == nil {
			return nil, fmt.Errorf("workspace resolver is required")
		}
		workingDir = s.config.WorkspaceForUser(userID)
		if err := os.MkdirAll(workingDir, 0o755); err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(workingDir) == "" || !s.config.AllowCustomWorkingDir {
		workingDir = s.config.DefaultWorkingDir
	}
	workingDir = filepath.Clean(workingDir)
	return store.Create(ctx, userID, workingDir)
}

func (s *Service) List(ctx context.Context, userID string) ([]*state.Session, error) {
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	sessions, err := store.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.sanitizeAndPersist(ctx, userID, sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *Service) ListPage(ctx context.Context, userID string, limit, offset int) ([]*state.Session, error) {
	if limit <= 0 && offset <= 0 {
		return s.List(ctx, userID)
	}
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	if pager, ok := store.(PageStore); ok && pager != nil {
		sessions, err := pager.ListPage(ctx, userID, limit, offset)
		if err != nil {
			return nil, err
		}
		if err := s.sanitizeAndPersist(ctx, userID, sessions); err != nil {
			return nil, err
		}
		return sessions, nil
	}
	sessions, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(sessions) {
		return []*state.Session{}, nil
	}
	if limit <= 0 {
		return sessions[offset:], nil
	}
	end := offset + limit
	if end > len(sessions) {
		end = len(sessions)
	}
	return sessions[offset:end], nil
}

func (s *Service) Get(ctx context.Context, userID, sessionID string) (*state.Session, error) {
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	session, err := store.Get(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session != nil && s.sanitize != nil && s.sanitize(session) {
		if err := store.Save(ctx, userID, session); err != nil {
			return nil, err
		}
	}
	return session, nil
}

func (s *Service) Delete(ctx context.Context, userID, sessionID string) error {
	store, err := s.requireStore()
	if err != nil {
		return err
	}
	if err := store.Delete(ctx, userID, sessionID); err != nil {
		return err
	}
	if hook := s.hooks.DeleteSearchIndex; hook != nil {
		if err := hook(ctx, userID, sessionID); err != nil {
			return err
		}
	}
	if hook := s.hooks.InvalidateContext; hook != nil {
		_ = hook(ctx, userID, sessionID)
	}
	if hook := s.hooks.DeleteLoopTriggers; hook != nil {
		_ = hook(ctx, userID, sessionID)
	}
	return nil
}

func (s *Service) sanitizeAndPersist(ctx context.Context, userID string, sessions []*state.Session) error {
	if s == nil || s.sanitize == nil {
		return nil
	}
	store, err := s.requireStore()
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session != nil && s.sanitize(session) {
			if err := store.Save(ctx, userID, session); err != nil {
				return err
			}
		}
	}
	return nil
}
