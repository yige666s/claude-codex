package agentruntime

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
)

type readinessCheck func(context.Context) error

type readinessResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (s *Server) AddReadinessCheck(name string, check func(context.Context) error) {
	if s == nil || name == "" || check == nil {
		return
	}
	s.readyMu.Lock()
	defer s.readyMu.Unlock()
	if s.readyChecks == nil {
		s.readyChecks = make(map[string]readinessCheck)
	}
	s.readyChecks[name] = check
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.isShuttingDown() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "shutting_down", "checks": []readinessResult{}})
		return
	}
	overall, results := s.readinessSnapshot(r.Context(), 2*time.Second)
	status := http.StatusOK
	if overall != "ok" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"status": overall, "checks": results})
}

func (s *Server) readinessSnapshot(ctx context.Context, timeout time.Duration) (string, []readinessResult) {
	s.readyMu.RLock()
	checks := make(map[string]readinessCheck, len(s.readyChecks))
	for name, check := range s.readyChecks {
		checks[name] = check
	}
	s.readyMu.RUnlock()
	if len(checks) == 0 {
		return "ok", []readinessResult{}
	}

	names := make([]string, 0, len(checks))
	for name := range checks {
		names = append(names, name)
	}
	sort.Strings(names)
	results := make([]readinessResult, 0, len(names))
	overall := "ok"
	for _, name := range names {
		checkCtx := ctx
		var cancel context.CancelFunc = func() {}
		if timeout > 0 {
			checkCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		err := checks[name](checkCtx)
		cancel()
		result := readinessResult{Name: name, Status: "ok"}
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			overall = "error"
		}
		results = append(results, result)
	}
	return overall, results
}

func LLMReadinessCheck(provider func() LLMGovernanceStatus) func(context.Context) error {
	return func(context.Context) error {
		if provider == nil {
			return fmt.Errorf("llm status provider is not configured")
		}
		status := provider()
		if len(status.Backends) == 0 {
			return fmt.Errorf("no llm backends configured")
		}
		for _, backend := range status.Backends {
			if backend.Healthy {
				return nil
			}
		}
		return fmt.Errorf("no healthy llm backend")
	}
}

func ObjectStoreReadinessCheck(objects ObjectStore, prefix string) func(context.Context) error {
	return func(ctx context.Context) error {
		if objects == nil {
			return fmt.Errorf("object store is not configured")
		}
		_, err := objects.List(ctx, prefix)
		return err
	}
}

func RedisReadinessCheck(limiter RateLimitPolicy) func(context.Context) error {
	return func(ctx context.Context) error {
		switch v := limiter.(type) {
		case *RedisRateLimiter:
			if v == nil {
				return fmt.Errorf("redis limiter is not configured")
			}
			return v.Ping(ctx)
		case RedisRateLimiter:
			return v.Ping(ctx)
		default:
			return fmt.Errorf("redis limiter is not configured")
		}
	}
}

func RedisClientReadinessCheck(client interface {
	Ping(context.Context) *redis.StatusCmd
}) func(context.Context) error {
	return func(ctx context.Context) error {
		if client == nil {
			return fmt.Errorf("redis client is not configured")
		}
		return client.Ping(ctx).Err()
	}
}
