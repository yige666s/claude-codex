package agentruntime

import (
	"context"
	"strconv"
	"strings"

	"claude-codex/internal/harness/state"
)

const (
	messageIndexSourceMessage     = "message"
	messageIndexSourceAttachment  = "attachment"
	defaultAttachmentChunkSize    = 4000
	defaultAttachmentChunkOverlap = 200
)

type MessageAttachmentContentIndexer interface {
	IndexAttachmentText(ctx context.Context, attachment state.MessageAttachment, text string) error
}

type MessageAttachmentContentDeleter interface {
	DeleteAttachmentText(ctx context.Context, attachment state.MessageAttachment) error
}

type CompositeMessageAttachmentContentIndexer []MessageAttachmentContentIndexer

func (i CompositeMessageAttachmentContentIndexer) IndexAttachmentText(ctx context.Context, attachment state.MessageAttachment, text string) error {
	for _, indexer := range i {
		if indexer == nil {
			continue
		}
		if err := indexer.IndexAttachmentText(ctx, attachment, text); err != nil {
			return err
		}
	}
	return nil
}

func attachmentIndexable(attachment state.MessageAttachment, text string) bool {
	return strings.TrimSpace(attachment.ID) != "" &&
		strings.TrimSpace(attachment.MessageID) != "" &&
		strings.TrimSpace(attachment.SessionID) != "" &&
		strings.TrimSpace(attachment.UserID) != "" &&
		strings.TrimSpace(text) != ""
}

func attachmentIndexText(attachment state.MessageAttachment, text string) string {
	text = strings.TrimSpace(text)
	name := strings.TrimSpace(attachment.FileName)
	if name == "" {
		return text
	}
	return name + "\n\n" + text
}

func attachmentTextChunks(text string, maxRunes, overlapRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = defaultAttachmentChunkSize
	}
	if overlapRunes < 0 || overlapRunes >= maxRunes {
		overlapRunes = 0
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return []string{text}
	}
	chunks := make([]string, 0, len(runes)/maxRunes+1)
	for start := 0; start < len(runes); {
		end := start + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end == len(runes) {
			break
		}
		start = end - overlapRunes
	}
	return chunks
}

func messageAttachmentDocumentID(attachment state.MessageAttachment, chunkIndex int) string {
	return strings.Join([]string{
		messageIndexSourceAttachment,
		strings.TrimSpace(attachment.UserID),
		strings.TrimSpace(attachment.MessageID),
		strings.TrimSpace(attachment.ID),
		strconv.Itoa(chunkIndex),
	}, ":")
}
