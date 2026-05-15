package agentruntime

import (
	"mime"
	"path/filepath"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
	publictypes "claude-codex/internal/public/types"
)

const (
	messageAttachmentFileTypeText     = "text"
	messageAttachmentFileTypeImage    = "image"
	messageAttachmentFileTypeDocument = "document"
	messageAttachmentFileTypeAudio    = "audio"
	messageAttachmentFileTypeVideo    = "video"
	messageAttachmentFileTypeFile     = "file"
)

func normalizeMessageContentParts(blocks []publictypes.ContentBlock) []publictypes.ContentBlock {
	if len(blocks) == 0 {
		return blocks
	}
	out := make([]publictypes.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		block.Type = normalizeContentBlockType(block.Type, sourceString(block.Source, "media_type", "mime_type", "mimeType", "content_type"), sourceString(block.Source, "filename", "file_name", "name"))
		if id := sourceString(block.Source, "attachment_id", "id"); id != "" {
			source := map[string]interface{}{
				"type":          "attachment_ref",
				"attachment_id": id,
			}
			if mediaType := sourceString(block.Source, "media_type", "mime_type", "mimeType", "content_type"); mediaType != "" {
				source["media_type"] = normalizedContentType(mediaType)
			}
			if filename := sourceString(block.Source, "filename", "file_name", "name"); filename != "" {
				source["filename"] = safeBaseName(filename)
			}
			if storageKey := sourceString(block.Source, "storage_key", "object_key"); storageKey != "" {
				source["storage_key"] = storageKey
			}
			if thumbnailKey := sourceString(block.Source, "thumbnail_key"); thumbnailKey != "" {
				source["thumbnail_key"] = thumbnailKey
			}
			block.Source = source
		}
		out = append(out, block)
	}
	return out
}

func messageAttachmentRefs(message state.Message) []state.MessageAttachment {
	blocks := message.ContentParts
	if len(blocks) == 0 && len(message.ContentBlocks) > 0 {
		blocks = message.ContentBlocks
	}
	if len(blocks) == 0 {
		return nil
	}
	refs := make([]state.MessageAttachment, 0)
	seen := make(map[string]bool)
	for _, block := range blocks {
		id := sourceString(block.Source, "attachment_id", "id")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		mimeType := normalizedContentType(sourceString(block.Source, "media_type", "mime_type", "mimeType", "content_type"))
		filename := safeBaseName(sourceString(block.Source, "filename", "file_name", "name"))
		ref := state.MessageAttachment{
			ID:              id,
			MessageID:       message.ID,
			SessionID:       message.SessionID,
			UserID:          message.UserID,
			FileType:        messageAttachmentFileType(block.Type, mimeType, filename),
			MimeType:        mimeType,
			FileName:        filename,
			StorageKey:      sourceString(block.Source, "storage_key", "object_key"),
			ThumbnailKey:    sourceString(block.Source, "thumbnail_key"),
			EmbeddingStatus: state.MessageAttachmentEmbeddingPending,
			CreatedAt:       message.CreatedAt,
		}
		refs = append(refs, ref)
	}
	return refs
}

func safeBaseName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.Base(value)
}

func normalizeContentBlockType(blockType, mimeType, filename string) string {
	blockType = strings.ToLower(strings.TrimSpace(blockType))
	switch blockType {
	case "text", "image", "file", "document", "audio", "video":
		if blockType == "document" {
			return "file"
		}
		return blockType
	}
	resolvedMime := strings.ToLower(strings.TrimSpace(firstNonEmptyString(mimeType, mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))))))
	switch {
	case strings.HasPrefix(resolvedMime, "text/"):
		return "text"
	case strings.HasPrefix(resolvedMime, "image/"):
		return "image"
	case strings.HasPrefix(resolvedMime, "audio/"):
		return "audio"
	case strings.HasPrefix(resolvedMime, "video/"):
		return "video"
	default:
		return "file"
	}
}

func messageAttachmentFileType(blockType, mimeType, filename string) string {
	blockType = strings.ToLower(strings.TrimSpace(blockType))
	switch blockType {
	case messageAttachmentFileTypeText, messageAttachmentFileTypeImage, messageAttachmentFileTypeAudio, messageAttachmentFileTypeVideo:
		return blockType
	case "document":
		return messageAttachmentFileTypeDocument
	}
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch {
	case strings.HasPrefix(mimeType, "text/"):
		return messageAttachmentFileTypeText
	case strings.HasPrefix(mimeType, "image/"):
		return messageAttachmentFileTypeImage
	case strings.HasPrefix(mimeType, "audio/"):
		return messageAttachmentFileTypeAudio
	case strings.HasPrefix(mimeType, "video/"):
		return messageAttachmentFileTypeVideo
	case isDocumentAttachment(filename, mimeType):
		return messageAttachmentFileTypeDocument
	default:
		return messageAttachmentFileTypeFile
	}
}

func isDocumentAttachment(filename, mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	if strings.Contains(mimeType, "pdf") || strings.Contains(mimeType, "document") || strings.Contains(mimeType, "spreadsheet") || strings.Contains(mimeType, "presentation") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".csv", ".md", ".json", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func mergeMessageAttachmentMetadata(ref state.MessageAttachment, artifact *Artifact) state.MessageAttachment {
	if artifact != nil {
		ref.ID = firstNonEmptyString(ref.ID, artifact.ID)
		ref.UserID = firstNonEmptyString(ref.UserID, artifact.UserID)
		ref.SessionID = firstNonEmptyString(ref.SessionID, artifact.SessionID)
		ref.FileName = firstNonEmptyString(ref.FileName, artifact.Filename)
		ref.MimeType = normalizedContentType(firstNonEmptyString(ref.MimeType, artifact.ContentType))
		ref.FileSize = artifact.SizeBytes
		ref.StorageKey = firstNonEmptyString(ref.StorageKey, artifact.ObjectKey)
		if ref.CreatedAt.IsZero() {
			ref.CreatedAt = artifact.CreatedAt
		}
	}
	if ref.FileType == "" {
		ref.FileType = messageAttachmentFileType("", ref.MimeType, ref.FileName)
	}
	if ref.MimeType == "" {
		ref.MimeType = normalizedContentType(mime.TypeByExtension(strings.ToLower(filepath.Ext(ref.FileName))))
	}
	if ref.CreatedAt.IsZero() {
		ref.CreatedAt = time.Now().UTC()
	}
	return ref
}
