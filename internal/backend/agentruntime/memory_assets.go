package agentruntime

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
	publictypes "claude-codex/internal/public/types"
)

const assetMemoryTextLimitBytes = textAttachmentPromptLimitBytes

type MemoryAssetExtractionOptions struct {
	Namespace  string `json:"namespace,omitempty"`
	Visibility string `json:"visibility,omitempty"`
}

func (r *Runtime) ExtractMemoryFromAsset(ctx context.Context, userID, kind, assetID string, options MemoryAssetExtractionOptions) ([]MemoryItem, error) {
	service, err := r.memoryItemService()
	if err != nil {
		return nil, err
	}
	settings, err := r.GetMemorySettings(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !settings.CaptureEnabled {
		return nil, fmt.Errorf("memory capture is disabled")
	}
	asset, data, err := r.getAsset(ctx, normalizeAssetKind(kind), userID, assetID)
	if err != nil {
		return nil, err
	}
	if isImageContentType(asset.ContentType) && r.assetInsights != nil {
		if _, err := r.processAssetInsight(ctx, asset, data); err != nil {
			return nil, err
		}
		return []MemoryItem{}, nil
	}
	namespace := normalizeMemoryNamespace(options.Namespace)
	visibility := normalizeMemoryVisibility(options.Visibility)
	candidates, err := r.extractAssetMemoryCandidates(ctx, userID, asset, data)
	if err != nil {
		return nil, err
	}
	sourceRef := memorySourceRefForAsset(asset)
	for i := range candidates {
		candidates[i].Namespace = namespace
		candidates[i].Visibility = visibility
		if candidates[i].Source == "" {
			candidates[i].Source = memorySourceForAsset(asset)
		}
		candidates[i].SourceRefs = append(candidates[i].SourceRefs, sourceRef)
		if candidates[i].Metadata == nil {
			candidates[i].Metadata = map[string]any{}
		}
		candidates[i].Metadata["source_asset_kind"] = normalizeAssetKind(asset.Kind)
		candidates[i].Metadata["source_asset_id"] = asset.ID
		candidates[i].Metadata["source_asset_filename"] = asset.Filename
		candidates[i].Metadata["source_asset_content_type"] = asset.ContentType
	}
	items := evaluateMemoryCandidates(userID, asset.SessionID, candidates)
	if len(items) == 0 {
		return []MemoryItem{}, nil
	}
	existing, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return nil, err
	}
	saved := make([]MemoryItem, 0, len(items))
	for _, candidate := range items {
		candidate.SourceRefs = normalizeMemorySourceRefs(append(candidate.SourceRefs, sourceRef))
		if candidate.Metadata == nil {
			candidate.Metadata = map[string]any{}
		}
		candidate.Metadata["source_refs"] = candidate.SourceRefs
		var conflictUpdates []MemoryItem
		candidate, conflictUpdates = applyMemoryConflictResolution(existing, candidate)
		for _, update := range conflictUpdates {
			if _, err := service.UpdateMemoryItem(ctx, userID, update); err != nil {
				return nil, err
			}
			existing = append(existing, update)
		}
		item := upsertMemoryItem(existing, candidate)
		item.SourceRefs = normalizeMemorySourceRefs(append(item.SourceRefs, sourceRef))
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["source_refs"] = item.SourceRefs
		updated, err := service.UpdateMemoryItem(ctx, userID, item)
		if err != nil {
			return nil, err
		}
		existing = append(existing, updated)
		saved = append(saved, updated)
	}
	if err := r.markMemoryAbstractionsDirty(ctx, userID, service); err != nil {
		return nil, err
	}
	return saved, nil
}

func (r *Runtime) extractAssetMemoryCandidates(ctx context.Context, userID string, asset *Artifact, data []byte) ([]MemoryCandidate, error) {
	if asset == nil {
		return nil, nil
	}
	if isTextAttachment(asset.Filename, asset.ContentType) {
		if len(data) > assetMemoryTextLimitBytes {
			return nil, fmt.Errorf("text asset %s exceeds memory extraction limit of %d bytes", asset.Filename, assetMemoryTextLimitBytes)
		}
		text := strings.ToValidUTF8(string(data), "\uFFFD")
		if r.memoryExtract != nil {
			candidates, err := r.memoryExtract.Extract(ctx, MemoryExtractionInput{
				UserID:    userID,
				SessionID: asset.SessionID,
				Messages: []state.Message{{
					Role:    "user",
					Content: assetMemoryTextPrompt(asset, text),
				}},
				Now: time.Now().UTC(),
			})
			if err == nil && len(candidates) > 0 {
				return candidates, nil
			}
		}
		return extractMemoryCandidates(text), nil
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(asset.ContentType)), "image/") {
		candidates, err := r.extractImageMemoryCandidates(ctx, userID, asset, data)
		if err == nil && len(candidates) > 0 {
			for i := range candidates {
				if candidates[i].Source == "" {
					candidates[i].Source = MemorySourceVision
				}
				if candidates[i].Metadata == nil {
					candidates[i].Metadata = map[string]any{}
				}
				candidates[i].Metadata["extractor"] = "vision"
			}
			return candidates, nil
		}
	}
	return nil, nil
}

