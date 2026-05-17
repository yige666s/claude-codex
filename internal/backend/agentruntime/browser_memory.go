package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
)

const browserMemoryInjectedKey = "agentruntime.browser_memory_context_injected"

func (r *Runtime) SaveBrowserMemory(ctx context.Context, userID string, req BrowserMemoryRequest) (MemoryItem, error) {
	if r.memory == nil {
		return MemoryItem{}, fmt.Errorf("memory is not configured")
	}
	settings, err := r.GetMemorySettings(ctx, userID)
	if err != nil {
		return MemoryItem{}, err
	}
	if !settings.Enabled || !settings.CaptureEnabled {
		return MemoryItem{}, fmt.Errorf("memory capture is disabled")
	}
	personalization, err := r.GetPersonalizationSettings(ctx, userID)
	if err != nil {
		return MemoryItem{}, err
	}
	if !personalization.FeatureFlags.UseBrowserMemory {
		return MemoryItem{}, fmt.Errorf("browser memory is disabled")
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return MemoryItem{}, fmt.Errorf("memory item operations are not supported")
	}
	item, err := browserMemoryItem(userID, req, time.Now().UTC())
	if err != nil {
		return MemoryItem{}, err
	}
	existing, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{Namespace: MemoryNamespaceBrowser, Status: ""})
	if err != nil {
		return MemoryItem{}, err
	}
	item = upsertMemoryItem(existing, item)
	return service.UpdateMemoryItem(ctx, userID, item)
}

func (r *Runtime) injectBrowserMemory(ctx context.Context, userID string, session *state.Session) error {
	if session == nil || r.memory == nil {
		return nil
	}
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	if session.Metadata[browserMemoryInjectedKey] == "true" {
		return nil
	}
	personalization, err := r.GetPersonalizationSettings(ctx, userID)
	if err != nil {
		return err
	}
	if !personalization.FeatureFlags.UseBrowserMemory {
		return nil
	}
	content, err := r.browserMemoryContext(ctx, userID, session)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	session.AddSystemContext(content)
	session.Metadata[browserMemoryInjectedKey] = "true"
	return nil
}

func (r *Runtime) browserMemoryContext(ctx context.Context, userID string, session *state.Session) (string, error) {
	if session == nil || r.memory == nil {
		return "", nil
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return "", nil
	}
	items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{
		Namespace: MemoryNamespaceBrowser,
		Status:    MemoryStatusActive,
	})
	if err != nil {
		return "", err
	}
	selected := selectMemoryItemsForSessionContext(items, lastVisibleUserMessage(session), session.ID, 5)
	if len(selected) == 0 {
		return "", nil
	}
	now := time.Now().UTC()
	for i := range selected {
		selected[i] = recordMemoryInjection(selected[i], session.ID, lastVisibleUserMessage(session), now)
		if _, err := service.UpdateMemoryItem(ctx, userID, selected[i]); err != nil {
			return "", err
		}
	}
	return "<browser-memory>\n# Browser Memory\n\n" + formatMemoryItems(selected) + "\n</browser-memory>", nil
}

func browserMemoryItem(userID string, req BrowserMemoryRequest, now time.Time) (MemoryItem, error) {
	title := truncatePersonalizationText(req.Title, 240)
	content := truncatePersonalizationText(req.Content, 4000)
	rawURL := truncatePersonalizationText(req.URL, 2000)
	normalizedURL := normalizeBrowserMemoryURL(rawURL)
	if strings.TrimSpace(title) == "" && strings.TrimSpace(content) == "" && strings.TrimSpace(normalizedURL) == "" {
		return MemoryItem{}, fmt.Errorf("browser memory requires url, title, or content")
	}
	if strings.TrimSpace(content) == "" {
		content = title
	}
	item := newConversationMemoryItem(userID, strings.TrimSpace(req.SessionID), formatBrowserMemoryContent(title, normalizedURL, content))
	item.ID = browserMemoryID(normalizedURL, title, content)
	item.Namespace = MemoryNamespaceBrowser
	item.Kind = MemoryKindSession
	item.Level = MemoryLevelAtomic
	item.Category = MemoryCategoryFact
	item.Source = MemorySourceBrowser
	item.Visibility = normalizeBrowserMemoryVisibility(req.Visibility)
	item.Confidence = 0.85
	item.Weight = 0.75
	item.Tags = normalizeMemoryTags(append([]string{"browser", "external-context"}, req.Tags...))
	item.SourceRefs = []MemorySourceRef{{
		Kind:      MemorySourceBrowser,
		ID:        browserMemorySourceID(normalizedURL, title),
		Filename:  title,
		SessionID: strings.TrimSpace(req.SessionID),
		URI:       normalizedURL,
	}}
	item.CreatedAt = now
	item.UpdatedAt = now
	item.Metadata = map[string]any{
		"source":        "browser_memory",
		"browser_url":   normalizedURL,
		"browser_title": title,
	}
	return normalizeMemoryItem(item), nil
}

func normalizeBrowserMemoryVisibility(value string) string {
	value = normalizeMemoryVisibility(value)
	if value == MemoryVisibilityShared {
		return MemoryVisibilityUser
	}
	return value
}

func normalizeBrowserMemoryURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}
	parsed.Fragment = ""
	return parsed.String()
}

func formatBrowserMemoryContent(title, uri, content string) string {
	var parts []string
	if strings.TrimSpace(title) != "" {
		parts = append(parts, "Browser page: "+strings.TrimSpace(title))
	}
	if strings.TrimSpace(uri) != "" {
		parts = append(parts, "URL: "+strings.TrimSpace(uri))
	}
	if strings.TrimSpace(content) != "" {
		parts = append(parts, "Context: "+strings.TrimSpace(content))
	}
	return strings.Join(parts, "\n")
}

func browserMemoryID(uri, title, content string) string {
	basis := strings.TrimSpace(strings.ToLower(uri))
	if basis == "" {
		basis = strings.TrimSpace(strings.ToLower(title))
	}
	if basis == "" {
		basis = strings.TrimSpace(content)
	}
	sum := sha256.Sum256([]byte("browser-memory\x00" + basis))
	return "mem_browser_" + hex.EncodeToString(sum[:])[:16]
}

func browserMemorySourceID(uri, title string) string {
	basis := strings.TrimSpace(uri)
	if basis == "" {
		basis = strings.TrimSpace(title)
	}
	sum := sha256.Sum256([]byte("browser-source\x00" + basis))
	return hex.EncodeToString(sum[:])[:16]
}
