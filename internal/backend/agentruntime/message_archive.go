package agentruntime

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"claude-codex/internal/harness/state"
)

const (
	defaultMessageArchivePrefix         = "message-archive"
	defaultMessageArchiveBatchSize      = 100
	defaultMessageArchiveAfter          = 30 * 24 * time.Hour
	defaultMessageArchivePollInterval   = time.Hour
	defaultMessageArchiveProcessTimeout = 2 * time.Minute
	messageArchivePayloadVersion        = 1
	messageArchivePreviewBytes          = 512
)

type MessageArchiveRecord struct {
	UserID            string
	SessionID         string
	MessageID         string
	ArchiveURI        string
	ArchiveChecksum   string
	ArchivedAt        time.Time
	ContentPreview    string
	ToolOutputPreview string
}

type MessageArchiveQueue interface {
	ListMessagesForArchival(ctx context.Context, cutoff time.Time, limit int) ([]state.Message, error)
	MarkMessagesArchived(ctx context.Context, records []MessageArchiveRecord, clearPayload bool) (int, error)
}

type MessageArchiveWorkerConfig struct {
	ArchiveAfter   time.Duration
	BatchSize      int
	PollInterval   time.Duration
	ProcessTimeout time.Duration
	ClearPGPayload bool
}

type MessageArchiveWorker struct {
	queue   MessageArchiveQueue
	archive *MessageArchiveObjectStore
	config  MessageArchiveWorkerConfig
	logger  *log.Logger
	now     func() time.Time
}

