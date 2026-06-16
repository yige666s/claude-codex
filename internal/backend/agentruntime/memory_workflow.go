package agentruntime

import (
	"context"
	"fmt"
	"time"

	"claude-codex/internal/harness/state"
)

const (
	memoryUpdateWorkflowName    = "memory_update"
	memoryUpdateWorkflowVersion = "v1"
)

type MemoryWorkflowService struct {
	base     MemoryService
	items    MemoryItemService
	episodes MemoryEpisodeService
	workflow *WorkflowEngine
}

func NewMemoryWorkflowService(base MemoryService, store WorkflowStore, events WorkflowEventSink) MemoryService {
	if base == nil {
		return base
	}
	return &MemoryWorkflowService{
		base:     base,
		items:    memoryItemServiceFrom(base),
		episodes: memoryEpisodeServiceFrom(base),
		workflow: NewWorkflowEngine(store, events),
	}
}

func memoryUpdateWorkflowDefinition() WorkflowDefinition {
	return WorkflowDefinition{
		Name:    memoryUpdateWorkflowName,
		Version: memoryUpdateWorkflowVersion,
		Steps: []WorkflowStepDefinition{
			{Name: "extract_candidates"},
			{Name: "load_existing"},
			{Name: "apply_memory_update"},
			{Name: "index_vectors"},
		},
	}
}

func (s *MemoryWorkflowService) LoadContext(ctx context.Context, userID string, session *state.Session) (string, error) {
	return s.base.LoadContext(ctx, userID, session)
}

func (s *MemoryWorkflowService) LoadUserMemory(ctx context.Context, userID string) (string, error) {
	return s.base.LoadUserMemory(ctx, userID)
}

func (s *MemoryWorkflowService) LoadSessionMemory(ctx context.Context, userID, sessionID string) (string, error) {
	return s.base.LoadSessionMemory(ctx, userID, sessionID)
}

func (s *MemoryWorkflowService) AfterTurn(ctx context.Context, userID string, session *state.Session) error {
	if s == nil || s.base == nil || session == nil {
		return nil
	}
	if s.workflow == nil {
		return s.base.AfterTurn(ctx, userID, session)
	}
	var updateErr error
	before := map[string]MemoryItem{}
	after := map[string]MemoryItem{}
	engine := NewWorkflowEngine(s.workflow.Store(), ContextWorkflowEventSink{})
	engine.RegisterStepHandler("extract_candidates", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		candidates := extractMemoryItems(userID, session)
		return map[string]any{
			"candidate_count": len(candidates),
			"has_candidates":  len(candidates) > 0,
		}, nil
	})
	engine.RegisterStepHandler("load_existing", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		if s.items == nil {
			return map[string]any{"existing_count": 0, "item_store": false}, nil
		}
		items, err := s.items.ListMemoryItems(ctx, userID, MemoryItemFilter{})
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			before[item.ID] = item
		}
		return map[string]any{"existing_count": len(items), "item_store": true}, nil
	})
	engine.RegisterStepHandler("apply_memory_update", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		updateErr = s.base.AfterTurn(ctx, userID, session)
		if updateErr != nil {
			return nil, updateErr
		}
		return map[string]any{"updated": true}, nil
	})
	engine.RegisterStepHandler("index_vectors", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		if s.items == nil {
			return map[string]any{"after_count": 0, "changed_count": 0, "vector_index_step": "delegated"}, nil
		}
		items, err := s.items.ListMemoryItems(ctx, userID, MemoryItemFilter{})
		if err != nil {
			return nil, err
		}
		changed := 0
		for _, item := range items {
			after[item.ID] = item
			old, ok := before[item.ID]
			if !ok || !old.UpdatedAt.Equal(item.UpdatedAt) || old.RawHash != item.RawHash {
				changed++
			}
		}
		return map[string]any{
			"after_count":       len(after),
			"changed_count":     changed,
			"vector_index_step": "delegated",
		}, nil
	})
	_, err := engine.Execute(ctx, WorkflowRequest{
		Definition: memoryUpdateWorkflowDefinition(),
		UserID:     userID,
		SessionID:  session.ID,
		JobID:      jobIDFromContext(ctx),
		State: map[string]any{
			"user_id":    userID,
			"session_id": session.ID,
		},
	})
	if err != nil {
		return err
	}
	return updateErr
}

