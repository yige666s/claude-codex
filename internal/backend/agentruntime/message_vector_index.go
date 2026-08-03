package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"claude-codex/internal/backend/httpclient"
	workerlifecycle "claude-codex/internal/backend/workers"
	"claude-codex/internal/harness/state"
)

const (
	defaultMessageVectorIndexQueueSize = 256
	defaultMessageVectorIndexWorkers   = 2
)

type MessageEmbeddingMetaStore interface {
	SaveMessageEmbeddingMeta(ctx context.Context, meta MessageEmbeddingMeta) error
}

type MessageVectorIndexer interface {
	IndexMessage(ctx context.Context, message state.Message) error
}

type MessageVectorDeleter interface {
	DeleteMessage(ctx context.Context, message state.Message) error
}

type QdrantMessageVectorIndexer struct {
	endpoint        string
	collection      string
	apiKey          string
	vectorSize      int
	modelVersion    string
	client          *http.Client
	embedder        QueryEmbedder
	metaStore       MessageEmbeddingMetaStore
	collectionMu    sync.Mutex
	collectionReady bool
}

func NewQdrantMessageVectorIndexer(config MessageSearchConfig, metaStore MessageEmbeddingMetaStore) *QdrantMessageVectorIndexer {
	config = normalizeMessageSearchConfig(config)
	embedConfig := config
	if strings.TrimSpace(config.EmbeddingIndexTaskType) != "" {
		embedConfig.EmbeddingTaskType = config.EmbeddingIndexTaskType
	}
	return &QdrantMessageVectorIndexer{
		endpoint:     strings.TrimRight(strings.TrimSpace(config.QdrantEndpoint), "/"),
		collection:   strings.TrimSpace(config.QdrantCollection),
		apiKey:       strings.TrimSpace(config.QdrantAPIKey),
		vectorSize:   config.EmbeddingDimensions,
		modelVersion: messageEmbeddingModelVersion(config),
		client:       &http.Client{Timeout: config.Timeout},
		embedder:     NewMessageQueryEmbedder(embedConfig),
		metaStore:    metaStore,
	}
}

func (i *QdrantMessageVectorIndexer) IndexMessage(ctx context.Context, message state.Message) error {
	if i == nil || i.endpoint == "" || i.collection == "" || i.embedder == nil {
		return errMessageSearchNotConfigured("qdrant vector indexer")
	}
	text := messageVectorIndexText(message)
	if text == "" {
		return nil
	}
	vector, err := i.embedder.EmbedQuery(ctx, text)
	if err != nil {
		return err
	}
	if len(vector) == 0 {
		return fmt.Errorf("message vector indexer received empty embedding")
	}
	if err := i.ensureCollection(ctx, len(vector)); err != nil {
		return err
	}
	vectorID := messageVectorID(message.UserID, message.ID, 0)
	payload := map[string]any{
		"message_id":    message.ID,
		"session_id":    message.SessionID,
		"user_id":       message.UserID,
		"seq_no":        message.SeqNo,
		"message_index": maxInt(int(message.SeqNo-1), 0),
		"role":          message.Role,
		"content":       text,
		"created_at":    message.CreatedAt.UTC().Format(time.RFC3339Nano),
		"status":        normalizedMessageStatus(message.Status),
		"hidden":        message.Hidden,
		"content_type":  message.ContentType,
	}
	if err := i.upsertPoint(ctx, vectorID, vector, payload); err != nil {
		return err
	}
	if i.metaStore != nil {
		return i.metaStore.SaveMessageEmbeddingMeta(ctx, MessageEmbeddingMeta{
			EmbeddingID:  messageVectorEmbeddingID(message.UserID, message.ID, 0, i.modelVersion),
			MessageID:    message.ID,
			SessionID:    message.SessionID,
			UserID:       message.UserID,
			ChunkIndex:   0,
			VectorID:     vectorID,
			ModelVersion: i.modelVersion,
			CreatedAt:    time.Now().UTC(),
		})
	}
	return nil
}

