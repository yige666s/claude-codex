package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	DefaultJobQueueStream        = "agentapi:jobs"
	DefaultJobQueueConsumerGroup = "agentapi-job-workers"
	DefaultJobQueueBlockTimeout  = 2 * time.Second
	DefaultJobQueueClaimIdle     = time.Minute
	DefaultJobQueueLockTTL       = 2 * time.Minute
)

type JobQueueItem struct {
	JobID           string `json:"job_id"`
	UserID          string `json:"user_id"`
	RequestID       string `json:"request_id,omitempty"`
	HideUserMessage bool   `json:"hide_user_message,omitempty"`
}

type JobQueueMessage struct {
	ID   string
	Item JobQueueItem
}

type JobQueue interface {
	EnqueueJob(ctx context.Context, item JobQueueItem) error
}

type JobQueueConsumer interface {
	JobQueue
	Ensure(ctx context.Context) error
	ReceiveJob(ctx context.Context) (*JobQueueMessage, error)
	AckJob(ctx context.Context, message *JobQueueMessage) error
	Close() error
}

type JobQueueLock interface {
	Refresh(ctx context.Context) error
	Release(ctx context.Context) error
}

type JobQueueLocker interface {
	AcquireJobLock(ctx context.Context, jobID string, ttl time.Duration) (JobQueueLock, bool, error)
}

type RedisJobQueueConfig struct {
	Stream       string
	Group        string
	Consumer     string
	BlockTimeout time.Duration
	ClaimIdle    time.Duration
	LockTTL      time.Duration
}

func (c RedisJobQueueConfig) normalized() RedisJobQueueConfig {
	out := c
	if strings.TrimSpace(out.Stream) == "" {
		out.Stream = DefaultJobQueueStream
	}
	if strings.TrimSpace(out.Group) == "" {
		out.Group = DefaultJobQueueConsumerGroup
	}
	if strings.TrimSpace(out.Consumer) == "" {
		out.Consumer = defaultJobQueueConsumerName()
	}
	if out.BlockTimeout <= 0 {
		out.BlockTimeout = DefaultJobQueueBlockTimeout
	}
	if out.ClaimIdle <= 0 {
		out.ClaimIdle = DefaultJobQueueClaimIdle
	}
	if out.LockTTL <= 0 {
		out.LockTTL = DefaultJobQueueLockTTL
	}
	return out
}

type RedisJobQueue struct {
	client redis.UniversalClient
	config RedisJobQueueConfig
}

func NewRedisJobQueue(client redis.UniversalClient, config RedisJobQueueConfig) *RedisJobQueue {
	return &RedisJobQueue{client: client, config: config.normalized()}
}

func (q *RedisJobQueue) Ensure(ctx context.Context) error {
	if q == nil || q.client == nil {
		return fmt.Errorf("redis job queue is not configured")
	}
	err := q.client.XGroupCreateMkStream(ctx, q.config.Stream, q.config.Group, "0").Err()
	if err != nil && !strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP") {
		return err
	}
	return nil
}

func (q *RedisJobQueue) EnqueueJob(ctx context.Context, item JobQueueItem) error {
	if q == nil || q.client == nil {
		return fmt.Errorf("redis job queue is not configured")
	}
	item.JobID = strings.TrimSpace(item.JobID)
	item.UserID = strings.TrimSpace(item.UserID)
	if item.JobID == "" {
		return fmt.Errorf("job id is required")
	}
	if item.UserID == "" {
		return fmt.Errorf("job user id is required")
	}
	values := map[string]any{
		"job_id":            item.JobID,
		"user_id":           item.UserID,
		"request_id":        item.RequestID,
		"hide_user_message": strconv.FormatBool(item.HideUserMessage),
	}
	return q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.config.Stream,
		Values: values,
	}).Err()
}

