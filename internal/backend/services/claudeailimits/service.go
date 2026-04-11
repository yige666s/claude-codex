package claudeailimits

import (
	"net/http"

	"claude-codex/internal/public/ratelimit"
)

type Service struct {
	tracker *ratelimit.Tracker
}

func NewService() *Service {
	return &Service{tracker: ratelimit.NewTracker()}
}

func (s *Service) Tracker() *ratelimit.Tracker {
	return s.tracker
}

func (s *Service) CurrentLimits() ratelimit.ClaudeAILimits {
	return s.tracker.GetCurrentLimits()
}

func (s *Service) RawUtilization() ratelimit.RawUtilization {
	return s.tracker.GetRawUtilization()
}

func (s *Service) ProcessResponseHeaders(headers http.Header) {
	s.tracker.ProcessResponseHeaders(headers)
}

func (s *Service) ProcessError(statusCode int, headers http.Header) {
	s.tracker.ProcessError(statusCode, headers)
}

func (s *Service) AddStatusListener(listener ratelimit.StatusListener) {
	s.tracker.AddStatusListener(listener)
}

func GetRateLimitDisplayName(limitType ratelimit.RateLimitType) string {
	return ratelimit.GetRateLimitDisplayName(limitType)
}
