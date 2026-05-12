package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
)

type ObjectSessionStore struct {
	objects ObjectStore
	prefix  string
}

func NewObjectSessionStore(objects ObjectStore, prefix string) *ObjectSessionStore {
	return &ObjectSessionStore{objects: objects, prefix: strings.Trim(prefix, "/")}
}

func (s *ObjectSessionStore) Create(ctx context.Context, userID, workingDir string) (*state.Session, error) {
	session := state.NewSession(workingDir)
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	session.Metadata["user_id_hash"] = userPathID(userID)
	if err := s.Save(ctx, userID, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *ObjectSessionStore) Get(ctx context.Context, userID, sessionID string) (*state.Session, error) {
	data, err := s.objects.Get(ctx, s.sessionKey(userID, sessionID))
	if err != nil {
		return nil, err
	}
	var session state.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *ObjectSessionStore) List(ctx context.Context, userID string) ([]*state.Session, error) {
	keys, err := s.objects.List(ctx, s.sessionsPrefix(userID))
	if err != nil {
		return nil, err
	}
	out := make([]*state.Session, 0, len(keys))
	for _, key := range keys {
		if !strings.HasSuffix(key, ".json") {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(key), ".json")
		session, err := s.Get(ctx, userID, id)
		if err == nil {
			out = append(out, session)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *ObjectSessionStore) Save(ctx context.Context, userID string, session *state.Session) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return s.objects.Put(ctx, s.sessionKey(userID, session.ID), data, "application/json")
}

func (s *ObjectSessionStore) Delete(ctx context.Context, userID, sessionID string) error {
	return s.objects.Delete(ctx, s.sessionKey(userID, sessionID))
}

func (s *ObjectSessionStore) DeleteUser(ctx context.Context, userID string) error {
	keys, err := s.objects.List(ctx, s.sessionsPrefix(userID))
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := s.objects.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (s *ObjectSessionStore) PruneBefore(ctx context.Context, cutoff time.Time) (int, error) {
	keys, err := s.objects.List(ctx, joinObjectKey(s.prefix, "users"))
	if err != nil {
		return 0, err
	}
	pruned := 0
	for _, key := range keys {
		if !strings.HasSuffix(key, ".json") {
			continue
		}
		data, err := s.objects.Get(ctx, key)
		if err != nil {
			continue
		}
		var session state.Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}
		if session.UpdatedAt.Before(cutoff) {
			if err := s.objects.Delete(ctx, key); err != nil {
				return pruned, err
			}
			pruned++
		}
	}
	return pruned, nil
}

func (s *ObjectSessionStore) sessionsPrefix(userID string) string {
	return joinObjectKey(s.prefix, "users", userPathID(userID), "sessions")
}

func (s *ObjectSessionStore) sessionKey(userID, sessionID string) string {
	return joinObjectKey(s.sessionsPrefix(userID), filepath.Base(sessionID)+".json")
}

type ObjectMemoryService struct {
	objects ObjectStore
	prefix  string
}

func NewObjectMemoryService(objects ObjectStore, prefix string) *ObjectMemoryService {
	return &ObjectMemoryService{objects: objects, prefix: strings.Trim(prefix, "/")}
}

func (m *ObjectMemoryService) LoadContext(ctx context.Context, userID string, session *state.Session) (string, error) {
	if session == nil {
		return "", nil
	}
	items, err := m.ListMemoryItems(ctx, userID, MemoryItemFilter{
		Status: MemoryStatusActive,
	})
	if err != nil {
		return "", err
	}
	if len(items) > 0 {
		query := lastVisibleUserMessage(session)
		selected := selectMemoryItemsForSessionContext(items, query, session.ID, 12)
		now := time.Now().UTC()
		for i := range selected {
			selected[i] = recordMemoryInjection(selected[i], session.ID, query, now)
			if _, err := m.UpdateMemoryItem(ctx, userID, selected[i]); err != nil {
				return "", err
			}
		}
		return "# Memory\n\n" + formatMemoryItems(selected), nil
	}
	var parts []string
	if content, err := m.LoadUserMemory(ctx, userID); err == nil && strings.TrimSpace(content) != "" {
		parts = append(parts, "# User memory\n\n"+content)
	}
	if content, err := m.LoadSessionMemory(ctx, userID, session.ID); err == nil && strings.TrimSpace(content) != "" {
		parts = append(parts, "# Session memory\n\n"+content)
	}
	return strings.Join(parts, "\n\n"), nil
}

func (m *ObjectMemoryService) LoadUserMemory(ctx context.Context, userID string) (string, error) {
	data, err := m.objects.Get(ctx, m.userMemoryKey(userID))
	if err != nil {
		return "", nil
	}
	return string(data), nil
}

func (m *ObjectMemoryService) LoadSessionMemory(ctx context.Context, userID, sessionID string) (string, error) {
	items, err := m.ListMemoryItems(ctx, userID, MemoryItemFilter{
		SessionID: sessionID,
		Kind:      MemoryKindSession,
	})
	if err != nil {
		return "", err
	}
	if len(items) > 0 {
		return formatMemoryItems(items), nil
	}
	data, err := m.objects.Get(ctx, m.sessionMemoryKey(userID, sessionID))
	if err != nil {
		return "", nil
	}
	return string(data), nil
}

func (m *ObjectMemoryService) AfterTurn(ctx context.Context, userID string, session *state.Session) error {
	if session == nil {
		return nil
	}
	candidates := extractMemoryItems(userID, session)
	if len(candidates) == 0 {
		return nil
	}
	existing, err := m.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		var conflictUpdates []MemoryItem
		candidate, conflictUpdates = applyMemoryConflictResolution(existing, candidate)
		for _, update := range conflictUpdates {
			if _, err := m.UpdateMemoryItem(ctx, userID, update); err != nil {
				return err
			}
			existing = append(existing, update)
		}
		item := upsertMemoryItem(existing, candidate)
		data, err := json.MarshalIndent(item, "", "  ")
		if err != nil {
			return err
		}
		if err := m.objects.Put(ctx, m.memoryItemKey(userID, item.ID), data, "application/json; charset=utf-8"); err != nil {
			return err
		}
		existing = append(existing, item)
	}
	return nil
}

func (m *ObjectMemoryService) DeleteSession(ctx context.Context, userID, sessionID string) error {
	if err := m.objects.Delete(ctx, m.sessionMemoryKey(userID, sessionID)); err != nil {
		return err
	}
	items, err := m.ListMemoryItems(ctx, userID, MemoryItemFilter{SessionID: sessionID})
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := m.objects.Delete(ctx, m.memoryItemKey(userID, item.ID)); err != nil {
			return err
		}
	}
	return nil
}

