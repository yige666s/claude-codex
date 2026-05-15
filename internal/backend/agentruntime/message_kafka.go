package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"

	"claude-codex/internal/harness/state"
)

const (
	defaultKafkaMessageTopic             = "agent.messages"
	defaultKafkaMessageConsumerGroup     = "agentapi-message-workers"
	defaultKafkaMessageConsumerProcessor = "vector"
	defaultKafkaMessageRetryAttempts     = 3
	defaultKafkaMessageRetryBackoff      = time.Second
	defaultKafkaMessageProcessTimeout    = 30 * time.Second
	defaultKafkaProcessedLockTTL         = 24 * time.Hour
	defaultKafkaProcessedLockPrefix      = "agent:message:processed"
)

type KafkaMessageEventConfig struct {
	Brokers        []string
	Topic          string
	ClientID       string
	GroupID        string
	DLQTopic       string
	RetryAttempts  int
	RetryBackoff   time.Duration
	ProcessTimeout time.Duration
}

func (c KafkaMessageEventConfig) normalized() KafkaMessageEventConfig {
	out := c
	out.Brokers = trimStringList(out.Brokers)
	if strings.TrimSpace(out.Topic) == "" {
		out.Topic = defaultKafkaMessageTopic
	}
	if strings.TrimSpace(out.ClientID) == "" {
		out.ClientID = "agentapi"
	}
	if strings.TrimSpace(out.GroupID) == "" {
		out.GroupID = defaultKafkaMessageConsumerGroup
	}
	if out.RetryAttempts <= 0 {
		out.RetryAttempts = defaultKafkaMessageRetryAttempts
	}
	if out.RetryBackoff <= 0 {
		out.RetryBackoff = defaultKafkaMessageRetryBackoff
	}
	if out.ProcessTimeout <= 0 {
		out.ProcessTimeout = defaultKafkaMessageProcessTimeout
	}
	return out
}

func NewKafkaMessageEventWriter(config KafkaMessageEventConfig) (*kafka.Writer, error) {
	config = config.normalized()
	if len(config.Brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers are required")
	}
	return &kafka.Writer{
		Addr:         kafka.TCP(config.Brokers...),
		Topic:        config.Topic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireAll,
		Async:        false,
		BatchTimeout: 10 * time.Millisecond,
		Transport:    &kafka.Transport{ClientID: config.ClientID},
	}, nil
}

func NewKafkaMessageEventReader(config KafkaMessageEventConfig) (*kafka.Reader, error) {
	config = config.normalized()
	if len(config.Brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers are required")
	}
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:        config.Brokers,
		Topic:          config.Topic,
		GroupID:        config.GroupID,
		Dialer:         &kafka.Dialer{ClientID: config.ClientID},
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: 0,
	}), nil
}

type MessageEventHandler interface {
	HandleMessageEvent(ctx context.Context, event MessageEvent) error
}

type MessageEventHandlerFunc func(ctx context.Context, event MessageEvent) error

func (f MessageEventHandlerFunc) HandleMessageEvent(ctx context.Context, event MessageEvent) error {
	return f(ctx, event)
}

type CompositeMessageEventHandler []MessageEventHandler

