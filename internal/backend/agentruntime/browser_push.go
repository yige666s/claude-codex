package agentruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const defaultBrowserPushTTLSeconds = 12 * 60 * 60

type BrowserPushConfig struct {
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	TTLSeconds      int
}

func (c BrowserPushConfig) Enabled() bool {
	return strings.TrimSpace(c.VAPIDPublicKey) != "" &&
		strings.TrimSpace(c.VAPIDPrivateKey) != "" &&
		strings.TrimSpace(c.VAPIDSubject) != ""
}

func (c BrowserPushConfig) ttlSeconds() int {
	if c.TTLSeconds > 0 {
		return c.TTLSeconds
	}
	return defaultBrowserPushTTLSeconds
}

type BrowserPushPublicConfig struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key,omitempty"`
}

type BrowserPushSubscriptionInput struct {
	Endpoint       string          `json:"endpoint"`
	ExpirationTime *int64          `json:"expiration_time,omitempty"`
	Keys           BrowserPushKeys `json:"keys"`
	UserAgent      string          `json:"user_agent,omitempty"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
}

type BrowserPushKeys struct {
	P256DH string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type BrowserPushSubscription struct {
	ID             string
	UserID         string
	Endpoint       string
	EndpointHash   string
	P256DH         string
	AuthSecret     string
	UserAgent      string
	Platform       string
	Enabled        bool
	DisabledReason string
	ExpiresAt      *time.Time
	LastSentAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type BrowserPushSubscriptionResponse struct {
	ID           string     `json:"id"`
	Enabled      bool       `json:"enabled"`
	EndpointHash string     `json:"endpoint_hash,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastSentAt   *time.Time `json:"last_sent_at,omitempty"`
}

func browserPushSubscriptionResponse(sub BrowserPushSubscription) BrowserPushSubscriptionResponse {
	return BrowserPushSubscriptionResponse{
		ID:           sub.ID,
		Enabled:      sub.Enabled,
		EndpointHash: sub.EndpointHash,
		CreatedAt:    sub.CreatedAt,
		UpdatedAt:    sub.UpdatedAt,
		LastSentAt:   sub.LastSentAt,
	}
}

type BrowserPushStore interface {
	Init(ctx context.Context) error
	UpsertBrowserPushSubscription(ctx context.Context, userID string, input BrowserPushSubscriptionInput, at time.Time) (BrowserPushSubscription, error)
	DeleteBrowserPushSubscription(ctx context.Context, userID, subscriptionID string, at time.Time) error
	ListEnabledBrowserPushSubscriptions(ctx context.Context, userID string, limit int) ([]BrowserPushSubscription, error)
	DisableBrowserPushSubscription(ctx context.Context, userID, subscriptionID, reason string, at time.Time) error
	MarkBrowserPushSent(ctx context.Context, userID, subscriptionID string, at time.Time) error
	DeleteUser(ctx context.Context, userID string) error
}

type MemoryBrowserPushStore struct {
	mu            sync.Mutex
	subscriptions map[string]BrowserPushSubscription
	byUserHash    map[string]string
}

func NewMemoryBrowserPushStore() *MemoryBrowserPushStore {
	return &MemoryBrowserPushStore{
		subscriptions: make(map[string]BrowserPushSubscription),
		byUserHash:    make(map[string]string),
	}
}

func (s *MemoryBrowserPushStore) Init(context.Context) error { return nil }

