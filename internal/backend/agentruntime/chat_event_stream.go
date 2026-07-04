package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	DefaultChatEventStreamPrefix = "agentapi:chat-events"
	DefaultChatEventStreamTTL    = 24 * time.Hour
)

type RedisChatStreamConfig struct {
	Prefix string
	TTL    time.Duration
	MaxLen int64
	Block  time.Duration
}

func (c RedisChatStreamConfig) normalized() RedisChatStreamConfig {
	out := c
	out.Prefix = strings.Trim(strings.TrimSpace(out.Prefix), ":")
	if out.Prefix == "" {
		out.Prefix = DefaultChatEventStreamPrefix
	}
	if out.TTL <= 0 {
		out.TTL = DefaultChatEventStreamTTL
	}
	if out.MaxLen <= 0 {
		out.MaxLen = 10000
	}
	if out.Block <= 0 {
		out.Block = DefaultChatStreamBlockRead
	}
	return out
}

type RedisChatStreamStore struct {
	client redis.UniversalClient
	config RedisChatStreamConfig
}

func NewRedisChatStreamStore(client redis.UniversalClient, config RedisChatStreamConfig) *RedisChatStreamStore {
	return &RedisChatStreamStore{client: client, config: config.normalized()}
}

func (s *RedisChatStreamStore) CreateRun(ctx context.Context, userID, sessionID string) (*ChatRunSummary, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("redis chat stream is not configured")
	}
	runID := NewChatRunID()
	now := time.Now().UTC()
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	meta := map[string]any{
		"run_id":     runID,
		"user_id":    userID,
		"session_id": sessionID,
		"status":     "running",
		"terminal":   "false",
		"updated_at": now.Format(time.RFC3339Nano),
	}
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, s.metaKey(runID), meta)
	pipe.Set(ctx, s.activeKey(userID, sessionID), runID, s.config.TTL)
	pipe.Expire(ctx, s.metaKey(runID), s.config.TTL)
	pipe.Expire(ctx, s.streamKey(runID), s.config.TTL)
	pipe.Expire(ctx, s.indexKey(runID), s.config.TTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	return &ChatRunSummary{RunID: runID, SessionID: sessionID, Status: "running", UpdatedAt: now}, nil
}

func (s *RedisChatStreamStore) Append(ctx context.Context, runID, userID, sessionID string, event Event) (*ChatStreamEvent, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("redis chat stream is not configured")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("chat run id is required")
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if event.SessionID == "" {
		event.SessionID = sessionID
	}
	event.RunID = runID
	record := &ChatStreamEvent{
		ID:        NewJobEventID(),
		RunID:     runID,
		UserID:    userID,
		SessionID: sessionID,
		Type:      event.Type,
		Event:     event,
		CreatedAt: time.Now().UTC(),
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	streamKey := s.streamKey(runID)
	streamID, err := s.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		MaxLen: s.config.MaxLen,
		Approx: true,
		Values: map[string]any{
			"event_id":   record.ID,
			"run_id":     record.RunID,
			"user_id":    record.UserID,
			"session_id": record.SessionID,
			"type":       record.Type,
			"payload":    string(payload),
			"created_at": record.CreatedAt.Format(time.RFC3339Nano),
		},
	}).Result()
	if err != nil {
		return nil, err
	}
	status := "running"
	terminal := "false"
	if chatStreamTerminal(record.Type) {
		status = chatStreamTerminalStatus(record.Type)
		terminal = "true"
	}
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, s.indexKey(runID), record.ID, streamID)
	pipe.HSet(ctx, s.metaKey(runID), map[string]any{
		"run_id":        runID,
		"user_id":       userID,
		"session_id":    sessionID,
		"status":        status,
		"terminal":      terminal,
		"last_event_id": record.ID,
		"updated_at":    record.CreatedAt.Format(time.RFC3339Nano),
	})
	if chatStreamTerminal(record.Type) {
		pipe.Del(ctx, s.activeKey(userID, sessionID))
	} else {
		pipe.Set(ctx, s.activeKey(userID, sessionID), runID, s.config.TTL)
	}
	pipe.Expire(ctx, streamKey, s.config.TTL)
	pipe.Expire(ctx, s.indexKey(runID), s.config.TTL)
	pipe.Expire(ctx, s.metaKey(runID), s.config.TTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *RedisChatStreamStore) ListAfter(ctx context.Context, userID, runID, afterID string, limit int) ([]*ChatStreamEvent, bool, error) {
	return s.read(ctx, userID, runID, afterID, limit, 0)
}