func (i *QdrantMessageVectorIndexer) IndexAttachmentText(ctx context.Context, attachment state.MessageAttachment, text string) error {
	if i == nil || i.endpoint == "" || i.collection == "" || i.embedder == nil {
		return errMessageSearchNotConfigured("qdrant vector indexer")
	}
	if !attachmentIndexable(attachment, text) {
		return nil
	}
	chunks := attachmentTextChunks(text, defaultAttachmentChunkSize, defaultAttachmentChunkOverlap)
	for chunkIndex, chunk := range chunks {
		indexText := attachmentIndexText(attachment, chunk)
		vector, err := i.embedder.EmbedQuery(ctx, indexText)
		if err != nil {
			return err
		}
		if len(vector) == 0 {
			return fmt.Errorf("message attachment vector indexer received empty embedding")
		}
		if err := i.ensureCollection(ctx, len(vector)); err != nil {
			return err
		}
		vectorID := messageAttachmentVectorID(attachment.UserID, attachment.MessageID, attachment.ID, chunkIndex)
		payload := messageAttachmentVectorPayload(attachment, indexText, chunkIndex)
		if err := i.upsertPoint(ctx, vectorID, vector, payload); err != nil {
			return err
		}
		if i.metaStore != nil {
			if err := i.metaStore.SaveMessageEmbeddingMeta(ctx, MessageEmbeddingMeta{
				EmbeddingID:  messageAttachmentVectorEmbeddingID(attachment.UserID, attachment.MessageID, attachment.ID, chunkIndex, i.modelVersion),
				MessageID:    attachment.MessageID,
				SessionID:    attachment.SessionID,
				UserID:       attachment.UserID,
				ChunkIndex:   chunkIndex,
				VectorID:     vectorID,
				ModelVersion: i.modelVersion,
				CreatedAt:    time.Now().UTC(),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *QdrantMessageVectorIndexer) DeleteMessage(ctx context.Context, message state.Message) error {
	if i == nil || i.endpoint == "" || i.collection == "" {
		return errMessageSearchNotConfigured("qdrant vector indexer")
	}
	if strings.TrimSpace(message.ID) == "" || strings.TrimSpace(message.UserID) == "" {
		return nil
	}
	body := map[string]any{
		"points": []string{messageVectorID(message.UserID, message.ID, 0)},
	}
	if err := i.postJSON(ctx, joinEndpointPath(i.endpoint, "collections", i.collection, "points", "delete")+"?wait=true", body); err != nil {
		return err
	}
	return i.deleteAttachmentsByMessage(ctx, message.UserID, message.ID)
}

func (i *QdrantMessageVectorIndexer) DeleteAttachmentText(ctx context.Context, attachment state.MessageAttachment) error {
	if i == nil || i.endpoint == "" || i.collection == "" {
		return errMessageSearchNotConfigured("qdrant vector indexer")
	}
	if strings.TrimSpace(attachment.UserID) == "" || strings.TrimSpace(attachment.MessageID) == "" || strings.TrimSpace(attachment.ID) == "" {
		return nil
	}
	body := map[string]any{
		"filter": qdrantAttachmentFilter(attachment.UserID, attachment.MessageID, attachment.ID),
	}
	return i.postJSON(ctx, joinEndpointPath(i.endpoint, "collections", i.collection, "points", "delete")+"?wait=true", body)
}

func (i *QdrantMessageVectorIndexer) deleteAttachmentsByMessage(ctx context.Context, userID, messageID string) error {
	userID = strings.TrimSpace(userID)
	messageID = strings.TrimSpace(messageID)
	if userID == "" || messageID == "" {
		return nil
	}
	body := map[string]any{
		"filter": qdrantAttachmentFilter(userID, messageID, ""),
	}
	return i.postJSON(ctx, joinEndpointPath(i.endpoint, "collections", i.collection, "points", "delete")+"?wait=true", body)
}

func (i *QdrantMessageVectorIndexer) ensureCollection(ctx context.Context, vectorSize int) error {
	if vectorSize <= 0 {
		return fmt.Errorf("qdrant collection vector size is required")
	}
	i.collectionMu.Lock()
	defer i.collectionMu.Unlock()
	if i.collectionReady {
		return nil
	}
	headers := make(http.Header)
	if i.apiKey != "" {
		headers.Set("api-key", i.apiKey)
	}
	status, bodyBytes, _, err := httpclient.New(
		httpclient.WithHTTPClient(i.client),
		httpclient.WithComponent("qdrant_message_vector"),
	).Bytes(ctx, http.MethodGet, joinEndpointPath(i.endpoint, "collections", i.collection), nil,
		httpclient.WithHeaders(headers),
		httpclient.WithOKStatuses(http.StatusOK, http.StatusNotFound),
	)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		i.collectionReady = true
		return nil
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("qdrant collection check failed: status %d: %s", status, strings.TrimSpace(string(bodyBytes)))
	}
	createBody := map[string]any{
		"vectors": map[string]any{
			"size":     vectorSize,
			"distance": "Cosine",
		},
	}
	if err := i.putJSON(ctx, joinEndpointPath(i.endpoint, "collections", i.collection), createBody); err != nil {
		return err
	}
	i.collectionReady = true
	return nil
}

func (i *QdrantMessageVectorIndexer) upsertPoint(ctx context.Context, vectorID string, vector []float32, payload map[string]any) error {
	body := map[string]any{
		"points": []map[string]any{
			{
				"id":      vectorID,
				"vector":  vector,
				"payload": payload,
			},
		},
	}
	return i.putJSON(ctx, joinEndpointPath(i.endpoint, "collections", i.collection, "points")+"?wait=true", body)
}

func (i *QdrantMessageVectorIndexer) putJSON(ctx context.Context, url string, payload any) error {
	return i.writeJSON(ctx, http.MethodPut, url, payload)
}

func (i *QdrantMessageVectorIndexer) postJSON(ctx context.Context, url string, payload any) error {
	return i.writeJSON(ctx, http.MethodPost, url, payload)
}

func (i *QdrantMessageVectorIndexer) writeJSON(ctx context.Context, method, url string, payload any) error {
	headers := make(http.Header)
	if i.apiKey != "" {
		headers.Set("api-key", i.apiKey)
	}
	err := httpclient.New(
		httpclient.WithHTTPClient(i.client),
		httpclient.WithComponent("qdrant_message_vector"),
	).JSON(ctx, method, url, payload, nil, httpclient.WithHeaders(headers))
	if err != nil {
		var statusErr *httpclient.StatusError
		if errors.As(err, &statusErr) {
			return fmt.Errorf("qdrant vector index write failed: %s: %s", statusErr.Status, strings.TrimSpace(statusErr.Body))
		}
		return err
	}
	return nil
}

type AsyncMessageVectorIndexPublisher struct {
	indexer MessageVectorIndexer
	queue   chan MessageEvent
	ctx     context.Context
	group   *workerlifecycle.Group
	logger  *slog.Logger
}

func NewAsyncMessageVectorIndexPublisher(indexer MessageVectorIndexer, workers, queueSize int) *AsyncMessageVectorIndexPublisher {
	return NewAsyncMessageVectorIndexPublisherWithLogger(indexer, workers, queueSize, nil)
}

func NewAsyncMessageVectorIndexPublisherWithLogger(indexer MessageVectorIndexer, workers, queueSize int, logger *slog.Logger) *AsyncMessageVectorIndexPublisher {
	if workers <= 0 {
		workers = defaultMessageVectorIndexWorkers
	}
	if queueSize <= 0 {
		queueSize = defaultMessageVectorIndexQueueSize
	}
	group := workerlifecycle.New(context.Background(), componentLogger(logger, "message_vector_index"))
	publisher := &AsyncMessageVectorIndexPublisher{
		indexer: indexer,
		queue:   make(chan MessageEvent, queueSize),
		ctx:     group.Context(),
		group:   group,
		logger:  componentLogger(logger, "message_vector_index"),
	}
	for worker := 0; worker < workers; worker++ {
		workerName := fmt.Sprintf("message_vector_index_%d", worker+1)
		publisher.group.Start(workerName, publisher.run)
	}
	return publisher
}

func (p *AsyncMessageVectorIndexPublisher) PublishMessageEvent(ctx context.Context, event MessageEvent) error {
	if p == nil || p.indexer == nil {
		return nil
	}
	if event.Type == MessageEventCreated && !messageVectorIndexable(event.Message) {
		return nil
	}
	if event.Type == MessageEventDeleted {
		if _, ok := p.indexer.(MessageVectorDeleter); !ok {
			return nil
		}
	} else if event.Type != MessageEventCreated {
		return nil
	}
	select {
	case p.queue <- event:
	case <-ctx.Done():
	case <-p.ctx.Done():
	default:
		attrs := contextLogAttrs(ctx, event.UserID, event.SessionID, "")
		attrs = append(attrs, slog.String("message_id", event.Message.ID))
		logWarn(ctx, p.logger, "message vector index queue full", attrs...)
	}
	return nil
}

func (p *AsyncMessageVectorIndexPublisher) PublishMessageEventSynchronously(ctx context.Context, event MessageEvent) error {
	if p == nil || p.indexer == nil {
		return nil
	}
	switch event.Type {
	case MessageEventCreated:
		if !messageVectorIndexable(event.Message) {
			return nil
		}
		return p.indexer.IndexMessage(ctx, event.Message)
	case MessageEventDeleted:
		if deleter, ok := p.indexer.(MessageVectorDeleter); ok {
			return deleter.DeleteMessage(ctx, event.Message)
		}
	}
	return nil
}

func (p *AsyncMessageVectorIndexPublisher) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	return p.group.Stop(ctx)
}

func (p *AsyncMessageVectorIndexPublisher) run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-p.queue:
			runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			var err error
			if event.Type == MessageEventDeleted {
				if deleter, ok := p.indexer.(MessageVectorDeleter); ok {
					err = deleter.DeleteMessage(runCtx, event.Message)
				}
			} else {
				err = p.indexer.IndexMessage(runCtx, event.Message)
			}
			if err != nil {
				attrs := contextLogAttrs(runCtx, event.UserID, event.SessionID, "")
				attrs = append(attrs, slog.String("message_id", event.Message.ID))
				logError(runCtx, p.logger, "message vector index failed", err, attrs...)
			}
			cancel()
		}
	}
}

