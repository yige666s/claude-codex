package agentruntime

import (
	"testing"
	"time"

	"claude-codex/internal/harness/state"
	publictypes "claude-codex/internal/public/types"
)

func TestMessageAttachmentRefsNormalizeMultimodalBlocks(t *testing.T) {
	now := time.Now().UTC()
	message := state.Message{
		ID:        "msg-1",
		UserID:    "alice",
		SessionID: "session-1",
		Role:      state.MessageRoleUser,
		ContentParts: []publictypes.ContentBlock{
			{Type: "text", Text: "describe this"},
			{Type: "document", Source: map[string]interface{}{
				"type":          "base64",
				"attachment_id": "att-1",
				"media_type":    "application/pdf",
				"filename":      "/tmp/report.pdf",
				"data":          "must-not-persist",
			}},
			{Source: map[string]interface{}{
				"attachment_id": "att-2",
				"media_type":    "audio/mpeg",
				"filename":      "voice.mp3",
			}},
		},
		CreatedAt: now,
	}

	parts := normalizeMessageContentParts(message.ContentParts)
	if parts[1].Type != "file" || parts[1].Source["type"] != "attachment_ref" || parts[1].Source["data"] != nil {
		t.Fatalf("expected sanitized document attachment ref, got %#v", parts[1])
	}
	if parts[1].Source["filename"] != "report.pdf" {
		t.Fatalf("expected basename filename, got %#v", parts[1].Source)
	}
	if parts[2].Type != "audio" {
		t.Fatalf("expected audio block type, got %#v", parts[2])
	}

	message.ContentParts = parts
	refs := messageAttachmentRefs(message)
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %#v", refs)
	}
	if refs[0].ID != "att-1" || refs[0].FileType != "document" || refs[0].MimeType != "application/pdf" || refs[0].FileName != "report.pdf" {
		t.Fatalf("unexpected document ref: %#v", refs[0])
	}
	if refs[1].ID != "att-2" || refs[1].FileType != "audio" || refs[1].MimeType != "audio/mpeg" || refs[1].FileName != "voice.mp3" {
		t.Fatalf("unexpected audio ref: %#v", refs[1])
	}
}

func TestMergeMessageAttachmentMetadataPrefersArtifactStorage(t *testing.T) {
	created := time.Now().UTC()
	ref := state.MessageAttachment{
		ID:       "att-1",
		FileType: "file",
		MimeType: "application/octet-stream",
	}
	artifact := &Artifact{
		ID:          "att-1",
		UserID:      "alice",
		SessionID:   "session-1",
		ObjectKey:   "objects/att-1.pdf",
		Filename:    "report.pdf",
		ContentType: "application/pdf",
		SizeBytes:   123,
		CreatedAt:   created,
	}

	got := mergeMessageAttachmentMetadata(ref, artifact)
	if got.StorageKey != artifact.ObjectKey || got.FileName != artifact.Filename || got.FileSize != artifact.SizeBytes {
		t.Fatalf("artifact metadata not merged: %#v", got)
	}
	if got.MimeType != "application/octet-stream" {
		t.Fatalf("explicit ref mime type should be preserved, got %q", got.MimeType)
	}
	if got.CreatedAt.IsZero() {
		t.Fatalf("expected created_at")
	}
}
