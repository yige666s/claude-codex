package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type InMemoryMemoryRecallTraceStore struct {
	mu     sync.RWMutex
	traces []MemoryRecallTrace
}

func NewInMemoryMemoryRecallTraceStore() *InMemoryMemoryRecallTraceStore {
	return &InMemoryMemoryRecallTraceStore{}
}

func (s *InMemoryMemoryRecallTraceStore) RecordMemoryRecallTrace(_ context.Context, trace MemoryRecallTrace) error {
	trace = normalizeMemoryRecallTrace(trace)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traces = append(s.traces, trace)
	return nil
}

func (s *InMemoryMemoryRecallTraceStore) ListMemoryRecallTraces(_ context.Context, userID, sessionID string, limit int) ([]MemoryRecallTrace, error) {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if limit <= 0 {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MemoryRecallTrace, 0, min(limit, len(s.traces)))
	for i := len(s.traces) - 1; i >= 0 && len(out) < limit; i-- {
		trace := s.traces[i]
		if userID != "" && trace.UserID != userID {
			continue
		}
		if sessionID != "" && trace.SessionID != sessionID {
			continue
		}
		out = append(out, cloneMemoryRecallTrace(trace))
	}
	return out, nil
}

func normalizeMemoryRecallTrace(trace MemoryRecallTrace) MemoryRecallTrace {
	trace.ID = strings.TrimSpace(trace.ID)
	if trace.ID == "" {
		trace.ID = uuid.NewString()
	}
	trace.UserID = strings.TrimSpace(trace.UserID)
	trace.SessionID = strings.TrimSpace(trace.SessionID)
	trace.TriggerReason = strings.TrimSpace(trace.TriggerReason)
	if trace.TriggerReason == "" {
		trace.TriggerReason = "unknown"
	}
	trace.Query = strings.TrimSpace(trace.Query)
	trace.OriginalQuery = strings.TrimSpace(trace.OriginalQuery)
	trace.RewrittenQuery = strings.TrimSpace(trace.RewrittenQuery)
	trace.QueryRewriteReason = strings.TrimSpace(trace.QueryRewriteReason)
	trace.QueryHash = strings.TrimSpace(trace.QueryHash)
	if trace.QueryHash == "" && trace.Query != "" {
		trace.QueryHash = memoryRecallQueryHash(trace.Query)
	}
	trace.MemoryItemIDs = compactStringList(trace.MemoryItemIDs)
	trace.EpisodeIDs = compactStringList(trace.EpisodeIDs)
	trace.SourceRefs = compactMemoryRecallSourceRefs(trace.SourceRefs)
	if trace.Metadata == nil {
		trace.Metadata = map[string]any{}
	}
	if trace.CreatedAt.IsZero() {
		trace.CreatedAt = time.Now().UTC()
	}
	return trace
}

func cloneMemoryRecallTrace(trace MemoryRecallTrace) MemoryRecallTrace {
	trace.MemoryItemIDs = append([]string(nil), trace.MemoryItemIDs...)
	trace.EpisodeIDs = append([]string(nil), trace.EpisodeIDs...)
	trace.SourceRefs = append([]MemorySourceRef(nil), trace.SourceRefs...)
	if trace.Metadata != nil {
		metadata := make(map[string]any, len(trace.Metadata))
		for key, value := range trace.Metadata {
			metadata[key] = value
		}
		trace.Metadata = metadata
	}
	return trace
}

func memoryRecallQueryHash(query string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(query)))
	return hex.EncodeToString(sum[:])
}

func memoryRecallItemIDs(items []MemoryItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id := strings.TrimSpace(item.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return compactStringList(ids)
}

func memoryRecallEpisodeIDs(results []MemoryEpisodeSearchResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		if id := strings.TrimSpace(result.Episode.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return compactStringList(ids)
}

func memoryRecallItemSourceRefs(items []MemoryItem) []MemorySourceRef {
	refs := make([]MemorySourceRef, 0, len(items))
	for _, item := range items {
		if id := strings.TrimSpace(item.ID); id != "" {
			refs = append(refs, MemorySourceRef{
				Kind:      "memory_item",
				ID:        id,
				SessionID: strings.TrimSpace(item.SessionID),
			})
		}
	}
	return compactMemoryRecallSourceRefs(refs)
}

func memoryRecallEpisodeSourceRefs(results []MemoryEpisodeSearchResult) []MemorySourceRef {
	refs := make([]MemorySourceRef, 0, len(results))
	for _, result := range results {
		if id := strings.TrimSpace(result.Episode.ID); id != "" {
			refs = append(refs, MemorySourceRef{
				Kind:      "memory_episode",
				ID:        id,
				SessionID: strings.TrimSpace(result.Episode.SessionID),
			})
		}
	}
	return compactMemoryRecallSourceRefs(refs)
}

func compactStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func compactMemoryRecallSourceRefs(refs []MemorySourceRef) []MemorySourceRef {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(refs))
	out := make([]MemorySourceRef, 0, len(refs))
	for _, ref := range refs {
		ref.Kind = strings.TrimSpace(ref.Kind)
		ref.ID = strings.TrimSpace(ref.ID)
		ref.SessionID = strings.TrimSpace(ref.SessionID)
		if ref.Kind == "" || ref.ID == "" {
			continue
		}
		key := ref.Kind + "\x00" + ref.ID + "\x00" + ref.SessionID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ref)
	}
	return out
}