func (r *Runtime) extractImageMemoryCandidates(ctx context.Context, userID string, asset *Artifact, data []byte) ([]MemoryCandidate, error) {
	if r.engineFactory == nil || int64(len(data)) > vertexInlineAttachmentLimitBytes {
		return nil, nil
	}
	contentType := normalizedContentType(firstNonEmptyString(asset.ContentType, mime.TypeByExtension(filepath.Ext(asset.Filename))))
	if contentType == "" || !strings.HasPrefix(contentType, "image/") {
		return nil, nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	runner := r.runnerForScope(Scope{UserID: userID, SessionID: asset.SessionID})
	blocks := []publictypes.ContentBlock{
		{
			Type: "text",
			Text: `Describe this image only for durable user memory extraction.

Return ONLY JSON in this exact shape:
{"memories":[{"content":"...", "category":"fact|preference|event|skill", "tags":["image"], "confidence":0.0, "importance":0.0, "reason":"short reason", "sensitivity":"none|pii|secret|unsafe", "expires_hint":""}]}

Rules:
- Store only durable user-relevant facts, preferences, events, or skills visible from or implied by the image.
- If the image is generic or not user-relevant, return an empty memories array.
- Do not store sensitive details such as addresses, credentials, IDs, or private documents.`,
		},
		{
			Type: attachmentBlockType(contentType),
			Source: map[string]interface{}{
				"type":       "base64",
				"media_type": contentType,
				"data":       base64.StdEncoding.EncodeToString(data),
			},
		},
	}
	result, err := runWithTokenStreamContent(timeoutCtx, runner, state.NewSession(""), blocks, false, nil)
	if err != nil {
		return nil, err
	}
	return parseLLMMemoryCandidates(result.Output)
}

func assetMemoryTextPrompt(asset *Artifact, text string) string {
	return fmt.Sprintf("Extract durable user memory from this %s named %q.\n\n%s", normalizeAssetKind(asset.Kind), asset.Filename, truncateMemoryContent(text))
}

func fallbackAssetMemoryCandidates(asset *Artifact) []MemoryCandidate {
	if asset == nil {
		return nil
	}
	kind := normalizeAssetKind(asset.Kind)
	contentType := strings.TrimSpace(asset.ContentType)
	category := MemoryCategoryEvent
	confidence := 0.62
	importance := 0.45
	tags := []string{kind, "asset"}
	content := fmt.Sprintf("User provided %s file %q", kind, asset.Filename)
	if contentType != "" {
		content += " with content type " + contentType
	}
	if strings.HasPrefix(strings.ToLower(contentType), "image/") {
		tags = append(tags, "image")
		content = fmt.Sprintf("User provided image file %q", asset.Filename)
		confidence = 0.66
		importance = 0.50
	}
	return []MemoryCandidate{{
		Content:    content,
		Category:   category,
		Tags:       tags,
		Source:     memorySourceForAsset(asset),
		Confidence: confidence,
		Importance: importance,
		Reason:     "asset_memory_extraction",
	}}
}

func memorySourceForAsset(asset *Artifact) string {
	if asset == nil {
		return MemorySourceConversation
	}
	switch normalizeAssetKind(asset.Kind) {
	case AssetKindAttachment:
		return MemorySourceAttachment
	default:
		return MemorySourceArtifact
	}
}

func memorySourceRefForAsset(asset *Artifact) MemorySourceRef {
	if asset == nil {
		return MemorySourceRef{}
	}
	return MemorySourceRef{
		Kind:        normalizeAssetKind(asset.Kind),
		ID:          asset.ID,
		Filename:    asset.Filename,
		ContentType: asset.ContentType,
		SessionID:   asset.SessionID,
		JobID:       asset.JobID,
	}
}
