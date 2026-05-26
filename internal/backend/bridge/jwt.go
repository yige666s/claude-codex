package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	backendretry "claude-codex/internal/backend/retry"
	"claude-codex/internal/backend/workers"
)

const (
	DefaultRefreshBuffer        = 5 * time.Minute
	DefaultFallbackRefreshDelay = 30 * time.Minute
	DefaultRefreshRetryDelay    = 1 * time.Minute
	DefaultMaxRefreshFailures   = 3
)

type AccessTokenProvider func() (string, error)
type RefreshCallback func(sessionID, accessToken string)

type TokenRefreshScheduler struct {
	provider             AccessTokenProvider
	onRefresh            RefreshCallback
	refreshBuffer        time.Duration
	fallbackRefreshDelay time.Duration
	retryDelay           time.Duration
	maxFailures          int
	retryPolicy          backendretry.Policy
	ctx                  context.Context
	cancel               context.CancelFunc
	workers              *workers.Group

	mu          sync.Mutex
	cancels     map[string]context.CancelFunc
	failures    map[string]int
	generations map[string]int64
}

func DecodeJWTPayload(token string) (map[string]any, bool) {
	jwt := strings.TrimSpace(token)
	jwt = strings.TrimPrefix(jwt, "sk-ant-si-")
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 || parts[1] == "" {
		return nil, false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func DecodeJWTExpiry(token string) (int64, bool) {
	payload, ok := DecodeJWTPayload(token)
	if !ok {
		return 0, false
	}
	switch value := payload["exp"].(type) {
	case float64:
		return int64(value), true
	case int64:
		return value, true
	case json.Number:
		exp, err := value.Int64()
		return exp, err == nil
	default:
		return 0, false
	}
}

func NewTokenRefreshScheduler(provider AccessTokenProvider, onRefresh RefreshCallback) *TokenRefreshScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &TokenRefreshScheduler{
		provider:             provider,
		onRefresh:            onRefresh,
		refreshBuffer:        DefaultRefreshBuffer,
		fallbackRefreshDelay: DefaultFallbackRefreshDelay,
		retryDelay:           DefaultRefreshRetryDelay,
		maxFailures:          DefaultMaxRefreshFailures,
		retryPolicy: backendretry.Policy{
			MaxAttempts: DefaultMaxRefreshFailures,
			BaseDelay:   DefaultRefreshRetryDelay,
			MaxDelay:    DefaultRefreshRetryDelay,
		},
		ctx:         ctx,
		cancel:      cancel,
		workers:     workers.New(ctx, slog.Default().With(slog.String("component", "token_refresh_scheduler"))),
		cancels:     make(map[string]context.CancelFunc),
		failures:    make(map[string]int),
		generations: make(map[string]int64),
	}
}

func (s *TokenRefreshScheduler) Schedule(sessionID, token string) {
	expiry, ok := DecodeJWTExpiry(token)
	if !ok {
		return
	}
	delay := time.Until(time.Unix(expiry, 0)) - s.refreshBuffer
	s.scheduleWithDelay(sessionID, delay)
}

func (s *TokenRefreshScheduler) ScheduleFromExpiresIn(sessionID string, expiresIn time.Duration) {
	delay := expiresIn - s.refreshBuffer
	s.scheduleWithDelay(sessionID, delay)
}

func (s *TokenRefreshScheduler) Cancel(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bumpGenerationLocked(sessionID)
	if cancel := s.cancels[sessionID]; cancel != nil {
		cancel()
		delete(s.cancels, sessionID)
	}
	delete(s.failures, sessionID)
}

func (s *TokenRefreshScheduler) CancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sessionID, cancel := range s.cancels {
		if cancel != nil {
			cancel()
		}
		s.bumpGenerationLocked(sessionID)
	}
	s.cancels = make(map[string]context.CancelFunc)
	s.failures = make(map[string]int)
}

func (s *TokenRefreshScheduler) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.CancelAll()
	s.cancel()
	if ctx == nil {
		ctx = context.Background()
	}
	return s.workers.Stop(ctx)
}

func (s *TokenRefreshScheduler) scheduleWithDelay(sessionID string, delay time.Duration) {
	if delay <= 0 {
		delay = 30 * time.Second
	}

	s.mu.Lock()
	if cancel := s.cancels[sessionID]; cancel != nil {
		cancel()
	}
	gen := s.bumpGenerationLocked(sessionID)
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancels[sessionID] = cancel
	name := fmt.Sprintf("token_refresh_%s_%d", sanitizeWorkerName(sessionID), gen)
	s.mu.Unlock()

	s.workers.Start(name, func(context.Context) error {
		if err := backendretry.Sleep(ctx, delay); err != nil {
			return nil
		}
		s.refreshLoop(ctx, sessionID, gen)
		return nil
	})
}

func (s *TokenRefreshScheduler) refreshLoop(ctx context.Context, sessionID string, generation int64) {
	for {
		if !s.isCurrentGeneration(sessionID, generation) {
			return
		}

		accessToken, err := s.provider()
		if err != nil || strings.TrimSpace(accessToken) == "" {
			failure, keepGoing := s.recordRefreshFailure(sessionID, generation)
			if !keepGoing {
				return
			}
			if sleepErr := s.refreshRetryPolicy().Sleep(ctx, failure, err); sleepErr != nil {
				return
			}
			continue
		}

		s.onRefresh(sessionID, accessToken)
		s.clearRefreshFailure(sessionID, generation)
		if err := backendretry.Sleep(ctx, s.fallbackRefreshDelay); err != nil {
			return
		}
	}
}

func (s *TokenRefreshScheduler) bumpGenerationLocked(sessionID string) int64 {
	s.generations[sessionID]++
	return s.generations[sessionID]
}

func (s *TokenRefreshScheduler) isCurrentGeneration(sessionID string, generation int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generations[sessionID] == generation
}

func (s *TokenRefreshScheduler) recordRefreshFailure(sessionID string, generation int64) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generations[sessionID] != generation {
		return 0, false
	}
	s.failures[sessionID]++
	failures := s.failures[sessionID]
	if failures >= s.maxFailures {
		if cancel := s.cancels[sessionID]; cancel != nil {
			cancel()
		}
		delete(s.cancels, sessionID)
		return failures, false
	}
	return failures, true
}

func (s *TokenRefreshScheduler) clearRefreshFailure(sessionID string, generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generations[sessionID] != generation {
		return
	}
	delete(s.failures, sessionID)
}

func (s *TokenRefreshScheduler) refreshRetryPolicy() backendretry.Policy {
	policy := s.retryPolicy
	policy.MaxAttempts = 1
	if s.retryDelay > 0 {
		policy.BaseDelay = s.retryDelay
		policy.MaxDelay = s.retryDelay
	}
	return policy
}

func sanitizeWorkerName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "session"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func AccessTokenProviderFromContext(ctx context.Context, fn func(context.Context) (string, error)) AccessTokenProvider {
	return func() (string, error) {
		if ctx == nil {
			return "", errors.New("nil context")
		}
		return fn(ctx)
	}
}
