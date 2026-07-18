package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
)

type RuleMemoryEpisodePromoter struct {
	Extractor MemoryExtractor
	Items     MemoryItemService
	Policy    MemoryPolicy
	Now       func() time.Time
}

func (p RuleMemoryEpisodePromoter) PromoteEpisodes(ctx context.Context, userID string, episodes []MemoryEpisode) ([]MemoryItem, error) {
	if p.Items == nil {
		return nil, fmt.Errorf("memory item service is not configured")
	}
	extractor := p.Extractor
	if extractor == nil {
		extractor = NewRuleMemoryExtractorWithPolicy(p.policy())
	}
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	existing, err := p.Items.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return nil, err
	}
	promoted := []MemoryItem{}
	for _, episode := range episodes {
		episode = normalizeMemoryEpisode(episode)
		if episode.ID == "" || episode.Status != MemoryEpisodeStatusActive {
			continue
		}
		candidates, err := extractor.Extract(ctx, MemoryExtractionInput{
			UserID:    userID,
			SessionID: episode.SessionID,
			Messages:  episodePromotionMessages(episode),
			Now:       now,
		})
		if err != nil {
			return promoted, err
		}
		for i := range candidates {
			candidates[i] = decorateEpisodePromotionCandidate(candidates[i], episode)
		}
		items := evaluateMemoryCandidatesWithPolicy(userID, episode.SessionID, candidates, p.policy())
		for _, item := range items {
			item = normalizeMemoryItem(item)
			if item.Metadata == nil {
				item.Metadata = map[string]any{}
			}
			item.Metadata["promoted_from_episode"] = true
			item.Metadata["episode_id"] = episode.ID
			item.Metadata["episode_title"] = episode.Title
			item.Metadata["promotion_source"] = "episodic_memory"
			item.Source = MemorySourceSystem
			item.SourceRefs = normalizeMemorySourceRefs(append(item.SourceRefs, MemorySourceRef{
				Kind:      "episode",
				ID:        episode.ID,
				SessionID: episode.SessionID,
			}))
			item.Tags = normalizeMemoryTags(append(item.Tags, "episode", "promoted"))
			var conflictUpdates []MemoryItem
			item, existing, conflictUpdates = resolveEpisodePromotionConflictsWithPolicy(existing, item, p.policy())
			for _, update := range conflictUpdates {
				if _, err := p.Items.UpdateMemoryItem(ctx, userID, update); err != nil {
					return promoted, err
				}
			}
			item = upsertMemoryItem(existing, item)
			updated, err := p.Items.UpdateMemoryItem(ctx, userID, item)
			if err != nil {
				return promoted, err
			}
			existing = append(existing, updated)
			promoted = append(promoted, updated)
		}
	}
	return promoted, nil
}

func (p RuleMemoryEpisodePromoter) policy() MemoryPolicy {
	return normalizeMemoryPolicy(p.Policy)
}

func episodePromotionMessages(episode MemoryEpisode) []state.Message {
	var content strings.Builder
	content.WriteString("Episode summary for durable memory promotion.\n")
	if episode.Title != "" {
		content.WriteString("Title: ")
		content.WriteString(episode.Title)
		content.WriteString("\n")
	}
	if episode.L0Abstract != "" {
		content.WriteString("Abstract: ")
		content.WriteString(episode.L0Abstract)
		content.WriteString("\n")
	}
	content.WriteString("Summary: ")
	content.WriteString(episode.Summary)
	if len(episode.KeyTopics) > 0 {
		content.WriteString("\nTopics: ")
		content.WriteString(strings.Join(episode.KeyTopics, ", "))
	}
	return []state.Message{{
		ID:      "episode:" + episode.ID + ":promotion",
		Role:    state.MessageRoleUser,
		Content: content.String(),
	}}
}

func decorateEpisodePromotionCandidate(candidate MemoryCandidate, episode MemoryEpisode) MemoryCandidate {
	candidate.Source = MemorySourceSystem
	candidate.SourceRefs = normalizeMemorySourceRefs(append(candidate.SourceRefs, MemorySourceRef{
		Kind:      "episode",
		ID:        episode.ID,
		SessionID: episode.SessionID,
	}))
	candidate.Tags = normalizeMemoryTags(append(candidate.Tags, "episode", "promoted"))
	if candidate.Metadata == nil {
		candidate.Metadata = map[string]any{}
	}
	candidate.Metadata["promoted_from_episode"] = true
	candidate.Metadata["episode_id"] = episode.ID
	candidate.Metadata["episode_title"] = episode.Title
	return candidate
}

func resolveEpisodePromotionConflicts(existing []MemoryItem, item MemoryItem) (MemoryItem, []MemoryItem, []MemoryItem) {
	return resolveEpisodePromotionConflictsWithPolicy(existing, item, DefaultMemoryPolicy())
}

func resolveEpisodePromotionConflictsWithPolicy(existing []MemoryItem, item MemoryItem, policy MemoryPolicy) (MemoryItem, []MemoryItem, []MemoryItem) {
	explicitConflicts := explicitPersonalizationConflictsWithPolicy(existing, item, policy)
	if len(explicitConflicts) > 0 {
		item.Status = MemoryStatusPendingConfirm
		item.ConflictIDs = normalizeMemoryIDs(append(item.ConflictIDs, explicitConflicts...))
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["conflict_strategy"] = "explicit_personalization"
		item.Metadata["maintenance_action"] = "episode_promotion_pending_personalization_conflict"
		item.Metadata["memory_policy_version"] = memoryPolicyVersion(policy)
		return item, existing, nil
	}
	candidate, updates := applyMemoryConflictResolutionWithPolicy(existing, item, policy)
	for _, update := range updates {
		replaced := false
		for i := range existing {
			if existing[i].ID == update.ID {
				existing[i] = update
				replaced = true
				break
			}
		}
		if !replaced {
			existing = append(existing, update)
		}
	}
	return candidate, existing, updates
}

func explicitPersonalizationConflicts(existing []MemoryItem, candidate MemoryItem) []string {
	return explicitPersonalizationConflictsWithPolicy(existing, candidate, DefaultMemoryPolicy())
}

func explicitPersonalizationConflictsWithPolicy(existing []MemoryItem, candidate MemoryItem, policy MemoryPolicy) []string {
	var conflicts []string
	for _, current := range existing {
		current = normalizeMemoryItem(current)
		if current.Status != MemoryStatusActive {
			continue
		}
		if current.Namespace != MemoryNamespacePersonalization || current.Source != MemorySourceUserEdit {
			continue
		}
		if conflictsWithCandidate, _ := memoryConflictCandidateWithPolicy(current, candidate, policy); conflictsWithCandidate {
			conflicts = append(conflicts, current.ID)
		}
	}
	return normalizeMemoryIDs(conflicts)
}
