package agentruntime

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"claude-codex/internal/harness/state"
)

const (
	defaultAttachmentWorkerBatchSize       = 25
	defaultAttachmentWorkerPollInterval    = 5 * time.Second
	defaultAttachmentWorkerProcessTimeout  = 30 * time.Second
	defaultAttachmentThumbnailMaxDimension = 512
)

type MessageAttachmentWorkerConfig struct {
	BatchSize             int
	PollInterval          time.Duration
	ProcessTimeout        time.Duration
	ThumbnailMaxDimension int
	ContentIndexer        MessageAttachmentContentIndexer
}

type MessageAttachmentWorker struct {
	queue          MessageAttachmentProcessingQueue
	artifacts      *ArtifactService
	config         MessageAttachmentWorkerConfig
	contentIndexer MessageAttachmentContentIndexer
	logger         *log.Logger
}

func NewMessageAttachmentWorker(queue MessageAttachmentProcessingQueue, artifacts *ArtifactService, config MessageAttachmentWorkerConfig, logger *log.Logger) *MessageAttachmentWorker {
	config = normalizeMessageAttachmentWorkerConfig(config)
	if logger == nil {
		logger = log.Default()
	}
	return &MessageAttachmentWorker{queue: queue, artifacts: artifacts, config: config, contentIndexer: config.ContentIndexer, logger: logger}
}

func normalizeMessageAttachmentWorkerConfig(config MessageAttachmentWorkerConfig) MessageAttachmentWorkerConfig {
	if config.BatchSize <= 0 || config.BatchSize > 500 {
		config.BatchSize = defaultAttachmentWorkerBatchSize
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaultAttachmentWorkerPollInterval
	}
	if config.ProcessTimeout <= 0 {
		config.ProcessTimeout = defaultAttachmentWorkerProcessTimeout
	}
	if config.ThumbnailMaxDimension <= 0 {
		config.ThumbnailMaxDimension = defaultAttachmentThumbnailMaxDimension
	}
	return config
}

func (w *MessageAttachmentWorker) Run(ctx context.Context) error {
	if w == nil || w.queue == nil || w.artifacts == nil || w.artifacts.Objects == nil {
		return fmt.Errorf("message attachment worker is not configured")
	}
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		if _, err := w.ProcessBatch(ctx); err != nil && !errorsIsContextDone(ctx, err) {
			w.logger.Printf("message attachment worker batch failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *MessageAttachmentWorker) ProcessBatch(ctx context.Context) (int, error) {
	if w == nil || w.queue == nil {
		return 0, fmt.Errorf("message attachment worker queue is not configured")
	}
	items, err := w.queue.ListPendingMessageAttachmentsForProcessing(ctx, w.config.BatchSize)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		processCtx, cancel := context.WithTimeout(ctx, w.config.ProcessTimeout)
		err := w.ProcessAttachment(processCtx, item)
		cancel()
		if err != nil {
			w.logger.Printf("process message attachment failed: user=%s session=%s message=%s attachment=%s: %v", item.UserID, item.SessionID, item.MessageID, item.ID, err)
			_ = w.queue.UpdateMessageAttachmentProcessing(ctx, item.UserID, item.MessageID, item.ID, state.MessageAttachmentEmbeddingFailed, item.ThumbnailKey, item.ExtractedTextKey)
		}
	}
	return len(items), nil
}

func (w *MessageAttachmentWorker) ProcessAttachment(ctx context.Context, item state.MessageAttachment) error {
	if w == nil || w.artifacts == nil || w.artifacts.Objects == nil {
		return fmt.Errorf("artifact service is not configured")
	}
	artifact, data, err := w.artifacts.Get(ctx, item.UserID, item.ID, AssetKindAttachment)
	if err != nil {
		return err
	}
	item = mergeMessageAttachmentMetadata(item, artifact)
	result, err := w.processAttachmentData(ctx, item, data)
	if err != nil {
		return err
	}
	if w.contentIndexer != nil && strings.TrimSpace(result.extractedText) != "" {
		if err := w.contentIndexer.IndexAttachmentText(ctx, item, result.extractedText); err != nil {
			_ = w.queue.UpdateMessageAttachmentProcessing(ctx, item.UserID, item.MessageID, item.ID, state.MessageAttachmentEmbeddingFailed, result.thumbnailKey, result.extractedTextKey)
			return err
		}
	}
	return w.queue.UpdateMessageAttachmentProcessing(ctx, item.UserID, item.MessageID, item.ID, state.MessageAttachmentEmbeddingDone, result.thumbnailKey, result.extractedTextKey)
}

type processedAttachmentResult struct {
	thumbnailKey     string
	extractedTextKey string
	extractedText    string
}

func (w *MessageAttachmentWorker) processAttachmentData(ctx context.Context, item state.MessageAttachment, data []byte) (processedAttachmentResult, error) {
	mimeType := strings.ToLower(strings.TrimSpace(strings.Split(item.MimeType, ";")[0]))
	switch {
	case strings.HasPrefix(mimeType, "image/") || item.FileType == messageAttachmentFileTypeImage:
		return w.processImageAttachment(ctx, item, data)
	case mimeType == "application/pdf" || strings.EqualFold(filepath.Ext(item.FileName), ".pdf"):
		return w.processPDFAttachment(ctx, item, data)
	case strings.HasPrefix(mimeType, "text/"):
		return w.processTextAttachment(ctx, item, data)
	default:
		return processedAttachmentResult{}, nil
	}
}

func (w *MessageAttachmentWorker) processImageAttachment(ctx context.Context, item state.MessageAttachment, data []byte) (processedAttachmentResult, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return processedAttachmentResult{}, err
	}
	cleaned, contentType, err := encodeCleanImage(img, format)
	if err == nil && len(cleaned) > 0 && strings.TrimSpace(item.StorageKey) != "" {
		if err := w.artifacts.Objects.Put(ctx, item.StorageKey, cleaned, contentType); err != nil {
			return processedAttachmentResult{}, err
		}
	}
	thumb := resizeImage(img, w.config.ThumbnailMaxDimension)
	var thumbBuf bytes.Buffer
	if err := jpeg.Encode(&thumbBuf, thumb, &jpeg.Options{Quality: 82}); err != nil {
		return processedAttachmentResult{}, err
	}
	key := derivedAttachmentObjectKey(item, "thumb.jpg")
	if err := w.artifacts.Objects.Put(ctx, key, thumbBuf.Bytes(), "image/jpeg"); err != nil {
		return processedAttachmentResult{}, err
	}
	return processedAttachmentResult{thumbnailKey: key}, nil
}

