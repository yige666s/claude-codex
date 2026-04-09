package server

import (
	"net/http"
	"strings"
)

// ConnectionRateLimiter implements simple per-IP rate limiting for new connections
type ConnectionRateLimiter struct {
	attempts     map[string][]int64
	maxPerWindow int
	windowMs     int64
}

// NewConnectionRateLimiter creates a new connection rate limiter
func NewConnectionRateLimiter(maxPerWindow int, windowMs int64) *ConnectionRateLimiter {
	if maxPerWindow == 0 {
		maxPerWindow = 5
	}
	if windowMs == 0 {
		windowMs = 60000 // 1 minute
	}

	return &ConnectionRateLimiter{
		attempts:     make(map[string][]int64),
		maxPerWindow: maxPerWindow,
		windowMs:     windowMs,
	}
}

// Allow checks if a connection from the given IP should be allowed
func (l *ConnectionRateLimiter) Allow(ip string) bool {
	now := getCurrentTimeMs()
	timestamps := l.attempts[ip]

	// Prune old entries
	recent := make([]int64, 0, len(timestamps))
	for _, t := range timestamps {
		if now-t < l.windowMs {
			recent = append(recent, t)
		}
	}

	if len(recent) >= l.maxPerWindow {
		l.attempts[ip] = recent
		return false
	}

	recent = append(recent, now)
	l.attempts[ip] = recent
	return true
}

// Cleanup periodically removes stale entries
func (l *ConnectionRateLimiter) Cleanup() {
	now := getCurrentTimeMs()
	for ip, timestamps := range l.attempts {
		recent := make([]int64, 0, len(timestamps))
		for _, t := range timestamps {
			if now-t < l.windowMs {
				recent = append(recent, t)
			}
		}

		if len(recent) == 0 {
			delete(l.attempts, ip)
		} else {
			l.attempts[ip] = recent
		}
	}
}

// ValidateAuthToken validates the auth token from a WebSocket upgrade request
// If AUTH_TOKEN is not set, all connections are allowed
func ValidateAuthToken(r *http.Request, authToken string) bool {
	if authToken == "" {
		return true
	}

	// Check query parameter
	token := r.URL.Query().Get("token")
	if token == authToken {
		return true
	}

	// Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
		return token == authToken
	}

	return false
}

// GetClientIP extracts the client IP from the request
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// getCurrentTimeMs returns current time in milliseconds
func getCurrentTimeMs() int64 {
	return timeNowUnixMilli()
}