func (s *RedisChatStreamStore) BlockRead(ctx context.Context, userID, runID, afterID string, limit int, block time.Duration) ([]*ChatStreamEvent, bool, error) {
	if block <= 0 {
		block = s.config.Block
	}
	return s.read(ctx, userID, runID, afterID, limit, block)
}

func (s *RedisChatStreamStore) read(ctx context.Context, userID, runID, afterID string, limit int, block time.Duration) ([]*ChatStreamEvent, bool, error) {
	if s == nil || s.client == nil {
		return nil, false, fmt.Errorf("redis chat stream is not configured")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, false, fmt.Errorf("chat run id is required")
	}
	if limit <= 0 {
		limit = DefaultChatStreamEventLimit
	}
	meta, err := s.meta(ctx, runID)
	if err != nil {
		return nil, false, err
	}
	if len(meta) == 0 {
		return nil, false, fmt.Errorf("chat run was not found")
	}
	if userID != "" && meta["user_id"] != "" && strings.TrimSpace(userID) != meta["user_id"] {
		return nil, false, fmt.Errorf("chat run was not found")
	}
	streamID := "0-0"
	afterID = strings.TrimSpace(afterID)
	if afterID != "" {
		if mapped, err := s.client.HGet(ctx, s.indexKey(runID), afterID).Result(); err == nil && strings.TrimSpace(mapped) != "" {
			streamID = mapped
		}
	}
	streams, err := s.client.XRead(ctx, &redis.XReadArgs{
		Streams: []string{s.streamKey(runID), streamID},
		Count:   int64(limit),
		Block:   block,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return []*ChatStreamEvent{}, chatStreamMetaTerminal(meta), nil
		}
		if ctx.Err() != nil {
			return []*ChatStreamEvent{}, chatStreamMetaTerminal(meta), ctx.Err()
		}
		return nil, false, err
	}
	out := make([]*ChatStreamEvent, 0)
	terminal := chatStreamMetaTerminal(meta)
	for _, stream := range streams {
		for _, message := range stream.Messages {
			event, ok := redisChatStreamEventFromValues(message.Values)
			if !ok {
				continue
			}
			if event.RunID != runID {
				continue
			}
			if userID != "" && event.UserID != "" && event.UserID != strings.TrimSpace(userID) {
				continue
			}
			if afterID != "" && event.ID <= afterID {
				continue
			}
			if chatStreamTerminal(event.Type) {
				terminal = true
			}
			out = append(out, event)
			if len(out) >= limit {
				return out, terminal, nil
			}
		}
	}
	return out, terminal, nil
}