func (s *MemoryWorkflowService) DeleteSession(ctx context.Context, userID, sessionID string) error {
	return s.base.DeleteSession(ctx, userID, sessionID)
}

func (s *MemoryWorkflowService) DeleteUser(ctx context.Context, userID string) error {
	return s.base.DeleteUser(ctx, userID)
}

func (s *MemoryWorkflowService) PruneBefore(ctx context.Context, cutoff time.Time) (int, error) {
	return s.base.PruneBefore(ctx, cutoff)
}

func (s *MemoryWorkflowService) DeleteSavedMemory(ctx context.Context, userID string) error {
	if service, ok := s.base.(SavedMemoryDeletionService); ok {
		return service.DeleteSavedMemory(ctx, userID)
	}
	return s.base.DeleteUser(ctx, userID)
}

func (s *MemoryWorkflowService) GetMemoryItem(ctx context.Context, userID, itemID string) (MemoryItem, error) {
	if s.items == nil {
		return MemoryItem{}, fmt.Errorf("memory item service is not supported")
	}
	return s.items.GetMemoryItem(ctx, userID, itemID)
}

func (s *MemoryWorkflowService) ListMemoryItems(ctx context.Context, userID string, filter MemoryItemFilter) ([]MemoryItem, error) {
	if s.items == nil {
		return []MemoryItem{}, fmt.Errorf("memory item service is not supported")
	}
	return s.items.ListMemoryItems(ctx, userID, filter)
}

func (s *MemoryWorkflowService) UpdateMemoryItem(ctx context.Context, userID string, item MemoryItem) (MemoryItem, error) {
	if s.items == nil {
		return MemoryItem{}, fmt.Errorf("memory item service is not supported")
	}
	return s.items.UpdateMemoryItem(ctx, userID, item)
}

func (s *MemoryWorkflowService) DeleteMemoryItem(ctx context.Context, userID, itemID string) error {
	if s.items == nil {
		return fmt.Errorf("memory item service is not supported")
	}
	return s.items.DeleteMemoryItem(ctx, userID, itemID)
}

func (s *MemoryWorkflowService) UpsertMemoryEpisode(ctx context.Context, userID string, episode MemoryEpisode) (MemoryEpisode, error) {
	if s.episodes == nil {
		return MemoryEpisode{}, fmt.Errorf("memory episode service is not supported")
	}
	return s.episodes.UpsertMemoryEpisode(ctx, userID, episode)
}

func (s *MemoryWorkflowService) GetMemoryEpisode(ctx context.Context, userID, episodeID string) (MemoryEpisode, error) {
	if s.episodes == nil {
		return MemoryEpisode{}, fmt.Errorf("memory episode service is not supported")
	}
	return s.episodes.GetMemoryEpisode(ctx, userID, episodeID)
}

func (s *MemoryWorkflowService) ListMemoryEpisodes(ctx context.Context, userID string, filter MemoryEpisodeFilter) ([]MemoryEpisode, error) {
	if s.episodes == nil {
		return []MemoryEpisode{}, fmt.Errorf("memory episode service is not supported")
	}
	return s.episodes.ListMemoryEpisodes(ctx, userID, filter)
}

func (s *MemoryWorkflowService) UpdateMemoryEpisode(ctx context.Context, userID string, episode MemoryEpisode) (MemoryEpisode, error) {
	if s.episodes == nil {
		return MemoryEpisode{}, fmt.Errorf("memory episode service is not supported")
	}
	return s.episodes.UpdateMemoryEpisode(ctx, userID, episode)
}

func (s *MemoryWorkflowService) DeleteMemoryEpisode(ctx context.Context, userID, episodeID string) error {
	if s.episodes == nil {
		return fmt.Errorf("memory episode service is not supported")
	}
	return s.episodes.DeleteMemoryEpisode(ctx, userID, episodeID)
}