func (h CompositeMessageEventHandler) HandleMessageEvent(ctx context.Context, event MessageEvent) error {
	for _, handler := range h {
		if handler == nil {
			continue
		}
		if err := handler.HandleMessageEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

type MessageFullTextIndexEventHandler struct {
	indexer MessageFullTextIndexer
}

func NewMessageFullTextIndexEventHandler(indexer MessageFullTextIndexer) *MessageFullTextIndexEventHandler {
	return &MessageFullTextIndexEventHandler{indexer: indexer}
}

func (h *MessageFullTextIndexEventHandler) HandleMessageEvent(ctx context.Context, event MessageEvent) error {
	if h == nil || h.indexer == nil {
		return nil
	}
	switch event.Type {
	case MessageEventCreated:
		return h.indexer.IndexMessage(ctx, event.Message)
	case MessageEventDeleted:
		if deleter, ok := h.indexer.(MessageFullTextDeleter); ok {
			return deleter.DeleteMessage(ctx, event.Message)
		}
		deleted := event.Message
		deleted.Status = state.MessageStatusDeleted
		return h.indexer.IndexMessage(ctx, deleted)
	default:
		return nil
	}
}

type MessageVectorIndexEventHandler struct {
	indexer MessageVectorIndexer
}

func NewMessageVectorIndexEventHandler(indexer MessageVectorIndexer) *MessageVectorIndexEventHandler {
	return &MessageVectorIndexEventHandler{indexer: indexer}
}

func (h *MessageVectorIndexEventHandler) HandleMessageEvent(ctx context.Context, event MessageEvent) error {
	if h == nil || h.indexer == nil {
		return nil
	}
	switch event.Type {
	case MessageEventCreated:
		if !messageVectorIndexable(event.Message) {
			return nil
		}
		return h.indexer.IndexMessage(ctx, event.Message)
	case MessageEventDeleted:
		if deleter, ok := h.indexer.(MessageVectorDeleter); ok {
			return deleter.DeleteMessage(ctx, event.Message)
		}
		return nil
	default:
		return nil
	}
}

type MessageEventProcessedLock interface {
	AcquireMessageEvent(ctx context.Context, processor string, event MessageEvent) (bool, func(context.Context, bool) error, error)
}

type RedisMessageEventProcessedLock struct {
	client redis.UniversalClient
	prefix string
	ttl    time.Duration
}

func NewRedisMessageEventProcessedLock(client redis.UniversalClient, prefix string, ttl time.Duration) *RedisMessageEventProcessedLock {
	prefix = strings.TrimRight(strings.TrimSpace(prefix), ":")
	if prefix == "" {
		prefix = defaultKafkaProcessedLockPrefix
	}
	if ttl <= 0 {
		ttl = defaultKafkaProcessedLockTTL
	}
	return &RedisMessageEventProcessedLock{client: client, prefix: prefix, ttl: ttl}
}

func (l *RedisMessageEventProcessedLock) AcquireMessageEvent(ctx context.Context, processor string, event MessageEvent) (bool, func(context.Context, bool) error, error) {
	if l == nil || l.client == nil {
		return true, func(context.Context, bool) error { return nil }, nil
	}
	key := l.key(processor, event)
	if key == "" {
		return true, func(context.Context, bool) error { return nil }, nil
	}
	acquired, err := l.client.SetNX(ctx, key, "1", l.ttl).Result()
	if err != nil || !acquired {
		return acquired, func(context.Context, bool) error { return nil }, err
	}
	return true, func(ctx context.Context, success bool) error {
		if success {
			return nil
		}
		return l.client.Del(ctx, key).Err()
	}, nil
}

func (l *RedisMessageEventProcessedLock) key(processor string, event MessageEvent) string {
	messageID := strings.TrimSpace(event.Message.ID)
	if messageID == "" {
		return ""
	}
	processor = strings.TrimSpace(processor)
	if processor == "" {
		processor = defaultKafkaMessageConsumerProcessor
	}
	eventType := strings.TrimSpace(event.Type)
	if eventType == "" {
		eventType = "unknown"
	}
	return fmt.Sprintf("%s:%s:%s:%s", l.prefix, processor, eventType, messageID)
}

type kafkaMessageReader interface {
	FetchMessage(context.Context) (kafka.Message, error)
	CommitMessages(context.Context, ...kafka.Message) error
	Close() error
}

type kafkaMessageWriter interface {
	WriteMessages(context.Context, ...kafka.Message) error
	Close() error
}

type KafkaMessageEventConsumerWorker struct {
	reader         kafkaMessageReader
	dlqWriter      kafkaMessageWriter
	handler        MessageEventHandler
	lock           MessageEventProcessedLock
	processor      string
	retryAttempts  int
	retryBackoff   time.Duration
	processTimeout time.Duration
	logger         *log.Logger
}

func NewKafkaMessageEventConsumerWorker(reader kafkaMessageReader, handler MessageEventHandler, config KafkaMessageEventConfig) *KafkaMessageEventConsumerWorker {
	config = config.normalized()
	return &KafkaMessageEventConsumerWorker{
		reader:         reader,
		handler:        handler,
		processor:      defaultKafkaMessageConsumerProcessor,
		retryAttempts:  config.RetryAttempts,
		retryBackoff:   config.RetryBackoff,
		processTimeout: config.ProcessTimeout,
		logger:         log.Default(),
	}
}

func (w *KafkaMessageEventConsumerWorker) SetDLQWriter(writer kafkaMessageWriter) {
	if w != nil {
		w.dlqWriter = writer
	}
}

func (w *KafkaMessageEventConsumerWorker) SetProcessedLock(lock MessageEventProcessedLock) {
	if w != nil {
		w.lock = lock
	}
}

func (w *KafkaMessageEventConsumerWorker) SetProcessor(name string) {
	if w != nil && strings.TrimSpace(name) != "" {
		w.processor = strings.TrimSpace(name)
	}
}

func (w *KafkaMessageEventConsumerWorker) Run(ctx context.Context) error {
	if w == nil || w.reader == nil || w.handler == nil {
		return nil
	}
	for {
		message, err := w.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if w.logger != nil {
				w.logger.Printf("kafka message event fetch failed: %v", err)
			}
			continue
		}
		if err := w.processKafkaMessage(ctx, message); err != nil {
			if w.logger != nil {
				w.logger.Printf("kafka message event process failed: topic=%s partition=%d offset=%d: %v", message.Topic, message.Partition, message.Offset, err)
			}
			continue
		}
		if err := w.reader.CommitMessages(ctx, message); err != nil && w.logger != nil {
			w.logger.Printf("kafka message event commit failed: topic=%s partition=%d offset=%d: %v", message.Topic, message.Partition, message.Offset, err)
		}
	}
}

func (w *KafkaMessageEventConsumerWorker) Close() error {
	if w == nil || w.reader == nil {
		return nil
	}
	err := w.reader.Close()
	if w.dlqWriter != nil {
		err = errors.Join(err, w.dlqWriter.Close())
	}
	return err
}

func (w *KafkaMessageEventConsumerWorker) processKafkaMessage(ctx context.Context, message kafka.Message) error {
	var event MessageEvent
	if err := json.Unmarshal(message.Value, &event); err != nil {
		return w.publishDLQ(ctx, message, event, err)
	}
	if strings.TrimSpace(event.Type) == "" {
		return w.publishDLQ(ctx, message, event, fmt.Errorf("message event type is required"))
	}
	acquired := true
	release := func(context.Context, bool) error { return nil }
	var err error
	if w.lock != nil {
		acquired, release, err = w.lock.AcquireMessageEvent(ctx, w.processor, event)
		if err != nil {
			return err
		}
		if !acquired {
			return nil
		}
	}
	err = w.handleWithRetry(ctx, event)
	if releaseErr := release(ctx, err == nil); releaseErr != nil && err == nil {
		err = releaseErr
	}
	if err != nil {
		return w.publishDLQ(ctx, message, event, err)
	}
	return nil
}

func (w *KafkaMessageEventConsumerWorker) handleWithRetry(ctx context.Context, event MessageEvent) error {
	attempts := w.retryAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		processCtx := ctx
		cancel := func() {}
		if w.processTimeout > 0 {
			processCtx, cancel = context.WithTimeout(ctx, w.processTimeout)
		}
		lastErr = w.handler.HandleMessageEvent(processCtx, event)
		cancel()
		if lastErr == nil {
			return nil
		}
		if attempt == attempts {
			break
		}
		if err := sleepContext(ctx, w.retryBackoff); err != nil {
			return err
		}
	}
	return lastErr
}