func (s *MemoryBrowserPushStore) UpsertBrowserPushSubscription(_ context.Context, userID string, input BrowserPushSubscriptionInput, at time.Time) (BrowserPushSubscription, error) {
	input, err := normalizeBrowserPushSubscriptionInput(input)
	if err != nil {
		return BrowserPushSubscription{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return BrowserPushSubscription{}, fmt.Errorf("user id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	endpointHash := browserPushEndpointHash(input.Endpoint)
	key := userID + "\x00" + endpointHash
	id := s.byUserHash[key]
	sub, ok := s.subscriptions[id]
	if !ok {
		sub = BrowserPushSubscription{
			ID:           NewBrowserPushSubscriptionID(),
			UserID:       userID,
			EndpointHash: endpointHash,
			Platform:     "web",
			CreatedAt:    at,
		}
	}
	sub.Endpoint = input.Endpoint
	sub.P256DH = input.Keys.P256DH
	sub.AuthSecret = input.Keys.Auth
	sub.UserAgent = input.UserAgent
	sub.Enabled = true
	sub.DisabledReason = ""
	sub.ExpiresAt = browserPushExpirationTime(input.ExpirationTime)
	sub.UpdatedAt = at
	s.subscriptions[sub.ID] = sub
	s.byUserHash[key] = sub.ID
	return sub, nil
}

func (s *MemoryBrowserPushStore) DeleteBrowserPushSubscription(_ context.Context, userID, subscriptionID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[subscriptionID]
	if !ok || sub.UserID != userID {
		return nil
	}
	sub.Enabled = false
	sub.DisabledReason = "deleted"
	sub.UpdatedAt = at
	s.subscriptions[subscriptionID] = sub
	return nil
}

func (s *MemoryBrowserPushStore) ListEnabledBrowserPushSubscriptions(_ context.Context, userID string, limit int) ([]BrowserPushSubscription, error) {
	if limit <= 0 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]BrowserPushSubscription, 0)
	for _, sub := range s.subscriptions {
		if sub.UserID == userID && sub.Enabled {
			out = append(out, sub)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *MemoryBrowserPushStore) DisableBrowserPushSubscription(_ context.Context, userID, subscriptionID, reason string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[subscriptionID]
	if !ok || sub.UserID != userID {
		return nil
	}
	sub.Enabled = false
	sub.DisabledReason = strings.TrimSpace(reason)
	sub.UpdatedAt = at
	s.subscriptions[subscriptionID] = sub
	return nil
}

func (s *MemoryBrowserPushStore) MarkBrowserPushSent(_ context.Context, userID, subscriptionID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[subscriptionID]
	if !ok || sub.UserID != userID {
		return nil
	}
	sub.LastSentAt = &at
	sub.UpdatedAt = at
	s.subscriptions[subscriptionID] = sub
	return nil
}

func (s *MemoryBrowserPushStore) DeleteUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sub := range s.subscriptions {
		if sub.UserID == userID {
			delete(s.subscriptions, id)
			delete(s.byUserHash, userID+"\x00"+sub.EndpointHash)
		}
	}
	return nil
}

type SQLBrowserPushStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLBrowserPushStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLBrowserPushStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLBrowserPushStore{db: db, dialect: dialect}
}

func (s *SQLBrowserPushStore) Init(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	return nil
}

func (s *SQLBrowserPushStore) UpsertBrowserPushSubscription(ctx context.Context, userID string, input BrowserPushSubscriptionInput, at time.Time) (BrowserPushSubscription, error) {
	input, err := normalizeBrowserPushSubscriptionInput(input)
	if err != nil {
		return BrowserPushSubscription{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return BrowserPushSubscription{}, fmt.Errorf("user id is required")
	}
	id := NewBrowserPushSubscriptionID()
	expiresAt := browserPushExpirationTime(input.ExpirationTime)
	hash := browserPushEndpointHash(input.Endpoint)
	query := `INSERT INTO agent_browser_push_subscriptions
		(subscription_id, user_id, endpoint, endpoint_hash, p256dh, auth_secret, user_agent, platform, enabled, disabled_reason, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'web', true, '', $8, $9, $10)
		ON CONFLICT (user_id, endpoint_hash) DO UPDATE SET
			endpoint = EXCLUDED.endpoint,
			p256dh = EXCLUDED.p256dh,
			auth_secret = EXCLUDED.auth_secret,
			user_agent = EXCLUDED.user_agent,
			enabled = true,
			disabled_reason = '',
			expires_at = EXCLUDED.expires_at,
			updated_at = EXCLUDED.updated_at
		RETURNING subscription_id, user_id, endpoint, endpoint_hash, p256dh, auth_secret, user_agent, platform, enabled, disabled_reason, expires_at, last_sent_at, created_at, updated_at`
	if s.dialect != SQLDialectPostgres {
		query = questionPlaceholders(query)
	}
	row := s.db.QueryRowContext(ctx, query, id, userID, input.Endpoint, hash, input.Keys.P256DH, input.Keys.Auth, input.UserAgent, expiresAt, at, at)
	return scanBrowserPushSubscription(row)
}

func (s *SQLBrowserPushStore) DeleteBrowserPushSubscription(ctx context.Context, userID, subscriptionID string, at time.Time) error {
	query := `UPDATE agent_browser_push_subscriptions
		SET enabled = false, disabled_reason = $1, updated_at = $2
		WHERE user_id = $3 AND subscription_id = $4`
	if s.dialect != SQLDialectPostgres {
		query = questionPlaceholders(query)
	}
	_, err := s.db.ExecContext(ctx, query, "deleted", at, userID, subscriptionID)
	return err
}

func (s *SQLBrowserPushStore) ListEnabledBrowserPushSubscriptions(ctx context.Context, userID string, limit int) ([]BrowserPushSubscription, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT subscription_id, user_id, endpoint, endpoint_hash, p256dh, auth_secret, user_agent, platform, enabled, disabled_reason, expires_at, last_sent_at, created_at, updated_at
		FROM agent_browser_push_subscriptions
		WHERE user_id = $1 AND enabled = true
		ORDER BY updated_at DESC
		LIMIT $2`
	if s.dialect != SQLDialectPostgres {
		query = questionPlaceholders(query)
	}
	rows, err := s.db.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BrowserPushSubscription, 0)
	for rows.Next() {
		sub, err := scanBrowserPushSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

func (s *SQLBrowserPushStore) DisableBrowserPushSubscription(ctx context.Context, userID, subscriptionID, reason string, at time.Time) error {
	query := `UPDATE agent_browser_push_subscriptions
		SET enabled = false, disabled_reason = $1, updated_at = $2
		WHERE user_id = $3 AND subscription_id = $4`
	if s.dialect != SQLDialectPostgres {
		query = questionPlaceholders(query)
	}
	_, err := s.db.ExecContext(ctx, query, strings.TrimSpace(reason), at, userID, subscriptionID)
	return err
}

func (s *SQLBrowserPushStore) MarkBrowserPushSent(ctx context.Context, userID, subscriptionID string, at time.Time) error {
	query := `UPDATE agent_browser_push_subscriptions
		SET last_sent_at = $1, updated_at = $2
		WHERE user_id = $3 AND subscription_id = $4`
	if s.dialect != SQLDialectPostgres {
		query = questionPlaceholders(query)
	}
	_, err := s.db.ExecContext(ctx, query, at, at, userID, subscriptionID)
	return err
}

func (s *SQLBrowserPushStore) DeleteUser(ctx context.Context, userID string) error {
	query := `DELETE FROM agent_browser_push_subscriptions WHERE user_id = $1`
	if s.dialect != SQLDialectPostgres {
		query = questionPlaceholders(query)
	}
	_, err := s.db.ExecContext(ctx, query, userID)
	return err
}

type browserPushScanner interface {
	Scan(dest ...any) error
}

func scanBrowserPushSubscription(scanner browserPushScanner) (BrowserPushSubscription, error) {
	var sub BrowserPushSubscription
	var expiresAt sql.NullTime
	var lastSentAt sql.NullTime
	err := scanner.Scan(
		&sub.ID,
		&sub.UserID,
		&sub.Endpoint,
		&sub.EndpointHash,
		&sub.P256DH,
		&sub.AuthSecret,
		&sub.UserAgent,
		&sub.Platform,
		&sub.Enabled,
		&sub.DisabledReason,
		&expiresAt,
		&lastSentAt,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	)
	if err != nil {
		return BrowserPushSubscription{}, err
	}
	if expiresAt.Valid {
		sub.ExpiresAt = &expiresAt.Time
	}
	if lastSentAt.Valid {
		sub.LastSentAt = &lastSentAt.Time
	}
	return sub, nil
}

type BrowserPushSender struct {
	config BrowserPushConfig
	client webpush.HTTPClient
}

func NewBrowserPushSender(config BrowserPushConfig) *BrowserPushSender {
	return &BrowserPushSender{config: config}
}

func (s *BrowserPushSender) Enabled() bool {
	return s != nil && s.config.Enabled()
}

func (s *BrowserPushSender) PublicConfig() BrowserPushPublicConfig {
	if !s.Enabled() {
		return BrowserPushPublicConfig{Enabled: false}
	}
	return BrowserPushPublicConfig{Enabled: true, PublicKey: strings.TrimSpace(s.config.VAPIDPublicKey)}
}

type BrowserPushPayload struct {
	Title            string `json:"title"`
	Body             string `json:"body"`
	URL              string `json:"url,omitempty"`
	Tag              string `json:"tag,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	JobID            string `json:"job_id,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	NotificationType string `json:"notification_type,omitempty"`
}

func (s *BrowserPushSender) Send(ctx context.Context, sub BrowserPushSubscription, payload BrowserPushPayload) (*http.Response, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("browser push is not configured")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	options := &webpush.Options{
		HTTPClient:      s.client,
		Subscriber:      strings.TrimSpace(s.config.VAPIDSubject),
		TTL:             s.config.ttlSeconds(),
		VAPIDPublicKey:  strings.TrimSpace(s.config.VAPIDPublicKey),
		VAPIDPrivateKey: strings.TrimSpace(s.config.VAPIDPrivateKey),
	}
	return webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256DH,
			Auth:   sub.AuthSecret,
		},
	}, options)
}