func (w *MessageAttachmentWorker) processPDFAttachment(ctx context.Context, item state.MessageAttachment, data []byte) (processedAttachmentResult, error) {
	text := extractPDFText(data)
	if strings.TrimSpace(text) == "" {
		return processedAttachmentResult{}, nil
	}
	key := derivedAttachmentObjectKey(item, "extracted.txt")
	if err := w.artifacts.Objects.Put(ctx, key, []byte(text), "text/plain; charset=utf-8"); err != nil {
		return processedAttachmentResult{}, err
	}
	return processedAttachmentResult{extractedTextKey: key, extractedText: text}, nil
}

func (w *MessageAttachmentWorker) processTextAttachment(ctx context.Context, item state.MessageAttachment, data []byte) (processedAttachmentResult, error) {
	text := cleanTextForExtraction(data)
	if strings.TrimSpace(text) == "" {
		return processedAttachmentResult{}, nil
	}
	key := derivedAttachmentObjectKey(item, "extracted.txt")
	if err := w.artifacts.Objects.Put(ctx, key, []byte(text), "text/plain; charset=utf-8"); err != nil {
		return processedAttachmentResult{}, err
	}
	return processedAttachmentResult{extractedTextKey: key, extractedText: text}, nil
}

func encodeCleanImage(img image.Image, format string) ([]byte, string, error) {
	var buf bytes.Buffer
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/jpeg", nil
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/png", nil
	default:
		return nil, "", fmt.Errorf("unsupported clean image format %q", format)
	}
}

func resizeImage(src image.Image, maxDimension int) image.Image {
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return src
	}
	if maxDimension <= 0 || (width <= maxDimension && height <= maxDimension) {
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
		return dst
	}
	scale := math.Min(float64(maxDimension)/float64(width), float64(maxDimension)/float64(height))
	dstWidth := max(1, int(math.Round(float64(width)*scale)))
	dstHeight := max(1, int(math.Round(float64(height)*scale)))
	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	for y := 0; y < dstHeight; y++ {
		srcY := bounds.Min.Y + min(height-1, int(float64(y)/scale))
		for x := 0; x < dstWidth; x++ {
			srcX := bounds.Min.X + min(width-1, int(float64(x)/scale))
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func derivedAttachmentObjectKey(item state.MessageAttachment, suffix string) string {
	id := safeBaseName(item.ID)
	if id == "" {
		id = "attachment"
	}
	base := strings.TrimSpace(item.StorageKey)
	if base == "" {
		return joinObjectKey("attachments", id, suffix)
	}
	return joinObjectKey(filepath.ToSlash(filepath.Dir(base)), "_processed", id+"-"+suffix)
}

func cleanTextForExtraction(data []byte) string {
	text := strings.TrimSpace(string(bytes.ToValidUTF8(data, []byte(" "))))
	if len(text) > 1<<20 {
		text = text[:1<<20]
	}
	return text
}

func extractPDFText(data []byte) string {
	var parts []string
	parts = append(parts, extractPDFContentText(data)...)
	for _, stream := range pdfStreams(data) {
		parts = append(parts, extractPDFContentText(stream)...)
	}
	return strings.TrimSpace(strings.Join(compactExtractedText(parts), "\n"))
}

func pdfStreams(data []byte) [][]byte {
	var streams [][]byte
	searchFrom := 0
	for {
		streamIndex := bytes.Index(data[searchFrom:], []byte("stream"))
		if streamIndex < 0 {
			break
		}
		streamIndex += searchFrom
		contentStart := streamIndex + len("stream")
		if contentStart < len(data) && data[contentStart] == '\r' {
			contentStart++
		}
		if contentStart < len(data) && data[contentStart] == '\n' {
			contentStart++
		}
		endIndex := bytes.Index(data[contentStart:], []byte("endstream"))
		if endIndex < 0 {
			break
		}
		contentEnd := contentStart + endIndex
		streamData := bytes.Trim(data[contentStart:contentEnd], "\r\n")
		headerStart := max(0, streamIndex-512)
		header := data[headerStart:streamIndex]
		if bytes.Contains(header, []byte("/FlateDecode")) {
			if inflated, err := inflateZlib(streamData); err == nil {
				streams = append(streams, inflated)
			}
		} else {
			streams = append(streams, streamData)
		}
		searchFrom = contentEnd + len("endstream")
	}
	return streams
}

func inflateZlib(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, 2<<20))
}

