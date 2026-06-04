package agentruntime

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
	"claude-codex/internal/public/fsutil"
)

type FileSessionStore struct {
	root string
}

func NewFileSessionStore(root string) *FileSessionStore {
	return &FileSessionStore{root: root}
}

func (s *FileSessionStore) Create(_ context.Context, userID, workingDir string) (*state.Session, error) {
	session := state.NewSession(workingDir)
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	session.Metadata["user_id_hash"] = userPathID(userID)
	if err := s.Save(context.Background(), userID, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *FileSessionStore) Get(_ context.Context, userID, sessionID string) (*state.Session, error) {
	path := s.sessionPath(userID, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var session state.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	if session.Status == state.SessionStatusDeleted {
		return nil, os.ErrNotExist
	}
	return &session, nil
}

func (s *FileSessionStore) List(_ context.Context, userID string) ([]*state.Session, error) {
	dir := s.sessionsDir(userID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*state.Session{}, nil
		}
		return nil, err
	}
	out := make([]*state.Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".json")
		session, err := s.Get(context.Background(), userID, sessionID)
		if err == nil && session.Status != state.SessionStatusDeleted {
			out = append(out, session)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *FileSessionStore) Save(_ context.Context, userID string, session *state.Session) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("user ID is required")
	}
	if session.ID == "" {
		return fmt.Errorf("session ID is required")
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	path := s.sessionPath(userID, session.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

func (s *FileSessionStore) Delete(_ context.Context, userID, sessionID string) error {
	session, err := s.getIncludingDeleted(userID, sessionID)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	softDeleteSession(session, time.Now().UTC())
	return s.Save(context.Background(), userID, session)
}

func (s *FileSessionStore) DeleteUser(_ context.Context, userID string) error {
	entries, err := os.ReadDir(s.sessionsDir(userID))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".json")
		session, err := s.getIncludingDeleted(userID, sessionID)
		if err != nil {
			return err
		}
		softDeleteSession(session, now)
		if err := s.Save(context.Background(), userID, session); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileSessionStore) PruneBefore(ctx context.Context, cutoff time.Time) (int, error) {
	root := filepath.Join(s.root, "users")
	pruned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var session state.Session
		if err := json.Unmarshal(data, &session); err != nil {
			return nil
		}
		if session.Status != state.SessionStatusDeleted && session.UpdatedAt.Before(cutoff) {
			softDeleteSession(&session, time.Now().UTC())
			data, err := json.MarshalIndent(&session, "", "  ")
			if err != nil {
				return err
			}
			if err := fsutil.WriteFileAtomic(path, data, 0o644); err != nil {
				return err
			}
			pruned++
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return pruned, err
}

func (s *FileSessionStore) getIncludingDeleted(userID, sessionID string) (*state.Session, error) {
	path := s.sessionPath(userID, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var session state.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func softDeleteSession(session *state.Session, at time.Time) {
	if session == nil {
		return
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	session.Status = state.SessionStatusDeleted
	session.Archived = true
	session.UpdatedAt = at
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	session.Metadata["deleted_at"] = at.Format(time.RFC3339Nano)
	for i := range session.Messages {
		session.Messages[i].Status = state.MessageStatusDeleted
		session.Messages[i].UpdatedAt = at
	}
}

func (s *FileSessionStore) sessionsDir(userID string) string {
	return filepath.Join(s.root, "users", userPathID(userID), "sessions")
}

func (s *FileSessionStore) sessionPath(userID, sessionID string) string {
	return filepath.Join(s.sessionsDir(userID), filepath.Base(sessionID)+".json")
}

type FileMemoryService struct {
	root string
}

func NewFileMemoryService(root string) *FileMemoryService {
	return &FileMemoryService{root: root}
}

func (m *FileMemoryService) LoadContext(ctx context.Context, userID string, session *state.Session) (string, error) {
	if strings.TrimSpace(userID) == "" || session == nil {
		return "", nil
	}
	items, err := m.ListMemoryItems(ctx, userID, MemoryItemFilter{
		Status: MemoryStatusActive,
	})
	if err != nil {
		return "", err
	}
	if len(items) > 0 {
		selected := selectMemoryItemsForSessionContext(items, lastVisibleUserMessage(session), session.ID, 12)
		now := time.Now().UTC()
		for i := range selected {
			selected[i] = recordMemoryInjection(selected[i], session.ID, lastVisibleUserMessage(session), now)
			if _, err := m.UpdateMemoryItem(ctx, userID, selected[i]); err != nil {
				return "", err
			}
		}
		return "# Memory\n\n" + formatMemoryItems(selected), nil
	}
	parts := make([]string, 0, 2)
	userMemory, err := m.LoadUserMemory(context.Background(), userID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(userMemory) != "" {
		parts = append(parts, "# User memory\n\n"+userMemory)
	}
	sessionMemory, err := m.LoadSessionMemory(context.Background(), userID, session.ID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(sessionMemory) != "" {
		parts = append(parts, "# Session memory\n\n"+sessionMemory)
	}
	return strings.Join(parts, "\n\n"), nil
}

func (m *FileMemoryService) LoadUserMemory(_ context.Context, userID string) (string, error) {
	data, err := os.ReadFile(m.userMemoryPath(userID))
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

func (m *FileMemoryService) LoadSessionMemory(_ context.Context, userID, sessionID string) (string, error) {
	items, err := m.ListMemoryItems(context.Background(), userID, MemoryItemFilter{
		SessionID: sessionID,
		Kind:      MemoryKindSession,
	})
	if err != nil {
		return "", err
	}
	if len(items) > 0 {
		return formatMemoryItems(items), nil
	}
	data, err := os.ReadFile(m.sessionMemoryPath(userID, sessionID))
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

func (m *FileMemoryService) AfterTurn(_ context.Context, userID string, session *state.Session) error {
	if strings.TrimSpace(userID) == "" || session == nil || session.ID == "" {
		return nil
	}
	candidates := extractMemoryItems(userID, session)
	if len(candidates) == 0 {
		return nil
	}
	existing, err := m.ListMemoryItems(context.Background(), userID, MemoryItemFilter{})
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		var conflictUpdates []MemoryItem
		candidate, conflictUpdates = applyMemoryConflictResolution(existing, candidate)
		for _, update := range conflictUpdates {
			data, err := json.MarshalIndent(update, "", "  ")
			if err != nil {
				return err
			}
			if err := fsutil.WriteFileAtomic(m.memoryItemPath(userID, update.ID), data, 0o644); err != nil {
				return err
			}
			existing = append(existing, update)
		}
		item := upsertMemoryItem(existing, candidate)
		data, err := json.MarshalIndent(item, "", "  ")
		if err != nil {
			return err
		}
		if err := fsutil.WriteFileAtomic(m.memoryItemPath(userID, item.ID), data, 0o644); err != nil {
			return err
		}
		existing = append(existing, item)
	}
	return nil
}

func (m *FileMemoryService) DeleteSession(_ context.Context, userID, sessionID string) error {
	if err := os.Remove(m.sessionMemoryPath(userID, sessionID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	items, err := m.ListMemoryItems(context.Background(), userID, MemoryItemFilter{SessionID: sessionID})
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := os.Remove(m.memoryItemPath(userID, item.ID)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (m *FileMemoryService) DeleteUser(_ context.Context, userID string) error {
	err := os.RemoveAll(filepath.Join(m.root, "users", userPathID(userID), "memory"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (m *FileMemoryService) DeleteSavedMemory(ctx context.Context, userID string) error {
	if err := os.Remove(m.userMemoryPath(userID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.RemoveAll(filepath.Join(m.root, "users", userPathID(userID), "memory", "sessions")); err != nil && !os.IsNotExist(err) {
		return err
	}
	items, err := m.ListMemoryItems(ctx, userID, MemoryItemFilter{Status: ""})
	if err != nil {
		return err
	}
	for _, item := range items {
		if isManagedPersonalizationMemory(item) {
			continue
		}
		if err := os.Remove(m.memoryItemPath(userID, item.ID)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (m *FileMemoryService) GetMemorySettings(_ context.Context, userID string) (MemorySettings, error) {
	data, err := os.ReadFile(m.memorySettingsPath(userID))
	if os.IsNotExist(err) {
		return defaultMemorySettings(), nil
	}
	if err != nil {
		return MemorySettings{}, err
	}
	var settings MemorySettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return MemorySettings{}, err
	}
	return normalizeMemorySettings(settings), nil
}

func (m *FileMemoryService) UpdateMemorySettings(_ context.Context, userID string, settings MemorySettings) (MemorySettings, error) {
	settings.UpdatedAt = time.Now().UTC()
	settings = normalizeMemorySettings(settings)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return MemorySettings{}, err
	}
	if err := fsutil.WriteFileAtomic(m.memorySettingsPath(userID), data, 0o644); err != nil {
		return MemorySettings{}, err
	}
	return settings, nil
}

func (m *FileMemoryService) GetPersonalizationSettings(_ context.Context, userID string) (PersonalizationSettings, error) {
	data, err := os.ReadFile(m.personalizationSettingsPath(userID))
	if os.IsNotExist(err) {
		return defaultPersonalizationSettings(), nil
	}
	if err != nil {
		return PersonalizationSettings{}, err
	}
	var settings PersonalizationSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return PersonalizationSettings{}, err
	}
	return normalizePersonalizationSettings(settings), nil
}

func (m *FileMemoryService) UpdatePersonalizationSettings(_ context.Context, userID string, settings PersonalizationSettings) (PersonalizationSettings, error) {
	settings.UpdatedAt = time.Now().UTC()
	settings = normalizePersonalizationSettings(settings)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return PersonalizationSettings{}, err
	}
	if err := fsutil.WriteFileAtomic(m.personalizationSettingsPath(userID), data, 0o644); err != nil {
		return PersonalizationSettings{}, err
	}
	return settings, nil
}

func (m *FileMemoryService) DeletePersonalizationSettings(_ context.Context, userID string) error {
	if err := os.Remove(m.personalizationSettingsPath(userID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *FileMemoryService) PruneBefore(ctx context.Context, cutoff time.Time) (int, error) {
	now := time.Now().UTC()
	items, err := m.listAllMemoryItems(ctx)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, item := range items {
		updated, ok := applyMemoryLifecycle(item, now)
		if !ok {
			continue
		}
		if _, err := m.UpdateMemoryItem(ctx, item.UserID, updated); err != nil {
			return changed, err
		}
		changed++
	}
	root := filepath.Join(m.root, "users")
	pruned := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		normalizedPath := filepath.ToSlash(path)
		if err != nil || d.IsDir() || !strings.Contains(normalizedPath, "/memory/") || !strings.HasSuffix(path, ".md") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			pruned++
		}
		return nil
	})
	if os.IsNotExist(err) {
		return changed, nil
	}
	return changed + pruned, err
}

func (m *FileMemoryService) ListAllMemoryItems(ctx context.Context) ([]MemoryItem, error) {
	return m.listAllMemoryItems(ctx)
}

func (m *FileMemoryService) userMemoryPath(userID string) string {
	return filepath.Join(m.root, "users", userPathID(userID), "memory", "user.md")
}

func (m *FileMemoryService) sessionMemoryPath(userID, sessionID string) string {
	return filepath.Join(m.root, "users", userPathID(userID), "memory", "sessions", filepath.Base(sessionID)+".md")
}

func (m *FileMemoryService) memoryItemsDir(userID string) string {
	return filepath.Join(m.root, "users", userPathID(userID), "memory", "items")
}

func (m *FileMemoryService) memoryItemPath(userID, itemID string) string {
	return filepath.Join(m.memoryItemsDir(userID), filepath.Base(itemID)+".json")
}

func (m *FileMemoryService) memorySettingsPath(userID string) string {
	return filepath.Join(m.root, "users", userPathID(userID), "memory", "settings.json")
}

func (m *FileMemoryService) personalizationSettingsPath(userID string) string {
	return filepath.Join(m.root, "users", userPathID(userID), "memory", "personalization.json")
}

func (m *FileMemoryService) ListMemoryItems(_ context.Context, userID string, filter MemoryItemFilter) ([]MemoryItem, error) {
	entries, err := os.ReadDir(m.memoryItemsDir(userID))
	if os.IsNotExist(err) {
		return []MemoryItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]MemoryItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.memoryItemsDir(userID), entry.Name()))
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

func (m *FileMemoryService) GetMemoryItem(_ context.Context, userID, itemID string) (MemoryItem, error) {
	data, err := os.ReadFile(m.memoryItemPath(userID, itemID))
	if err != nil {
		return MemoryItem{}, err
	}
	var item MemoryItem
	if err := json.Unmarshal(data, &item); err != nil {
		return MemoryItem{}, err
	}
	if item.UserID != "" && item.UserID != userID {
		return MemoryItem{}, os.ErrNotExist
	}
	return normalizeMemoryItem(item), nil
}

func (m *FileMemoryService) UpdateMemoryItem(_ context.Context, userID string, item MemoryItem) (MemoryItem, error) {
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
	if err := fsutil.WriteFileAtomic(m.memoryItemPath(userID, item.ID), data, 0o644); err != nil {
		return MemoryItem{}, err
	}
	return item, nil
}

func (m *FileMemoryService) DeleteMemoryItem(_ context.Context, userID, itemID string) error {
	err := os.Remove(m.memoryItemPath(userID, itemID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (m *FileMemoryService) listAllMemoryItems(ctx context.Context) ([]MemoryItem, error) {
	root := filepath.Join(m.root, "users")
	var items []MemoryItem
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") || !strings.Contains(filepath.ToSlash(path), "/memory/items/") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var item MemoryItem
		if err := json.Unmarshal(data, &item); err != nil {
			return nil
		}
		items = append(items, normalizeMemoryItem(item))
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return items, err
}

func summarizeLastTurn(session *state.Session) string {
	var user, assistant string
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		switch msg.Role {
		case "assistant":
			if assistant == "" {
				assistant = strings.TrimSpace(msg.Content)
			}
		case "user":
			if user == "" && !msg.Hidden {
				user = strings.TrimSpace(msg.Content)
			}
		}
		if user != "" && assistant != "" {
			break
		}
	}
	if user == "" && assistant == "" {
		return ""
	}
	return fmt.Sprintf("- user: %s\n- assistant: %s", truncateForMemory(user), truncateForMemory(assistant))
}

func newConversationMemoryItem(userID, sessionID, content string) MemoryItem {
	now := time.Now().UTC()
	return MemoryItem{
		ID:         newMemoryID(),
		UserID:     userID,
		SessionID:  sessionID,
		Kind:       MemoryKindSession,
		Source:     MemorySourceConversation,
		Visibility: MemoryVisibilityUser,
		Content:    strings.TrimSpace(content),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func newMemoryID() string {
	var random [8]byte
	if _, err := crand.Read(random[:]); err == nil {
		return "mem_" + hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("mem_%d", time.Now().UTC().UnixNano())
}

func memoryItemMatches(item MemoryItem, filter MemoryItemFilter) bool {
	if filter.SessionID != "" && item.SessionID != filter.SessionID {
		return false
	}
	if filter.Namespace != "" && item.Namespace != normalizeMemoryNamespace(filter.Namespace) {
		return false
	}
	if filter.Kind != "" && item.Kind != filter.Kind {
		return false
	}
	if filter.Level != "" && item.Level != filter.Level {
		return false
	}
	if filter.Category != "" && item.Category != filter.Category {
		return false
	}
	if filter.Visibility != "" && item.Visibility != filter.Visibility {
		return false
	}
	if filter.Status != "" && item.Status != filter.Status {
		return false
	}
	if filter.Query != "" && !strings.Contains(strings.ToLower(item.Content), strings.ToLower(filter.Query)) {
		return false
	}
	if filter.SourceKind != "" || filter.SourceID != "" {
		if !memoryItemHasSourceRef(item, filter.SourceKind, filter.SourceID) {
			return false
		}
	}
	return true
}

func sortMemoryItems(items []MemoryItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Weight == items[j].Weight {
			if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
				return items[i].ID > items[j].ID
			}
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].Weight > items[j].Weight
	})
}

func limitMemoryItems(items []MemoryItem, limit int) []MemoryItem {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func truncateForMemory(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if len(value) <= 800 {
		return value
	}
	return value[:797] + "..."
}

func userPathID(userID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(userID)))
	return hex.EncodeToString(sum[:])[:32]
}