func (r *Runtime) SetBrowserPushStore(store BrowserPushStore) {
	if r == nil {
		return
	}
	if store == nil {
		store = NewMemoryBrowserPushStore()
	}
	r.browserPush = store
}

func (r *Runtime) SetBrowserPushSender(sender *BrowserPushSender) {
	if r == nil {
		return
	}
	r.browserPushSender = sender
}

func (r *Runtime) BrowserPushPublicConfig() BrowserPushPublicConfig {
	if r == nil || r.browserPushSender == nil {
		return BrowserPushPublicConfig{Enabled: false}
	}
	return r.browserPushSender.PublicConfig()
}

func (r *Runtime) UpsertBrowserPushSubscription(ctx context.Context, userID string, input BrowserPushSubscriptionInput) (BrowserPushSubscription, error) {
	return r.browserPushStore().UpsertBrowserPushSubscription(ctx, userID, input, time.Now().UTC())
}

func (r *Runtime) DeleteBrowserPushSubscription(ctx context.Context, userID, subscriptionID string) error {
	return r.browserPushStore().DeleteBrowserPushSubscription(ctx, userID, strings.TrimSpace(subscriptionID), time.Now().UTC())
}

func (r *Runtime) SendTestBrowserPush(ctx context.Context, userID string) error {
	if r == nil || !r.browserPushSender.Enabled() {
		return fmt.Errorf("browser push is not configured")
	}
	return r.sendBrowserPushToUser(ctx, userID, BrowserPushPayload{
		Title:            "AgentAPI notifications are ready",
		Body:             "You will receive task updates even after this page is closed.",
		URL:              "/app",
		Tag:              "agentapi-browser-push-test",
		NotificationType: "test",
	})
}