func (w *KafkaMessageEventConsumerWorker) publishDLQ(ctx context.Context, message kafka.Message, event MessageEvent, cause error) error {
	if w.dlqWriter == nil {
		return cause
	}
	headers := append([]kafka.Header{}, message.Headers...)
	headers = append(headers, kafka.Header{Key: "x-error", Value: []byte(cause.Error())})
	headers = append(headers, kafka.Header{Key: "x-source-topic", Value: []byte(message.Topic)})
	dlqMessage := kafka.Message{
		Key:     message.Key,
		Value:   message.Value,
		Time:    time.Now().UTC(),
		Headers: headers,
	}
	if event.SessionID != "" && len(dlqMessage.Key) == 0 {
		dlqMessage.Key = []byte(event.SessionID)
	}
	if err := w.dlqWriter.WriteMessages(ctx, dlqMessage); err != nil {
		return fmt.Errorf("%w; dlq write failed: %v", cause, err)
	}
	return nil
}

func KafkaBrokerReadinessCheck(brokers []string) func(context.Context) error {
	brokers = trimStringList(brokers)
	return func(ctx context.Context) error {
		if len(brokers) == 0 {
			return fmt.Errorf("kafka brokers are not configured")
		}
		var lastErr error
		for _, broker := range brokers {
			conn, err := (&kafka.Dialer{}).DialContext(ctx, "tcp", broker)
			if err == nil {
				_ = conn.Close()
				return nil
			}
			lastErr = err
		}
		return lastErr
	}
}

func trimStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
