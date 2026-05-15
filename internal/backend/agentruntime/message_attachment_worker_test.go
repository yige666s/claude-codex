package agentruntime

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestMessageAttachmentWorkerGeneratesImageThumbnail(t *testing.T) {
	ctx := context.Background()
	objects := NewFileObjectStore(t.TempDir())
	artifacts := NewArtifactService(newMemoryArtifactStore(), objects, "objects")
	var imageData bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 5), B: 120, A: 255})
		}
	}
	if err := png.Encode(&imageData, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	artifact, err := artifacts.Create(ctx, AssetKindAttachment, "alice", "session-1", "photo.png", "image/png", imageData.Bytes())
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}
	queue := &captureAttachmentProcessingQueue{}
	worker := NewMessageAttachmentWorker(queue, artifacts, MessageAttachmentWorkerConfig{ThumbnailMaxDimension: 16}, nil)
	if err := worker.ProcessAttachment(ctx, state.MessageAttachment{
		ID:         artifact.ID,
		MessageID:  "message-1",
		SessionID:  "session-1",
		UserID:     "alice",
		FileType:   messageAttachmentFileTypeImage,
		MimeType:   "image/png",
		FileName:   "photo.png",
		StorageKey: artifact.ObjectKey,
		CreatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("process image attachment: %v", err)
	}
	if queue.status != state.MessageAttachmentEmbeddingDone || queue.thumbnailKey == "" {
		t.Fatalf("expected done thumbnail update, got %#v", queue)
	}
	thumbnailData, err := objects.Get(ctx, queue.thumbnailKey)
	if err != nil {
		t.Fatalf("load thumbnail: %v", err)
	}
	thumbnail, _, err := image.Decode(bytes.NewReader(thumbnailData))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if thumbnail.Bounds().Dx() > 16 || thumbnail.Bounds().Dy() > 16 {
		t.Fatalf("thumbnail too large: %v", thumbnail.Bounds())
	}
}

func TestMessageAttachmentWorkerExtractsPDFText(t *testing.T) {
	ctx := context.Background()
	objects := NewFileObjectStore(t.TempDir())
	artifacts := NewArtifactService(newMemoryArtifactStore(), objects, "objects")
	pdf := []byte("%PDF-1.4\n1 0 obj\n<<>>\nstream\nBT (Hello PDF text) Tj ET\nendstream\nendobj\n%%EOF")
	artifact, err := artifacts.Create(ctx, AssetKindAttachment, "alice", "session-1", "report.pdf", "application/pdf", pdf)
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}
	queue := &captureAttachmentProcessingQueue{}
	worker := NewMessageAttachmentWorker(queue, artifacts, MessageAttachmentWorkerConfig{}, nil)
	if err := worker.ProcessAttachment(ctx, state.MessageAttachment{
		ID:         artifact.ID,
		MessageID:  "message-1",
		SessionID:  "session-1",
		UserID:     "alice",
		FileType:   messageAttachmentFileTypeDocument,
		MimeType:   "application/pdf",
		FileName:   "report.pdf",
		StorageKey: artifact.ObjectKey,
		CreatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("process pdf attachment: %v", err)
	}
	if queue.status != state.MessageAttachmentEmbeddingDone || queue.extractedTextKey == "" {
		t.Fatalf("expected done extracted-text update, got %#v", queue)
	}
	textData, err := objects.Get(ctx, queue.extractedTextKey)
	if err != nil {
		t.Fatalf("load extracted text: %v", err)
	}
	if got := string(textData); got != "Hello PDF text" {
		t.Fatalf("unexpected extracted text %q", got)
	}
}

func TestMessageAttachmentWorkerIndexesExtractedText(t *testing.T) {
	ctx := context.Background()
	objects := NewFileObjectStore(t.TempDir())
	artifacts := NewArtifactService(newMemoryArtifactStore(), objects, "objects")
	artifact, err := artifacts.Create(ctx, AssetKindAttachment, "alice", "session-1", "notes.txt", "text/plain", []byte("attachment search text"))
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}
	queue := &captureAttachmentProcessingQueue{}
	indexer := &captureAttachmentContentIndexer{}
	worker := NewMessageAttachmentWorker(queue, artifacts, MessageAttachmentWorkerConfig{ContentIndexer: indexer}, nil)
	if err := worker.ProcessAttachment(ctx, state.MessageAttachment{
		ID:         artifact.ID,
		MessageID:  "message-1",
		SessionID:  "session-1",
		UserID:     "alice",
		FileType:   messageAttachmentFileTypeText,
		MimeType:   "text/plain",
		FileName:   "notes.txt",
		StorageKey: artifact.ObjectKey,
		CreatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("process text attachment: %v", err)
	}
	if queue.status != state.MessageAttachmentEmbeddingDone || queue.extractedTextKey == "" {
		t.Fatalf("expected done extracted-text update, got %#v", queue)
	}
	if len(indexer.items) != 1 || indexer.items[0].attachment.ID != artifact.ID || indexer.items[0].text != "attachment search text" {
		t.Fatalf("expected extracted attachment text to be indexed, got %#v", indexer.items)
	}
}

type captureAttachmentProcessingQueue struct {
	status           int
	thumbnailKey     string
	extractedTextKey string
}

func (q *captureAttachmentProcessingQueue) ListPendingMessageAttachmentsForProcessing(context.Context, int) ([]state.MessageAttachment, error) {
	return nil, nil
}

func (q *captureAttachmentProcessingQueue) UpdateMessageAttachmentProcessing(_ context.Context, _, _, _ string, status int, thumbnailKey, extractedTextKey string) error {
	q.status = status
	q.thumbnailKey = thumbnailKey
	q.extractedTextKey = extractedTextKey
	return nil
}

type captureAttachmentContentIndexer struct {
	items []struct {
		attachment state.MessageAttachment
		text       string
	}
}

func (i *captureAttachmentContentIndexer) IndexAttachmentText(_ context.Context, attachment state.MessageAttachment, text string) error {
	i.items = append(i.items, struct {
		attachment state.MessageAttachment
		text       string
	}{attachment: attachment, text: text})
	return nil
}