func (s *MemoryWorkflowService) SearchMemoryEpisodes(ctx context.Context, userID, query string, opts MemoryEpisodeSearchOptions) ([]MemoryEpisodeSearchResult, error) {
	if s.episodes == nil {
		return []MemoryEpisodeSearchResult{}, fmt.Errorf("memory episode service is not supported")
	}
	return s.episodes.SearchMemoryEpisodes(ctx, userID, query, opts)
}

func (s *MemoryWorkflowService) RecordMemoryEpisodeRecall(ctx context.Context, userID, episodeID string, score float64) error {
	if s.episodes == nil {
		return fmt.Errorf("memory episode service is not supported")
	}
	return s.episodes.RecordMemoryEpisodeRecall(ctx, userID, episodeID, score)
}

func (s *MemoryWorkflowService) RecordMemoryEpisodeUse(ctx context.Context, userID, episodeID string) error {
	if s.episodes == nil {
		return fmt.Errorf("memory episode service is not supported")
	}
	return s.episodes.RecordMemoryEpisodeUse(ctx, userID, episodeID)
}

func (s *MemoryWorkflowService) ListUnpromotedMemoryEpisodes(ctx context.Context, userID string, limit int) ([]MemoryEpisode, error) {
	if s.episodes == nil {
		return []MemoryEpisode{}, fmt.Errorf("memory episode service is not supported")
	}
	return s.episodes.ListUnpromotedMemoryEpisodes(ctx, userID, limit)
}

func (s *MemoryWorkflowService) MarkMemoryEpisodesPromoted(ctx context.Context, userID string, episodeIDs []string) error {
	if s.episodes == nil {
		return fmt.Errorf("memory episode service is not supported")
	}
	return s.episodes.MarkMemoryEpisodesPromoted(ctx, userID, episodeIDs)
}

func (s *MemoryWorkflowService) DeleteMemoryEpisodesForSession(ctx context.Context, userID, sessionID string) error {
	if s.episodes == nil {
		return nil
	}
	return s.episodes.DeleteMemoryEpisodesForSession(ctx, userID, sessionID)
}

func (s *MemoryWorkflowService) GetMemorySettings(ctx context.Context, userID string) (MemorySettings, error) {
	if service, ok := s.base.(MemorySettingsService); ok {
		return service.GetMemorySettings(ctx, userID)
	}
	return defaultMemorySettings(), nil
}

func (s *MemoryWorkflowService) UpdateMemorySettings(ctx context.Context, userID string, settings MemorySettings) (MemorySettings, error) {
	if service, ok := s.base.(MemorySettingsService); ok {
		return service.UpdateMemorySettings(ctx, userID, settings)
	}
	return MemorySettings{}, fmt.Errorf("memory settings are not supported")
}

func (s *MemoryWorkflowService) GetPersonalizationSettings(ctx context.Context, userID string) (PersonalizationSettings, error) {
	if service, ok := s.base.(PersonalizationSettingsService); ok {
		return service.GetPersonalizationSettings(ctx, userID)
	}
	return defaultPersonalizationSettings(), nil
}

func (s *MemoryWorkflowService) UpdatePersonalizationSettings(ctx context.Context, userID string, settings PersonalizationSettings) (PersonalizationSettings, error) {
	if service, ok := s.base.(PersonalizationSettingsService); ok {
		return service.UpdatePersonalizationSettings(ctx, userID, settings)
	}
	return PersonalizationSettings{}, fmt.Errorf("personalization settings are not supported")
}

func (s *MemoryWorkflowService) DeletePersonalizationSettings(ctx context.Context, userID string) error {
	if service, ok := s.base.(PersonalizationSettingsService); ok {
		return service.DeletePersonalizationSettings(ctx, userID)
	}
	return nil
}

func memoryItemServiceFrom(service MemoryService) MemoryItemService {
	items, _ := service.(MemoryItemService)
	return items
}

func memoryEpisodeServiceFrom(service MemoryService) MemoryEpisodeService {
	switch s := service.(type) {
	case *MemoryWorkflowService:
		if s.episodes == nil {
			return nil
		}
		return s
	case *MemoryVectorService:
		if s.episodes == nil {
			return nil
		}
		return s
	}
	episodes, _ := service.(MemoryEpisodeService)
	return episodes
}