func (s *RedisChatStreamStore) LatestActiveForSession(ctx context.Context, userID, sessionID string) (*ChatRunSummary, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("redis chat stream is not configured")
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" || sessionID == "" {
		return nil, nil
	}
	runID, err := s.client.Get(ctx, s.activeKey(userID, sessionID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	meta, err := s.meta(ctx, runID)
	if err != nil {
		return nil, err
	}
	if len(meta) == 0 || chatStreamMetaTerminal(meta) {
		_ = s.client.Del(ctx, s.activeKey(userID, sessionID)).Err()
		return nil, nil
	}
	return chatRunSummaryFromMeta(runID, meta), nil
}

func (s *RedisChatStreamStore) MarkTerminal(ctx context.Context, runID string, terminalType string, _ string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("redis chat stream is not configured")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("chat run id is required")
	}
	meta, err := s.meta(ctx, runID)
	if err != nil {
		return err
	}
	if len(meta) == 0 {
		return fmt.Errorf("chat run was not found")
	}
	now := time.Now().UTC()
	status := chatStreamTerminalStatus(terminalType)
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, s.metaKey(runID), map[string]any{
		"status":     status,
		"terminal":   "true",
		"updated_at": now.Format(time.RFC3339Nano),
	})
	if meta["user_id"] != "" && meta["session_id"] != "" {
		pipe.Del(ctx, s.activeKey(meta["user_id"], meta["session_id"]))
	}
	pipe.Expire(ctx, s.streamKey(runID), s.config.TTL)
	pipe.Expire(ctx, s.indexKey(runID), s.config.TTL)
	pipe.Expire(ctx, s.metaKey(runID), s.config.TTL)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisChatStreamStore) meta(ctx context.Context, runID string) (map[string]string, error) {
	values, err := s.client.HGetAll(ctx, s.metaKey(runID)).Result()
	if err != nil {
		return nil, err
	}
	return values, nil
}

func (s *RedisChatStreamStore) streamKey(runID string) string {
	return s.config.Prefix + ":run:" + strings.TrimSpace(runID) + ":events"
}

func (s *RedisChatStreamStore) indexKey(runID string) string {
	return s.config.Prefix + ":run:" + strings.TrimSpace(runID) + ":event-ids"
}

func (s *RedisChatStreamStore) metaKey(runID string) string {
	return s.config.Prefix + ":run:" + strings.TrimSpace(runID) + ":meta"
}

func (s *RedisChatStreamStore) activeKey(userID, sessionID string) string {
	return s.config.Prefix + ":active:" + strings.TrimSpace(userID) + ":" + strings.TrimSpace(sessionID)
}

func redisChatStreamEventFromValues(values map[string]any) (*ChatStreamEvent, bool) {
	payload := redisStringValue(values["payload"])
	if payload != "" {
		var event ChatStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err == nil && event.ID != "" && event.RunID != "" {
			return &event, true
		}
	}
	eventID := redisStringValue(values["event_id"])
	runID := redisStringValue(values["run_id"])
	if eventID == "" || runID == "" {
		return nil, false
	}
	createdAt := time.Now().UTC()
	if raw := redisStringValue(values["created_at"]); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			createdAt = parsed
		}
	}
	return &ChatStreamEvent{
		ID:        eventID,
		RunID:     runID,
		UserID:    redisStringValue(values["user_id"]),
		SessionID: redisStringValue(values["session_id"]),
		Type:      redisStringValue(values["type"]),
		Event: Event{
			Type:      redisStringValue(values["type"]),
			RunID:     runID,
			SessionID: redisStringValue(values["session_id"]),
		},
		CreatedAt: createdAt,
	}, true
}

func chatStreamMetaTerminal(meta map[string]string) bool {
	status := strings.TrimSpace(meta["status"])
	return strings.EqualFold(strings.TrimSpace(meta["terminal"]), "true") || status == "succeeded" || status == "failed" || status == "cancelled"
}

func chatRunSummaryFromMeta(runID string, meta map[string]string) *ChatRunSummary {
	updatedAt := time.Time{}
	if raw := meta["updated_at"]; raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			updatedAt = parsed
		}
	}
	return &ChatRunSummary{
		RunID:       firstNonEmptyString(strings.TrimSpace(meta["run_id"]), strings.TrimSpace(runID)),
		SessionID:   strings.TrimSpace(meta["session_id"]),
		Status:      strings.TrimSpace(meta["status"]),
		Terminal:    chatStreamMetaTerminal(meta),
		LastEventID: strings.TrimSpace(meta["last_event_id"]),
		UpdatedAt:   updatedAt,
	}
}
