package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
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

	mu          sync.Mutex
	timers      map[string]*time.Timer
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
	return &TokenRefreshScheduler{
		provider:             provider,
		onRefresh:            onRefresh,
		refreshBuffer:        DefaultRefreshBuffer,
		fallbackRefreshDelay: DefaultFallbackRefreshDelay,
		retryDelay:           DefaultRefreshRetryDelay,
		maxFailures:          DefaultMaxRefreshFailures,
		timers:               make(map[string]*time.Timer),
		failures:             make(map[string]int),
		generations:          make(map[string]int64),
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
	if timer := s.timers[sessionID]; timer != nil {
		timer.Stop()
		delete(s.timers, sessionID)
	}
	delete(s.failures, sessionID)
}

func (s *TokenRefreshScheduler) CancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sessionID, timer := range s.timers {
		if timer != nil {
			timer.Stop()
		}
		s.bumpGenerationLocked(sessionID)
	}
	s.timers = make(map[string]*time.Timer)
	s.failures = make(map[string]int)
}

func (s *TokenRefreshScheduler) scheduleWithDelay(sessionID string, delay time.Duration) {
	if delay <= 0 {
		delay = 30 * time.Second
	}

	s.mu.Lock()
	if timer := s.timers[sessionID]; timer != nil {
		timer.Stop()
	}
	gen := s.bumpGenerationLocked(sessionID)
	s.timers[sessionID] = time.AfterFunc(delay, func() {
		s.doRefresh(sessionID, gen)
	})
	s.mu.Unlock()
}

func (s *TokenRefreshScheduler) doRefresh(sessionID string, generation int64) {
	accessToken, err := s.provider()
	if err != nil || strings.TrimSpace(accessToken) == "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.generations[sessionID] != generation {
			return
		}
		s.failures[sessionID]++
		if s.failures[sessionID] >= s.maxFailures {
			delete(s.timers, sessionID)
			return
		}
		s.timers[sessionID] = time.AfterFunc(s.retryDelay, func() {
			s.doRefresh(sessionID, generation)
		})
		return
	}

	s.onRefresh(sessionID, accessToken)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generations[sessionID] != generation {
		return
	}
	delete(s.failures, sessionID)
	s.timers[sessionID] = time.AfterFunc(s.fallbackRefreshDelay, func() {
		s.doRefresh(sessionID, generation)
	})
}

func (s *TokenRefreshScheduler) bumpGenerationLocked(sessionID string) int64 {
	s.generations[sessionID]++
	return s.generations[sessionID]
}

func AccessTokenProviderFromContext(ctx context.Context, fn func(context.Context) (string, error)) AccessTokenProvider {
	return func() (string, error) {
		if ctx == nil {
			return "", errors.New("nil context")
		}
		return fn(ctx)
	}
}