var pdfTextArraySpacingPattern = regexp.MustCompile(`\]\s*TJ|Tj|["']`)

func extractPDFContentText(data []byte) []string {
	var out []string
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '(':
			text, next := readPDFLiteralString(data, i+1)
			i = next
			if looksLikeExtractedText(text) {
				out = append(out, text)
			}
		case '<':
			if i+1 < len(data) && data[i+1] == '<' {
				continue
			}
			text, next := readPDFHexString(data, i+1)
			i = next
			if looksLikeExtractedText(text) {
				out = append(out, text)
			}
		}
	}
	return out
}

func readPDFLiteralString(data []byte, start int) (string, int) {
	var out strings.Builder
	depth := 1
	for i := start; i < len(data); i++ {
		ch := data[i]
		if ch == '\\' && i+1 < len(data) {
			i++
			escaped := data[i]
			switch escaped {
			case 'n':
				out.WriteByte('\n')
			case 'r':
				out.WriteByte('\r')
			case 't':
				out.WriteByte('\t')
			case 'b', 'f':
				out.WriteByte(' ')
			case '\\', '(', ')':
				out.WriteByte(escaped)
			case '\r', '\n':
			default:
				if escaped >= '0' && escaped <= '7' {
					octal := []byte{escaped}
					for j := 0; j < 2 && i+1 < len(data) && data[i+1] >= '0' && data[i+1] <= '7'; j++ {
						i++
						octal = append(octal, data[i])
					}
					var value byte
					for _, digit := range octal {
						value = value*8 + (digit - '0')
					}
					out.WriteByte(value)
				} else {
					out.WriteByte(escaped)
				}
			}
			continue
		}
		switch ch {
		case '(':
			depth++
			out.WriteByte(ch)
		case ')':
			depth--
			if depth == 0 {
				return cleanExtractedPDFText(out.String()), i
			}
			out.WriteByte(ch)
		default:
			out.WriteByte(ch)
		}
	}
	return cleanExtractedPDFText(out.String()), len(data)
}

func readPDFHexString(data []byte, start int) (string, int) {
	var hexDigits []byte
	for i := start; i < len(data); i++ {
		if data[i] == '>' {
			if len(hexDigits)%2 == 1 {
				hexDigits = append(hexDigits, '0')
			}
			decoded := make([]byte, hex.DecodedLen(len(hexDigits)))
			if _, err := hex.Decode(decoded, hexDigits); err != nil {
				return "", i
			}
			return cleanExtractedPDFText(string(bytes.Trim(decoded, "\x00"))), i
		}
		if isHexByte(data[i]) {
			hexDigits = append(hexDigits, data[i])
		}
	}
	return "", len(data)
}

func isHexByte(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func cleanExtractedPDFText(text string) string {
	text = strings.ReplaceAll(text, "\x00", "")
	text = strings.TrimSpace(pdfTextArraySpacingPattern.ReplaceAllString(text, " "))
	return strings.Join(strings.Fields(text), " ")
}

func looksLikeExtractedText(text string) bool {
	text = strings.TrimSpace(text)
	if len(text) < 2 || !utf8.ValidString(text) {
		return false
	}
	printable := 0
	letters := 0
	for _, r := range text {
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			printable++
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			letters++
		}
	}
	return letters > 0 && printable*100/len([]rune(text)) >= 80
}

func compactExtractedText(parts []string) []string {
	seen := make(map[string]bool, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}

func errorsIsContextDone(ctx context.Context, err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded || ctx.Err() != nil
}