func messageVectorIndexable(message state.Message) bool {
	if strings.TrimSpace(message.ID) == "" || strings.TrimSpace(message.UserID) == "" || strings.TrimSpace(message.SessionID) == "" {
		return false
	}
	if message.Hidden || message.Role == state.MessageRoleTool {
		return false
	}
	status := normalizedMessageStatus(message.Status)
	if status != state.MessageStatusNormal {
		return false
	}
	return messageVectorIndexText(message) != ""
}

func messageVectorIndexText(message state.Message) string {
	parts := make([]string, 0, 4)
	if strings.TrimSpace(message.Content) != "" {
		parts = append(parts, strings.TrimSpace(message.Content))
	}
	blocks := message.ContentParts
	if len(blocks) == 0 {
		blocks = message.ContentBlocks
	}
	for _, block := range blocks {
		if strings.TrimSpace(block.Text) != "" {
			parts = append(parts, strings.TrimSpace(block.Text))
		}
		if strings.TrimSpace(block.Content) != "" {
			parts = append(parts, strings.TrimSpace(block.Content))
		}
	}
	if strings.TrimSpace(message.ToolOutput) != "" && message.Role != state.MessageRoleTool {
		parts = append(parts, strings.TrimSpace(message.ToolOutput))
	}
	return strings.Join(parts, "\n\n")
}

