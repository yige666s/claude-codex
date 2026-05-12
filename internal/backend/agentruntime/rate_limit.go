package agentruntime

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RateLimitPolicy interface {
	Allow(key string) bool
}

type NoopRateLimiter struct{}

func (NoopRateLimiter) Allow(string) bool { return true }

type RateLimiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = 60
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimiter{limit: limit, window: window, hits: make(map[string][]time.Time)}
}

func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	recent := l.hits[key][:0]
	for _, hit := range l.hits[key] {
		if hit.After(cutoff) {
			recent = append(recent, hit)
		}
	}
	if len(recent) >= l.limit {
		l.hits[key] = recent
		return false
	}
	recent = append(recent, now)
	l.hits[key] = recent
	return true
}

type RedisRateLimiter struct {
	Address  string
	Password string
	DB       int
	Prefix   string
	Limit    int
	Window   time.Duration
	Timeout  time.Duration
	FailOpen bool
}

func (l RedisRateLimiter) Allow(key string) bool {
	ok, err := l.allow(context.Background(), key)
	if err != nil {
		return l.FailOpen
	}
	return ok
}

func (l RedisRateLimiter) Ping(ctx context.Context) error {
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = 750 * time.Millisecond
	}
	address := l.Address
	if address == "" {
		address = "127.0.0.1:6379"
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	reader := bufio.NewReader(conn)
	if l.Password != "" {
		if err := redisWriteCommand(conn, "AUTH", l.Password); err != nil {
			return err
		}
		if _, err := redisReadSimple(reader); err != nil {
			return err
		}
	}
	if l.DB > 0 {
		if err := redisWriteCommand(conn, "SELECT", strconv.Itoa(l.DB)); err != nil {
			return err
		}
		if _, err := redisReadSimple(reader); err != nil {
			return err
		}
	}
	if err := redisWriteCommand(conn, "PING"); err != nil {
		return err
	}
	reply, err := redisReadSimple(reader)
	if err != nil {
		return err
	}
	if reply != "+PONG" {
		return fmt.Errorf("unexpected redis ping response: %s", reply)
	}
	return nil
}

func (l RedisRateLimiter) allow(ctx context.Context, key string) (bool, error) {
	if l.Limit <= 0 {
		return true, nil
	}
	window := l.Window
	if window <= 0 {
		window = time.Minute
	}
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = 750 * time.Millisecond
	}
	address := l.Address
	if address == "" {
		address = "127.0.0.1:6379"
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	reader := bufio.NewReader(conn)
	if l.Password != "" {
		if err := redisWriteCommand(conn, "AUTH", l.Password); err != nil {
			return false, err
		}
		if _, err := redisReadSimple(reader); err != nil {
			return false, err
		}
	}
	if l.DB > 0 {
		if err := redisWriteCommand(conn, "SELECT", strconv.Itoa(l.DB)); err != nil {
			return false, err
		}
		if _, err := redisReadSimple(reader); err != nil {
			return false, err
		}
	}
	prefix := l.Prefix
	if prefix == "" {
		prefix = "agentapi:rate"
	}
	redisKey := prefix + ":" + key + ":" + strconv.FormatInt(time.Now().Unix()/int64(window.Seconds()), 10)
	if err := redisWriteCommand(conn, "INCR", redisKey); err != nil {
		return false, err
	}
	count, err := redisReadInt(reader)
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := redisWriteCommand(conn, "EXPIRE", redisKey, strconv.Itoa(int(window.Seconds())+2)); err != nil {
			return false, err
		}
		if _, err := redisReadInt(reader); err != nil {
			return false, err
		}
	}
	return count <= int64(l.Limit), nil
}

func NewRedisRateLimiter(rawURL string, limit int, window time.Duration, failOpen bool) (*RedisRateLimiter, error) {
	cfg := &RedisRateLimiter{Limit: limit, Window: window, FailOpen: failOpen}
	if strings.TrimSpace(rawURL) == "" {
		cfg.Address = "127.0.0.1:6379"
		return cfg, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	cfg.Address = parsed.Host
	if password, ok := parsed.User.Password(); ok {
		cfg.Password = password
	}
	if db := strings.Trim(parsed.Path, "/"); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil {
			return nil, fmt.Errorf("invalid redis db: %w", err)
		}
		cfg.DB = n
	}
	if prefix := parsed.Query().Get("prefix"); prefix != "" {
		cfg.Prefix = prefix
	}
	return cfg, nil
}

func redisWriteCommand(conn net.Conn, args ...string) error {
	var b strings.Builder
	b.WriteString("*")
	b.WriteString(strconv.Itoa(len(args)))
	b.WriteString("\r\n")
	for _, arg := range args {
		b.WriteString("$")
		b.WriteString(strconv.Itoa(len(arg)))
		b.WriteString("\r\n")
		b.WriteString(arg)
		b.WriteString("\r\n")
	}
	_, err := conn.Write([]byte(b.String()))
	return err
}

func redisReadSimple(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "-") {
		return "", fmt.Errorf("redis error: %s", strings.TrimPrefix(line, "-"))
	}
	return line, nil
}

func redisReadInt(r *bufio.Reader) (int64, error) {
	line, err := redisReadSimple(r)
	if err != nil {
		return 0, err
	}
	if !strings.HasPrefix(line, ":") {
		return 0, fmt.Errorf("unexpected redis integer response: %s", line)
	}
	return strconv.ParseInt(strings.TrimPrefix(line, ":"), 10, 64)
}
