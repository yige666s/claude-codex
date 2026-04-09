package context

import (
	"sync"
	"time"
)

// CacheEntry represents a cached context value with metadata
type CacheEntry struct {
	Value     interface{}
	Timestamp time.Time
	TTL       time.Duration
}

// ContextCache manages cached context data with TTL support
type ContextCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
}

// NewContextCache creates a new context cache
func NewContextCache() *ContextCache {
	return &ContextCache{
		entries: make(map[string]*CacheEntry),
	}
}

// Get retrieves a value from the cache
func (c *ContextCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil, false
	}

	// Check if entry has expired
	if entry.TTL > 0 && time.Since(entry.Timestamp) > entry.TTL {
		return nil, false
	}

	return entry.Value, true
}

// Set stores a value in the cache with optional TTL
func (c *ContextCache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &CacheEntry{
		Value:     value,
		Timestamp: time.Now(),
		TTL:       ttl,
	}
}

// Delete removes a value from the cache
func (c *ContextCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
}

// Clear removes all entries from the cache
func (c *ContextCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
}

// Invalidate removes expired entries from the cache
func (c *ContextCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if entry.TTL > 0 && now.Sub(entry.Timestamp) > entry.TTL {
			delete(c.entries, key)
		}
	}
}

// Size returns the number of entries in the cache
func (c *ContextCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

// Keys returns all keys in the cache
func (c *ContextCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.entries))
	for key := range c.entries {
		keys = append(keys, key)
	}
	return keys
}

// Global cache instance for context data
var globalCache = NewContextCache()

// GetGlobalCache returns the global context cache
func GetGlobalCache() *ContextCache {
	return globalCache
}

// ClearAllCaches clears all context caches
func ClearAllCaches() {
	ClearSystemContextCache()
	ClearUserContextCache()
	ClearGitStatusCache()
	globalCache.Clear()
}