func normalizedMessageStatus(status int) int {
	if status == 0 {
		return state.MessageStatusNormal
	}
	return status
}

func messageEmbeddingModelVersion(config MessageSearchConfig) string {
	config = normalizeMessageSearchConfig(config)
	parts := []string{config.EmbeddingProvider, config.EmbeddingModel}
	if config.EmbeddingDimensions > 0 {
		parts = append(parts, fmt.Sprintf("%d", config.EmbeddingDimensions))
	}
	return strings.Join(parts, ":")
}

func messageVectorID(userID, messageID string, chunkIndex int) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.Join([]string{userID, messageID, fmt.Sprintf("%d", chunkIndex)}, ":"))).String()
}

func messageVectorEmbeddingID(userID, messageID string, chunkIndex int, modelVersion string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.Join([]string{userID, messageID, fmt.Sprintf("%d", chunkIndex), modelVersion}, ":"))).String()
}

func messageAttachmentVectorID(userID, messageID, attachmentID string, chunkIndex int) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.Join([]string{userID, messageID, attachmentID, fmt.Sprintf("%d", chunkIndex)}, ":"))).String()
}

func messageAttachmentVectorEmbeddingID(userID, messageID, attachmentID string, chunkIndex int, modelVersion string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.Join([]string{userID, messageID, attachmentID, fmt.Sprintf("%d", chunkIndex), modelVersion}, ":"))).String()
}

func messageAttachmentVectorPayload(attachment state.MessageAttachment, text string, chunkIndex int) map[string]any {
	createdAt := attachment.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return map[string]any{
		"message_id":    attachment.MessageID,
		"attachment_id": attachment.ID,
		"source_type":   messageIndexSourceAttachment,
		"session_id":    attachment.SessionID,
		"user_id":       attachment.UserID,
		"seq_no":        0,
		"message_index": 0,
		"chunk_index":   chunkIndex,
		"role":          messageIndexSourceAttachment,
		"content":       strings.TrimSpace(text),
		"created_at":    createdAt.UTC().Format(time.RFC3339Nano),
		"status":        state.MessageStatusNormal,
		"hidden":        false,
		"content_type":  messageIndexSourceAttachment,
		"file_name":     strings.TrimSpace(attachment.FileName),
		"file_type":     strings.TrimSpace(attachment.FileType),
		"mime_type":     strings.TrimSpace(attachment.MimeType),
	}
}

func qdrantAttachmentFilter(userID, messageID, attachmentID string) map[string]any {
	must := []map[string]any{
		{"key": "source_type", "match": map[string]any{"value": messageIndexSourceAttachment}},
		{"key": "user_id", "match": map[string]any{"value": strings.TrimSpace(userID)}},
		{"key": "message_id", "match": map[string]any{"value": strings.TrimSpace(messageID)}},
	}
	if strings.TrimSpace(attachmentID) != "" {
		must = append(must, map[string]any{"key": "attachment_id", "match": map[string]any{"value": strings.TrimSpace(attachmentID)}})
	}
	return map[string]any{"must": must}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
