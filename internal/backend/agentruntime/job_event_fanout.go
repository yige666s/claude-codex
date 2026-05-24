package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const DefaultJobEventFanoutChannel = "agentapi:job-events"

type JobEventPublisher interface {
	PublishJobEvent(ctx context.Context, event *JobEvent) error
}

type RedisJobEventFanoutConfig struct {
	Channel string
	Origin  string
}

func (c RedisJobEventFanoutConfig) normalized() RedisJobEventFanoutConfig {
	out := c
	if strings.TrimSpace(out.Channel) == "" {
		out.Channel = DefaultJobEventFanoutChannel
	}
	if strings.TrimSpace(out.Origin) == "" {
		out.Origin = defaultJobEventFanoutOrigin()
	}
	return out
}

type RedisJobEventFanout struct {
	client redis.UniversalClient
	config RedisJobEventFanoutConfig
	logger *log.Logger
}

func NewRedisJobEventFanout(client redis.UniversalClient, config RedisJobEventFanoutConfig, logger *log.Logger) *RedisJobEventFanout {
	if logger == nil {
		logger = log.Default()
	}
	return &RedisJobEventFanout{client: client, config: config.normalized(), logger: logger}
}

func (f *RedisJobEventFanout) PublishJobEvent(ctx context.Context, event *JobEvent) error {
	if f == nil || f.client == nil || event == nil {
		return nil
	}
	payload, err := json.Marshal(redisJobEventFanoutMessage{
		Origin: f.config.Origin,
		Event:  event,
		SentAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	return f.client.Publish(ctx, f.config.Channel, payload).Err()
}

func (f *RedisJobEventFanout) Run(ctx context.Context, onEvent func(*JobEvent)) error {
	if f == nil || f.client == nil || onEvent == nil {
		return nil
	}
	pubsub := f.client.Subscribe(ctx, f.config.Channel)
	defer pubsub.Close()
	if _, err := pubsub.Receive(ctx); err != nil {
		return err
	}
	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case message, ok := <-ch:
			if !ok {
				return nil
			}
			event, ok := f.decode(message.Payload)
			if !ok {
				continue
			}
			onEvent(event)
		}
	}
}

func (f *RedisJobEventFanout) decode(payload string) (*JobEvent, bool) {
	var message redisJobEventFanoutMessage
	if err := json.Unmarshal([]byte(payload), &message); err != nil {
		if f.logger != nil {
			f.logger.Printf("decode redis job event fanout: %v", err)
		}
		return nil, false
	}
	if strings.TrimSpace(message.Origin) == strings.TrimSpace(f.config.Origin) {
		return nil, false
	}
	if message.Event == nil || strings.TrimSpace(message.Event.ID) == "" || strings.TrimSpace(message.Event.JobID) == "" {
		return nil, false
	}
	return message.Event, true
}

type redisJobEventFanoutMessage struct {
	Origin string    `json:"origin"`
	Event  *JobEvent `json:"event"`
	SentAt time.Time `json:"sent_at"`
}

func defaultJobEventFanoutOrigin() string {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "agentapi"
	}
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), newSortableID())
}
