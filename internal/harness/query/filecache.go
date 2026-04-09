package query

import (
	"path/filepath"
	"sync"
)

// FileState represents the cached state of a file.
type FileState struct {
	Content       string
	Timestamp     int64
	Offset        *int
	Limit         *int
	IsPartialView bool
}

// FileStateCache is a cache for file states with size limits.
type FileStateCache struct {
	mu            sync.RWMutex
	cache         map[string]*FileState
	maxEntries    int
	maxSizeBytes  int64
	currentSize   int64
	accessOrder   []string // LRU tracking
}

// NewFileStateCache creates a new file state cache.
func NewFileStateCache(maxEntries int, maxSizeBytes int64) *FileStateCache {
	return &FileStateCache{
		cache:        make(map[string]*FileState),
		maxEntries:   maxEntries,
		maxSizeBytes: maxSizeBytes,
		accessOrder:  make([]string, 0, maxEntries),
	}
}

// Get retrieves a file state from the cache.
func (fsc *FileStateCache) Get(key string) (*FileState, bool) {
	fsc.mu.Lock()
	defer fsc.mu.Unlock()

	normalizedKey := filepath.Clean(key)
	state, ok := fsc.cache[normalizedKey]
	if !ok {
		return nil, false
	}

	// Update access order (move to end)
	fsc.updateAccessOrder(normalizedKey)

	return state, true
}

// Set adds or updates a file state in the cache.
func (fsc *FileStateCache) Set(key string, state *FileState) {
	fsc.mu.Lock()
	defer fsc.mu.Unlock()

	normalizedKey := filepath.Clean(key)

	// Calculate size
	stateSize := int64(len(state.Content))

	// If key already exists, subtract old size
	if oldState, exists := fsc.cache[normalizedKey]; exists {
		fsc.currentSize -= int64(len(oldState.Content))
	}

	// Evict entries if necessary
	for (len(fsc.cache) >= fsc.maxEntries || fsc.currentSize+stateSize > fsc.maxSizeBytes) && len(fsc.accessOrder) > 0 {
		fsc.evictOldest()
	}

	// Add new entry
	fsc.cache[normalizedKey] = state
	fsc.currentSize += stateSize
	fsc.updateAccessOrder(normalizedKey)
}

// Has checks if a key exists in the cache.
func (fsc *FileStateCache) Has(key string) bool {
	fsc.mu.RLock()
	defer fsc.mu.RUnlock()

	normalizedKey := filepath.Clean(key)
	_, ok := fsc.cache[normalizedKey]
	return ok
}

// Delete removes a file state from the cache.
func (fsc *FileStateCache) Delete(key string) bool {
	fsc.mu.Lock()
	defer fsc.mu.Unlock()

	normalizedKey := filepath.Clean(key)
	state, ok := fsc.cache[normalizedKey]
	if !ok {
		return false
	}

	fsc.currentSize -= int64(len(state.Content))
	delete(fsc.cache, normalizedKey)
	fsc.removeFromAccessOrder(normalizedKey)

	return true
}

// Clear removes all entries from the cache.
func (fsc *FileStateCache) Clear() {
	fsc.mu.Lock()
	defer fsc.mu.Unlock()

	fsc.cache = make(map[string]*FileState)
	fsc.accessOrder = make([]string, 0, fsc.maxEntries)
	fsc.currentSize = 0
}

// Keys returns all keys in the cache.
func (fsc *FileStateCache) Keys() []string {
	fsc.mu.RLock()
	defer fsc.mu.RUnlock()

	keys := make([]string, 0, len(fsc.cache))
	for k := range fsc.cache {
		keys = append(keys, k)
	}
	return keys
}

// Entries returns all entries in the cache.
func (fsc *FileStateCache) Entries() map[string]*FileState {
	fsc.mu.RLock()
	defer fsc.mu.RUnlock()

	entries := make(map[string]*FileState, len(fsc.cache))
	for k, v := range fsc.cache {
		entries[k] = v
	}
	return entries
}

// Size returns the number of entries in the cache.
func (fsc *FileStateCache) Size() int {
	fsc.mu.RLock()
	defer fsc.mu.RUnlock()

	return len(fsc.cache)
}

// CurrentSizeBytes returns the current size in bytes.
func (fsc *FileStateCache) CurrentSizeBytes() int64 {
	fsc.mu.RLock()
	defer fsc.mu.RUnlock()

	return fsc.currentSize
}

// updateAccessOrder updates the access order for LRU eviction.
func (fsc *FileStateCache) updateAccessOrder(key string) {
	// Remove from current position
	fsc.removeFromAccessOrder(key)
	// Add to end
	fsc.accessOrder = append(fsc.accessOrder, key)
}

// removeFromAccessOrder removes a key from the access order.
func (fsc *FileStateCache) removeFromAccessOrder(key string) {
	for i, k := range fsc.accessOrder {
		if k == key {
			fsc.accessOrder = append(fsc.accessOrder[:i], fsc.accessOrder[i+1:]...)
			break
		}
	}
}

// evictOldest evicts the oldest entry from the cache.
func (fsc *FileStateCache) evictOldest() {
	if len(fsc.accessOrder) == 0 {
		return
	}

	oldestKey := fsc.accessOrder[0]
	if state, ok := fsc.cache[oldestKey]; ok {
		fsc.currentSize -= int64(len(state.Content))
		delete(fsc.cache, oldestKey)
	}
	fsc.accessOrder = fsc.accessOrder[1:]
}
