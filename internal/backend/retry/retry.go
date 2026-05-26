package retry

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Policy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Jitter      float64
	Methods     map[string]bool
	Statuses    map[int]bool
	Now         func() time.Time
}

type RetryAfterProvider interface {
	RetryAfter() time.Duration
}

type RetryAfterHeaderProvider interface {
	RetryAfterHeader() string
}

type HTTPResponseProvider interface {
	HTTPResponse() *http.Response
}

func NoRetry() Policy {
	return Policy{MaxAttempts: 1}
}

func (p Policy) Attempts() int {
	if p.MaxAttempts <= 0 {
		return 1
	}
	return p.MaxAttempts
}

func (p Policy) AttemptsForMethod(method string) int {
	if !p.methodAllowed(method) {
		return 1
	}
	return p.Attempts()
}

func (p Policy) ShouldRetry(method string, status int, err error) bool {
	if !p.methodAllowed(method) {
		return false
	}
	if status == 0 {
		return err != nil
	}
	if len(p.Statuses) == 0 {
		return false
	}
	return p.Statuses[status]
}

func (p Policy) Delay(attempt int, err error) time.Duration {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	if retryAfter := retryAfterDelay(err, now()); retryAfter > 0 {
		return retryAfter
	}
	base := p.BaseDelay
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	delay := base
	if attempt > 1 {
		delay = time.Duration(1<<min(attempt-1, 5)) * base
	}
	if p.MaxDelay > 0 && delay > p.MaxDelay {
		delay = p.MaxDelay
	}
	return applyJitter(delay, p.Jitter)
}

func (p Policy) Sleep(ctx context.Context, attempt int, err error) error {
	return Sleep(ctx, p.Delay(attempt, err))
}

func Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p Policy) methodAllowed(method string) bool {
	if len(p.Methods) == 0 {
		return true
	}
	return p.Methods[strings.ToUpper(strings.TrimSpace(method))]
}

func retryAfterDelay(err error, now time.Time) time.Duration {
	if err == nil {
		return 0
	}
	if provider, ok := err.(RetryAfterProvider); ok {
		return positiveDuration(provider.RetryAfter())
	}
	if provider, ok := err.(RetryAfterHeaderProvider); ok {
		return ParseRetryAfter(provider.RetryAfterHeader(), now)
	}
	if provider, ok := err.(HTTPResponseProvider); ok {
		if resp := provider.HTTPResponse(); resp != nil {
			return ParseRetryAfter(resp.Header.Get("Retry-After"), now)
		}
	}
	return 0
}

func ParseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return positiveDuration(time.Duration(seconds) * time.Second)
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	return positiveDuration(when.Sub(now))
}

func positiveDuration(value time.Duration) time.Duration {
	if value <= 0 {
		return 0
	}
	return value
}

func applyJitter(delay time.Duration, jitter float64) time.Duration {
	if delay <= 0 || jitter <= 0 {
		return delay
	}
	if jitter > 1 {
		jitter = 1
	}
	span := float64(delay) * jitter
	minDelay := float64(delay) - span
	maxDelay := float64(delay) + span
	return time.Duration(minDelay + randomFloat64()*(maxDelay-minDelay))
}

func randomFloat64() float64 {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return 0.5
	}
	return float64(binary.BigEndian.Uint64(data[:])>>11) / float64(uint64(1)<<53)
}