func (r *Runtime) browserPushStore() BrowserPushStore {
	if r == nil {
		return NewMemoryBrowserPushStore()
	}
	if r.browserPush == nil {
		r.browserPush = NewMemoryBrowserPushStore()
	}
	return r.browserPush
}

func (r *Runtime) notifyTaskInboxJob(ctx context.Context, job *Job, status, errorText string) {
	if r == nil || job == nil || !r.browserPushSender.Enabled() {
		return
	}
	notificationType := "job_completed"
	title := "Task completed"
	body := strings.TrimSpace(job.Content)
	if body == "" {
		body = "A background task has finished."
	}
	if status == JobStatusFailed {
		notificationType = "job_failed"
		title = "Task failed"
		if strings.TrimSpace(errorText) != "" {
			body = strings.TrimSpace(errorText)
		}
	} else if status == JobStatusCancelled {
		notificationType = "job_cancelled"
		title = "Task cancelled"
	}
	payload := BrowserPushPayload{
		Title:            title,
		Body:             truncateRunes(body, 180),
		URL:              "/app",
		Tag:              "agentapi-" + job.ID + "-" + status,
		TaskID:           job.ID,
		JobID:            job.ID,
		SessionID:        job.SessionID,
		NotificationType: notificationType,
	}
	if err := r.sendBrowserPushToUser(ctx, job.UserID, payload); err != nil && r.logger != nil {
		r.logger.Warn("send browser push", "user_id", job.UserID, "job_id", job.ID, "error", err)
	}
}