func NewMessageArchiveWorker(queue MessageArchiveQueue, archive *MessageArchiveObjectStore, config MessageArchiveWorkerConfig, logger *log.Logger) *MessageArchiveWorker {
	config = normalizeMessageArchiveWorkerConfig(config)
	if logger == nil {
		logger = log.Default()
	}
	return &MessageArchiveWorker{
		queue:   queue,
		archive: archive,
		config:  config,
		logger:  logger,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func normalizeMessageArchiveWorkerConfig(config MessageArchiveWorkerConfig) MessageArchiveWorkerConfig {
	if config.ArchiveAfter <= 0 {
		config.ArchiveAfter = defaultMessageArchiveAfter
	}
	if config.BatchSize <= 0 || config.BatchSize > 1000 {
		config.BatchSize = defaultMessageArchiveBatchSize
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaultMessageArchivePollInterval
	}
	if config.ProcessTimeout <= 0 {
		config.ProcessTimeout = defaultMessageArchiveProcessTimeout
	}
	return config
}

func (w *MessageArchiveWorker) Run(ctx context.Context) error {
	if w == nil || w.queue == nil || w.archive == nil {
		return fmt.Errorf("message archive worker is not configured")
	}
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		if _, err := w.ProcessBatch(ctx); err != nil && !errorsIsContextDone(ctx, err) {
			w.logger.Printf("message archive worker batch failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *MessageArchiveWorker) ProcessBatch(ctx context.Context) (int, error) {
	if w == nil || w.queue == nil || w.archive == nil {
		return 0, fmt.Errorf("message archive worker is not configured")
	}
	cutoff := w.now().Add(-w.config.ArchiveAfter)
	messages, err := w.queue.ListMessagesForArchival(ctx, cutoff, w.config.BatchSize)
	if err != nil {
		return 0, err
	}
	records := make([]MessageArchiveRecord, 0, len(messages))
	for _, message := range messages {
		processCtx, cancel := context.WithTimeout(ctx, w.config.ProcessTimeout)
		record, err := w.archive.WriteMessage(processCtx, message)
		cancel()
		if err != nil {
			w.logger.Printf("archive message failed: user=%s session=%s message=%s: %v", message.UserID, message.SessionID, message.ID, err)
			continue
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return len(messages), nil
	}
	if _, err := w.queue.MarkMessagesArchived(ctx, records, w.config.ClearPGPayload); err != nil {
		return len(messages), err
	}
	return len(messages), nil
}

type MessageArchiveObjectStore struct {
	objects ObjectStore
	prefix  string
}

func NewMessageArchiveObjectStore(objects ObjectStore, prefix string) *MessageArchiveObjectStore {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		prefix = defaultMessageArchivePrefix
	}
	return &MessageArchiveObjectStore{objects: objects, prefix: prefix}
}

type archivedMessagePayload struct {
	Version    int           `json:"version"`
	ArchivedAt time.Time     `json:"archived_at"`
	Message    state.Message `json:"message"`
}

func (s *MessageArchiveObjectStore) WriteMessage(ctx context.Context, message state.Message) (MessageArchiveRecord, error) {
	if s == nil || s.objects == nil {
		return MessageArchiveRecord{}, fmt.Errorf("message archive object store is not configured")
	}
	now := time.Now().UTC()
	stored := message
	stored.ArchiveURI = ""
	stored.ArchiveChecksum = ""
	stored.ArchivedAt = nil
	payload, err := json.Marshal(archivedMessagePayload{
		Version:    messageArchivePayloadVersion,
		ArchivedAt: now,
		Message:    stored,
	})
	if err != nil {
		return MessageArchiveRecord{}, err
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(payload); err != nil {
		_ = gz.Close()
		return MessageArchiveRecord{}, err
	}
	if err := gz.Close(); err != nil {
		return MessageArchiveRecord{}, err
	}
	data := compressed.Bytes()
	sum := sha256.Sum256(data)
	key := s.messageKey(message)
	if err := s.objects.Put(ctx, key, data, "application/gzip"); err != nil {
		return MessageArchiveRecord{}, err
	}
	return MessageArchiveRecord{
		UserID:            message.UserID,
		SessionID:         message.SessionID,
		MessageID:         message.ID,
		ArchiveURI:        key,
		ArchiveChecksum:   "sha256:" + hex.EncodeToString(sum[:]),
		ArchivedAt:        now,
		ContentPreview:    messageArchivePreview(message.Content),
		ToolOutputPreview: messageArchivePreview(message.ToolOutput),
	}, nil
}

func (s *MessageArchiveObjectStore) ReadMessage(ctx context.Context, uri, checksum string) (state.Message, error) {
	if s == nil || s.objects == nil {
		return state.Message{}, fmt.Errorf("message archive object store is not configured")
	}
	data, err := s.objects.Get(ctx, strings.TrimSpace(uri))
	if err != nil {
		return state.Message{}, err
	}
	if checksum = strings.TrimSpace(checksum); checksum != "" {
		sum := sha256.Sum256(data)
		got := "sha256:" + hex.EncodeToString(sum[:])
		if !strings.EqualFold(got, checksum) {
			return state.Message{}, fmt.Errorf("message archive checksum mismatch: got %s want %s", got, checksum)
		}
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return state.Message{}, err
	}
	defer gz.Close()
	raw, err := io.ReadAll(gz)
	if err != nil {
		return state.Message{}, err
	}
	var payload archivedMessagePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return state.Message{}, err
	}
	return payload.Message, nil
}

var archivePathUnsafe = regexp.MustCompile(`[^A-Za-z0-9._=-]+`)

func (s *MessageArchiveObjectStore) messageKey(message state.Message) string {
	createdAt := message.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	year, month, _ := createdAt.Date()
	name := fmt.Sprintf("%012d-%s.json.gz", message.SeqNo, safeArchivePathSegment(message.ID))
	return path.Join(
		s.prefix,
		fmt.Sprintf("year=%04d", year),
		fmt.Sprintf("month=%02d", int(month)),
		"user_hash="+userPathID(message.UserID),
		"session_id="+safeArchivePathSegment(message.SessionID),
		name,
	)
}

func safeArchivePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	value = archivePathUnsafe.ReplaceAllString(value, "_")
	value = strings.Trim(value, "._-")
	if value == "" {
		return "_"
	}
	return value
}

func messageArchivePreview(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= messageArchivePreviewBytes {
		return value
	}
	cut := messageArchivePreviewBytes
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut])
}
