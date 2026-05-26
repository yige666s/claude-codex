package run

import (
	"strings"
	"time"

	"claude-codex/internal/backend/agentruntime"
)

func buildKafkaMessageEventConsumerWorker(
	config agentruntime.KafkaMessageEventConfig,
	searchConfig agentruntime.MessageSearchConfig,
	sessionStore agentruntime.SessionStore,
	processedLockBackend string,
	processedLockRedisURL string,
	processedLockTTL time.Duration,
) (*agentruntime.KafkaMessageEventConsumerWorker, interface{ Close() error }) {
	reader, err := agentruntime.NewKafkaMessageEventReader(config)
	if err != nil {
		logFatalf("init kafka message event consumer reader: %v", err)
	}
	handlers := make([]agentruntime.MessageEventHandler, 0, 2)
	if agentruntime.MessageFullTextIndexingEnabled(searchConfig) {
		handlers = append(handlers, agentruntime.NewMessageFullTextIndexEventHandler(
			agentruntime.NewHTTPMessageFullTextIndexer(searchConfig),
		))
	}
	if agentruntime.MessageVectorIndexingEnabled(searchConfig) {
		metaStore, ok := sessionStore.(agentruntime.MessageEmbeddingMetaStore)
		if !ok {
			logFatalf("kafka message vector indexing requires a message embedding meta store")
		}
		indexer := agentruntime.NewQdrantMessageVectorIndexer(searchConfig, metaStore)
		handlers = append(handlers, agentruntime.NewMessageVectorIndexEventHandler(indexer))
	}
	if len(handlers) == 0 {
		logFatalf("kafka message event consumer requires Elasticsearch/OpenSearch full-text indexing or Qdrant vector indexing configuration")
	}
	var handler agentruntime.MessageEventHandler = handlers[0]
	if len(handlers) > 1 {
		handler = agentruntime.CompositeMessageEventHandler(handlers)
	}
	consumer := agentruntime.NewKafkaMessageEventConsumerWorkerWithLogger(reader, handler, config, runLogger("kafka_message_event_consumer"))
	consumer.SetProcessor("search-index")
	if strings.TrimSpace(config.DLQTopic) != "" {
		dlqConfig := config
		dlqConfig.Topic = config.DLQTopic
		writer, err := agentruntime.NewKafkaMessageEventWriter(dlqConfig)
		if err != nil {
			logFatalf("init kafka message event dlq writer: %v", err)
		}
		consumer.SetDLQWriter(writer)
	}
	var redisClient interface{ Close() error }
	switch strings.ToLower(strings.TrimSpace(processedLockBackend)) {
	case "redis":
		client, err := agentruntime.NewRedisClientFromURL(processedLockRedisURL)
		if err != nil {
			logFatalf("init kafka message event processed lock redis client: %v", err)
		}
		consumer.SetProcessedLock(agentruntime.NewRedisMessageEventProcessedLock(client, agentruntime.RedisPrefixFromURL(processedLockRedisURL), processedLockTTL))
		redisClient = client
	case "none", "off", "disabled":
	default:
		logFatalf("unsupported message event processed lock backend: %s", processedLockBackend)
	}
	return consumer, redisClient
}

func buildMessageAttachmentContentIndexer(searchConfig agentruntime.MessageSearchConfig, sessionStore agentruntime.SessionStore) agentruntime.MessageAttachmentContentIndexer {
	indexers := make([]agentruntime.MessageAttachmentContentIndexer, 0, 2)
	if agentruntime.MessageFullTextIndexingEnabled(searchConfig) {
		indexers = append(indexers, agentruntime.NewHTTPMessageFullTextIndexer(searchConfig))
	}
	if agentruntime.MessageVectorIndexingEnabled(searchConfig) {
		metaStore, ok := sessionStore.(agentruntime.MessageEmbeddingMetaStore)
		if !ok {
			logInfof("message attachment vector indexing disabled: message embedding meta store is required")
		} else {
			indexers = append(indexers, agentruntime.NewQdrantMessageVectorIndexer(searchConfig, metaStore))
		}
	}
	switch len(indexers) {
	case 0:
		return nil
	case 1:
		return indexers[0]
	default:
		return agentruntime.CompositeMessageAttachmentContentIndexer(indexers)
	}
}