func (r *Runtime) sendBrowserPushToUser(ctx context.Context, userID string, payload BrowserPushPayload) error {
	if r == nil || !r.browserPushSender.Enabled() {
		return fmt.Errorf("browser push is not configured")
	}
	subs, err := r.browserPushStore().ListEnabledBrowserPushSubscriptions(ctx, userID, 100)
	if err != nil {
		return err
	}
	var joined error
	for _, sub := range subs {
		resp, sendErr := r.browserPushSender.Send(ctx, sub, payload)
		if resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
				_ = r.browserPushStore().DisableBrowserPushSubscription(context.Background(), userID, sub.ID, fmt.Sprintf("push endpoint returned %d", resp.StatusCode), time.Now().UTC())
			}
			if resp.StatusCode >= 300 && sendErr == nil {
				sendErr = fmt.Errorf("push endpoint returned %d", resp.StatusCode)
			}
		}
		if sendErr != nil {
			joined = errors.Join(joined, sendErr)
			continue
		}
		_ = r.browserPushStore().MarkBrowserPushSent(context.Background(), userID, sub.ID, time.Now().UTC())
	}
	return joined
}

func normalizeBrowserPushSubscriptionInput(input BrowserPushSubscriptionInput) (BrowserPushSubscriptionInput, error) {
	input.Endpoint = strings.TrimSpace(input.Endpoint)
	input.Keys.P256DH = strings.TrimSpace(input.Keys.P256DH)
	input.Keys.Auth = strings.TrimSpace(input.Keys.Auth)
	input.UserAgent = truncateRunes(strings.TrimSpace(input.UserAgent), 512)
	if input.Endpoint == "" {
		return input, fmt.Errorf("endpoint is required")
	}
	if input.Keys.P256DH == "" {
		return input, fmt.Errorf("p256dh key is required")
	}
	if input.Keys.Auth == "" {
		return input, fmt.Errorf("auth key is required")
	}
	return input, nil
}

func browserPushEndpointHash(endpoint string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(endpoint)))
	return hex.EncodeToString(sum[:])
}

func browserPushExpirationTime(expirationTime *int64) *time.Time {
	if expirationTime == nil || *expirationTime <= 0 {
		return nil
	}
	at := time.UnixMilli(*expirationTime).UTC()
	return &at
}

func NewBrowserPushSubscriptionID() string {
	return "push-" + newSortableID()
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func questionPlaceholders(query string) string {
	for i := 64; i >= 1; i-- {
		query = strings.ReplaceAll(query, "$"+strconv.Itoa(i), "?")
	}
	return query
}
