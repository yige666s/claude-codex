package prompt

import (
	"sync"
	"time"
)

// SectionCache manages cached [REDACTED] section values.
type SectionCache struct {
	entries map[string]*cacheEntry
	mu      sync.RWMutex
}

type cacheEntry struct {
	value     string
	timestamp time.Time
}

// NewSectionCache creates a new section cache.
func NewSectionCache() *SectionCache {
	return &SectionCache{
		entries: make(map[string]*cacheEntry),
	}
}

// Get retrieves a cached section value.
func (c *SectionCache) Get(name string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, found := c.entries[name]
	if !found {
		return "", false
	}
	return entry.value, true
}

// Set stores a section value in the cache.
func (c *SectionCache) Set(name string, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[name] = &cacheEntry{
		value:     value,
		timestamp: time.Now(),
	}
}

// Delete removes a section from the cache.
func (c *SectionCache) Delete(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, name)
}

// Clear removes all cached sections.
func (c *SectionCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
}

// Has checks if a section is cached.
func (c *SectionCache) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, found := c.entries[name]
	return found
}

// Size returns the number of cached sections.
func (c *SectionCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// CacheStats provides statistics about the cache.
type CacheStats struct {
	Size         int
	OldestEntry  time.Time
	NewestEntry  time.Time
	SectionNames []string
}

// Stats returns statistics about the cache.
func (c *SectionCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := CacheStats{
		Size:         len(c.entries),
		SectionNames: make([]string, 0, len(c.entries)),
	}

	var oldest, newest time.Time
	first := true

	for name, entry := range c.entries {
		stats.SectionNames = append(stats.SectionNames, name)

		if first {
			oldest = entry.timestamp
			newest = entry.timestamp
			first = false
		} else {
			if entry.timestamp.Before(oldest) {
				oldest = entry.timestamp
			}
			if entry.timestamp.After(newest) {
				newest = entry.timestamp
			}
		}
	}

	stats.OldestEntry = oldest
	stats.NewestEntry = newest

	return stats
}

// InvalidateOlderThan removes cache entries older than the specified duration.
func (c *SectionCache) InvalidateOlderThan(duration time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-duration)
	removed := 0

	for name, entry := range c.entries {
		if entry.timestamp.Before(cutoff) {
			delete(c.entries, name)
			removed++
		}
	}

	return removed
}

// GetAll returns all cached section names and values.
func (c *SectionCache) GetAll() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]string, len(c.entries))
	for name, entry := range c.entries {
		result[name] = entry.value
	}
	return result
}