func (q *RedisJobQueue) ReceiveJob(ctx context.Context) (*JobQueueMessage, error) {
	if q == nil || q.client == nil {
		return nil, fmt.Errorf("redis job queue is not configured")
	}
	if message, err := q.claimPendingJob(ctx); err != nil {
		return nil, err
	} else if message != nil {
		return message, nil
	}
	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    q.config.Group,
		Consumer: q.config.Consumer,
		Streams:  []string{q.config.Stream, ">"},
		Count:    1,
		Block:    q.config.BlockTimeout,
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) || ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return firstJobQueueMessage(streams)
}

func (q *RedisJobQueue) claimPendingJob(ctx context.Context) (*JobQueueMessage, error) {
	messages, _, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   q.config.Stream,
		Group:    q.config.Group,
		Consumer: q.config.Consumer,
		MinIdle:  q.config.ClaimIdle,
		Start:    "0-0",
		Count:    1,
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}
	return decodeJobQueueMessage(messages[0])
}

func (q *RedisJobQueue) AckJob(ctx context.Context, message *JobQueueMessage) error {
	if q == nil || q.client == nil || message == nil || strings.TrimSpace(message.ID) == "" {
		return nil
	}
	return q.client.XAck(ctx, q.config.Stream, q.config.Group, message.ID).Err()
}

func (q *RedisJobQueue) AcquireJobLock(ctx context.Context, jobID string, ttl time.Duration) (JobQueueLock, bool, error) {
	if q == nil || q.client == nil {
		return nil, false, fmt.Errorf("redis job queue is not configured")
	}
	if ttl <= 0 {
		ttl = q.config.LockTTL
	}
	token := newSortableID()
	key := q.jobLockKey(jobID)
	acquired, err := q.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil || !acquired {
		return nil, acquired, err
	}
	return &redisJobQueueLock{client: q.client, key: key, token: token, ttl: ttl}, true, nil
}

func (q *RedisJobQueue) Close() error {
	if q == nil || q.client == nil {
		return nil
	}
	return q.client.Close()
}

func (q *RedisJobQueue) jobLockKey(jobID string) string {
	stream := strings.TrimSpace(q.config.Stream)
	if stream == "" {
		stream = DefaultJobQueueStream
	}
	return stream + ":lock:" + strings.TrimSpace(jobID)
}

type redisJobQueueLock struct {
	client redis.UniversalClient
	key    string
	token  string
	ttl    time.Duration
}

func (l *redisJobQueueLock) Refresh(ctx context.Context) error {
	if l == nil || l.client == nil {
		return nil
	}
	ok, err := l.client.Eval(ctx, redisRefreshJobLockScript, []string{l.key}, l.token, int64(l.ttl/time.Millisecond)).Bool()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("job lock lost")
	}
	return nil
}

func (l *redisJobQueueLock) Release(ctx context.Context) error {
	if l == nil || l.client == nil {
		return nil
	}
	return l.client.Eval(ctx, redisReleaseJobLockScript, []string{l.key}, l.token).Err()
}

const redisRefreshJobLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`

const redisReleaseJobLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`

func firstJobQueueMessage(streams []redis.XStream) (*JobQueueMessage, error) {
	for _, stream := range streams {
		for _, message := range stream.Messages {
			return decodeJobQueueMessage(message)
		}
	}
	return nil, nil
}

func decodeJobQueueMessage(message redis.XMessage) (*JobQueueMessage, error) {
	item := JobQueueItem{
		JobID:     jobQueueStringValue(message.Values["job_id"]),
		UserID:    jobQueueStringValue(message.Values["user_id"]),
		RequestID: jobQueueStringValue(message.Values["request_id"]),
	}
	if rawJSON := strings.TrimSpace(jobQueueStringValue(message.Values["payload"])); rawJSON != "" {
		_ = json.Unmarshal([]byte(rawJSON), &item)
	}
	item.HideUserMessage, _ = strconv.ParseBool(jobQueueStringValue(message.Values["hide_user_message"]))
	if strings.TrimSpace(item.JobID) == "" || strings.TrimSpace(item.UserID) == "" {
		return nil, fmt.Errorf("invalid job queue message %s", message.ID)
	}
	return &JobQueueMessage{ID: message.ID, Item: item}, nil
}

func jobQueueStringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func defaultJobQueueConsumerName() string {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "agentapi"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

type JobWorkerConfig struct {
	LockTTL      time.Duration
	LockRefresh  time.Duration
	IdleBackoff  time.Duration
	ErrorBackoff time.Duration
}

func (c JobWorkerConfig) normalized() JobWorkerConfig {
	out := c
	if out.LockTTL <= 0 {
		out.LockTTL = DefaultJobQueueLockTTL
	}
	if out.LockRefresh <= 0 {
		out.LockRefresh = out.LockTTL / 3
	}
	if out.LockRefresh <= 0 {
		out.LockRefresh = time.Second
	}
	if out.IdleBackoff <= 0 {
		out.IdleBackoff = 250 * time.Millisecond
	}
	if out.ErrorBackoff <= 0 {
		out.ErrorBackoff = time.Second
	}
	return out
}

type JobWorker struct {
	queue              JobQueueConsumer
	runner             *Runtime
	config             JobWorkerConfig
	logger             *slog.Logger
	idleRetryPolicy    RetryPolicy
	receiveRetryPolicy RetryPolicy
}

func NewJobWorker(queue JobQueueConsumer, runner *Runtime, config JobWorkerConfig, logger *log.Logger) *JobWorker {
	return NewJobWorkerWithLogger(queue, runner, config, newStructuredLogger(logger))
}

func NewJobWorkerWithLogger(queue JobQueueConsumer, runner *Runtime, config JobWorkerConfig, logger *slog.Logger) *JobWorker {
	config = config.normalized()
	return &JobWorker{
		queue:  queue,
		runner: runner,
		config: config,
		logger: componentLogger(logger, "job_worker"),
		idleRetryPolicy: RetryPolicy{
			BaseDelay: config.IdleBackoff,
			MaxDelay:  config.IdleBackoff,
		},
		receiveRetryPolicy: RetryPolicy{
			BaseDelay: config.ErrorBackoff,
			MaxDelay:  config.ErrorBackoff,
		},
	}
}

func (w *JobWorker) Run(ctx context.Context) error {
	if w == nil || w.queue == nil || w.runner == nil {
		return nil
	}
	if err := w.queue.Ensure(ctx); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		message, err := w.queue.ReceiveJob(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if w.logger != nil {
				logError(ctx, w.logger, "job worker receive failed", err)
			}
			if err := w.receiveRetryPolicy.Sleep(ctx, 1, err); err != nil {
				return err
			}
			continue
		}
		if message == nil {
			if err := w.idleRetryPolicy.Sleep(ctx, 1, nil); err != nil {
				return err
			}
			continue
		}
		ack, err := w.process(ctx, message)
		if err != nil && w.logger != nil {
			logError(ctx, w.logger, "job worker process failed", err,
				slog.String("job_id", message.Item.JobID),
				slog.String("message_id", message.ID),
			)
		}
		if ack {
			if err := w.queue.AckJob(ctx, message); err != nil && w.logger != nil {
				logError(ctx, w.logger, "job worker ack failed", err,
					slog.String("job_id", message.Item.JobID),
					slog.String("message_id", message.ID),
				)
			}
		}
	}
}

func (w *JobWorker) process(ctx context.Context, message *JobQueueMessage) (bool, error) {
	var lock JobQueueLock
	if locker, ok := w.queue.(JobQueueLocker); ok {
		acquiredLock, acquired, err := locker.AcquireJobLock(ctx, message.Item.JobID, w.config.LockTTL)
		if err != nil {
			return false, err
		}
		if !acquired {
			return false, nil
		}
		lock = acquiredLock
		defer func() { _ = lock.Release(context.Background()) }()
	}
	stopRefresh := startJobLockRefresh(ctx, lock, w.config.LockRefresh)
	defer stopRefresh()
	if err := w.runner.RunQueuedJob(ctx, message.Item); err != nil {
		return false, err
	}
	return true, nil
}

func startJobLockRefresh(ctx context.Context, lock JobQueueLock, interval time.Duration) func() {
	if lock == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = time.Second
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = lock.Refresh(ctx)
			}
		}
	}()
	return func() { close(done) }
}