func (m *ObjectMemoryService) DeleteUser(ctx context.Context, userID string) error {
	keys, err := m.objects.List(ctx, joinObjectKey(m.prefix, "users", userPathID(userID), "memory"))
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := m.objects.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (m *ObjectMemoryService) GetMemorySettings(ctx context.Context, userID string) (MemorySettings, error) {
	data, err := m.objects.Get(ctx, m.memorySettingsKey(userID))
	if err != nil {
		return defaultMemorySettings(), nil
	}
	var settings MemorySettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return MemorySettings{}, err
	}
	return normalizeMemorySettings(settings), nil
}

func (m *ObjectMemoryService) UpdateMemorySettings(ctx context.Context, userID string, settings MemorySettings) (MemorySettings, error) {
	settings.UpdatedAt = time.Now().UTC()
	settings = normalizeMemorySettings(settings)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return MemorySettings{}, err
	}
	if err := m.objects.Put(ctx, m.memorySettingsKey(userID), data, "application/json; charset=utf-8"); err != nil {
		return MemorySettings{}, err
	}
	return settings, nil
}

func (m *ObjectMemoryService) PruneBefore(ctx context.Context, _ time.Time) (int, error) {
	keys, err := m.objects.List(ctx, joinObjectKey(m.prefix, "users"))
	if err != nil {
		return 0, err
	}
	changed := 0
	now := time.Now().UTC()
	for _, key := range keys {
		if !strings.Contains(key, "/memory/items/") || !strings.HasSuffix(key, ".json") {
			continue
		}
		data, err := m.objects.Get(ctx, key)
		if err != nil {
			return changed, err
		}
		var item MemoryItem
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		updated, ok := applyMemoryLifecycle(item, now)
		if !ok || updated.UserID == "" {
			continue
		}
		if _, err := m.UpdateMemoryItem(ctx, updated.UserID, updated); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func (m *ObjectMemoryService) userMemoryKey(userID string) string {
	return joinObjectKey(m.prefix, "users", userPathID(userID), "memory", "user.md")
}

func (m *ObjectMemoryService) sessionMemoryKey(userID, sessionID string) string {
	return joinObjectKey(m.prefix, "users", userPathID(userID), "memory", "sessions", filepath.Base(sessionID)+".md")
}

func (m *ObjectMemoryService) memoryItemsPrefix(userID string) string {
	return joinObjectKey(m.prefix, "users", userPathID(userID), "memory", "items")
}

func (m *ObjectMemoryService) memoryItemKey(userID, itemID string) string {
	return joinObjectKey(m.memoryItemsPrefix(userID), filepath.Base(itemID)+".json")
}

func (m *ObjectMemoryService) memorySettingsKey(userID string) string {
	return joinObjectKey(m.prefix, "users", userPathID(userID), "memory", "settings.json")
}

func (m *ObjectMemoryService) ListMemoryItems(ctx context.Context, userID string, filter MemoryItemFilter) ([]MemoryItem, error) {
	keys, err := m.objects.List(ctx, m.memoryItemsPrefix(userID))
	if err != nil {
		return nil, err
	}
	items := make([]MemoryItem, 0, len(keys))
	for _, key := range keys {
		if !strings.HasSuffix(key, ".json") {
			continue
		}
		data, err := m.objects.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		var item MemoryItem
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		if item.UserID != "" && item.UserID != userID {
			continue
		}
		item = normalizeMemoryItem(item)
		if memoryItemMatches(item, filter) {
			items = append(items, item)
		}
	}
	sortMemoryItems(items)
	return limitMemoryItems(items, filter.Limit), nil
}

func (m *ObjectMemoryService) GetMemoryItem(ctx context.Context, userID, itemID string) (MemoryItem, error) {
	data, err := m.objects.Get(ctx, m.memoryItemKey(userID, itemID))
	if err != nil {
		return MemoryItem{}, err
	}
	var item MemoryItem
	if err := json.Unmarshal(data, &item); err != nil {
		return MemoryItem{}, err
	}
	if item.UserID != "" && item.UserID != userID {
		return MemoryItem{}, fmt.Errorf("memory item not found")
	}
	return normalizeMemoryItem(item), nil
}

func (m *ObjectMemoryService) UpdateMemoryItem(ctx context.Context, userID string, item MemoryItem) (MemoryItem, error) {
	if strings.TrimSpace(item.ID) == "" {
		return MemoryItem{}, fmt.Errorf("memory item ID is required")
	}
	item.UserID = userID
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now().UTC()
	}
	item = normalizeMemoryItem(item)
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return MemoryItem{}, err
	}
	if err := m.objects.Put(ctx, m.memoryItemKey(userID, item.ID), data, "application/json; charset=utf-8"); err != nil {
		return MemoryItem{}, err
	}
	return item, nil
}

func (m *ObjectMemoryService) DeleteMemoryItem(ctx context.Context, userID, itemID string) error {
	return m.objects.Delete(ctx, m.memoryItemKey(userID, itemID))
}

func joinObjectKey(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" && part != "." {
			clean = append(clean, part)
		}
	}
	return strings.Join(clean, "/")
}
