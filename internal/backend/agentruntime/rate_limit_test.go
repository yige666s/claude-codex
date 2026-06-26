package agentruntime

import (
	"testing"
	"time"
)

func TestConfigurableRateLimiterAppliesUpdatedLimit(t *testing.T) {
	limiter := NewConfigurableRateLimiter(NewRateLimiter(1, time.Minute), func(limit int) (RateLimitPolicy, error) {
		if limit == 0 {
			return NoopRateLimiter{}, nil
		}
		return NewRateLimiter(limit, time.Minute), nil
	})
	if !limiter.Allow("user-1") {
		t.Fatal("first request should be allowed")
	}
	if limiter.Allow("user-1") {
		t.Fatal("second request should be rate limited")
	}
	if err := limiter.SetLimit(2); err != nil {
		t.Fatalf("set limit: %v", err)
	}
	if !limiter.Allow("user-1") || !limiter.Allow("user-1") {
		t.Fatal("updated limit should allow two fresh requests")
	}
	if limiter.Allow("user-1") {
		t.Fatal("third request after update should be rate limited")
	}
}

func TestConfigurableRateLimiterZeroDisablesLimit(t *testing.T) {
	limiter := NewConfigurableRateLimiter(NewRateLimiter(1, time.Minute), nil)
	if err := limiter.SetLimit(0); err != nil {
		t.Fatalf("set limit: %v", err)
	}
	for i := 0; i < 3; i++ {
		if !limiter.Allow("user-1") {
			t.Fatalf("request %d should be allowed when limit is disabled", i+1)
		}
	}
}
