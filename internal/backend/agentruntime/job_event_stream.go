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
	DefaultJobEventStreamPrefix = "agentapi:job-events"
	DefaultJobEventStreamTTL    = 24 * time.Hour
)

type RedisJobEventStreamConfig struct {
	Prefix string
	TTL    time.Duration
	MaxLen int64
}

func (c RedisJobEventStreamConfig) normalized() RedisJobEventStreamConfig {
	out := c
	out.Prefix = strings.Trim(strings.TrimSpace(out.Prefix), ":")
	if out.Prefix == "" {
		out.Prefix = DefaultJobEventStreamPrefix
	}
	if out.TTL <= 0 {
		out.TTL = DefaultJobEventStreamTTL
	}
	if out.MaxLen <= 0 {
		out.MaxLen = 10000
	}
	return out
}

type RedisJobEventStreamStore struct {
	client redis.UniversalClient
	config RedisJobEventStreamConfig
}

func NewRedisJobEventStreamStore(client redis.UniversalClient, config RedisJobEventStreamConfig) *RedisJobEventStreamStore {
	return &RedisJobEventStreamStore{client: client, config: config.normalized()}
}

func (s *RedisJobEventStreamStore) AppendJobEvent(ctx context.Context, event *JobEvent) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("redis job event stream is not configured")
	}
	if event == nil || strings.TrimSpace(event.JobID) == "" || strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("job event id and job id are required")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	streamKey := s.streamKey(event.JobID)
	streamID, err := s.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		MaxLen: s.config.MaxLen,
		Approx: true,
		Values: map[string]any{
			"event_id":   event.ID,
			"user_id":    event.UserID,
			"session_id": event.SessionID,
			"type":       event.Type,
			"payload":    string(payload),
			"created_at": event.CreatedAt.UTC().Format(time.RFC3339Nano),
		},
	}).Result()
	if err != nil {
		return err
	}
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, s.indexKey(event.JobID), event.ID, streamID)
	pipe.Expire(ctx, streamKey, s.config.TTL)
	pipe.Expire(ctx, s.indexKey(event.JobID), s.config.TTL)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisJobEventStreamStore) BlockReadJobEvents(ctx context.Context, userID, jobID, afterID string, limit int, block time.Duration) ([]*JobEvent, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("redis job event stream is not configured")
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, fmt.Errorf("job id is required")
	}
	if limit <= 0 {
		limit = 500
	}
	if block <= 0 {
		block = 10 * time.Second
	}
	streamID := "0-0"
	afterID = strings.TrimSpace(afterID)
	if afterID != "" {
		if mapped, err := s.client.HGet(ctx, s.indexKey(jobID), afterID).Result(); err == nil && strings.TrimSpace(mapped) != "" {
			streamID = mapped
		}
	}
	streams, err := s.client.XRead(ctx, &redis.XReadArgs{
		Streams: []string{s.streamKey(jobID), streamID},
		Count:   int64(limit),
		Block:   block,
	}).Result()
	if err != nil {
		if err == redis.Nil || ctx.Err() != nil {
			return []*JobEvent{}, ctx.Err()
		}
		return nil, err
	}
	out := make([]*JobEvent, 0)
	for _, stream := range streams {
		for _, message := range stream.Messages {
			event, ok := redisJobEventFromValues(message.Values)
			if !ok {
				continue
			}
			if userID != "" && event.UserID != userID {
				continue
			}
			if event.JobID != jobID {
				continue
			}
			if afterID != "" && event.ID <= afterID {
				continue
			}
			out = append(out, event)
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func (s *RedisJobEventStreamStore) streamKey(jobID string) string {
	return s.config.Prefix + ":job:" + strings.TrimSpace(jobID) + ":events"
}

func (s *RedisJobEventStreamStore) indexKey(jobID string) string {
	return s.config.Prefix + ":job:" + strings.TrimSpace(jobID) + ":event-ids"
}

func redisJobEventFromValues(values map[string]any) (*JobEvent, bool) {
	payload := redisStringValue(values["payload"])
	if payload != "" {
		var event JobEvent
		if err := json.Unmarshal([]byte(payload), &event); err == nil && event.ID != "" && event.JobID != "" {
			return &event, true
		}
	}
	eventID := redisStringValue(values["event_id"])
	jobID := redisStringValue(values["job_id"])
	if eventID == "" || jobID == "" {
		return nil, false
	}
	event := &JobEvent{
		ID:        eventID,
		JobID:     jobID,
		UserID:    redisStringValue(values["user_id"]),
		SessionID: redisStringValue(values["session_id"]),
		Type:      redisStringValue(values["type"]),
	}
	if createdAt := redisStringValue(values["created_at"]); createdAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			event.CreatedAt = parsed
		}
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return event, true
}

func redisStringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
