package securestorage

import (
	"context"
	"sync"
	"time"
)

type cacheEntry struct {
	data     Data
	cachedAt time.Time
}

const KeychainCacheTTL = 30 * time.Second

var (
	keychainCacheMu sync.RWMutex
	keychainCache   = map[string]cacheEntry{}

	prefetchMu    sync.Mutex
	prefetchTasks = map[string]chan struct{}{}
)

func cacheKeyForStore(serviceName, accountName string) string {
	return serviceName + "\x00" + accountName
}

func cacheKeyForKeychainStore(store *KeychainStore) string {
	if store == nil {
		return ""
	}
	return cacheKeyForStore(store.serviceName, store.accountName)
}

func getCachedKeychainData(key string) (Data, bool) {
	keychainCacheMu.RLock()
	defer keychainCacheMu.RUnlock()
	entry, ok := keychainCache[key]
	if !ok || time.Since(entry.cachedAt) > KeychainCacheTTL {
		return nil, false
	}
	return cloneData(entry.data), true
}

func setCachedKeychainData(key string, data Data) {
	keychainCacheMu.Lock()
	defer keychainCacheMu.Unlock()
	keychainCache[key] = cacheEntry{data: cloneData(data), cachedAt: time.Now()}
}

func ClearKeychainCache() {
	keychainCacheMu.Lock()
	defer keychainCacheMu.Unlock()
	keychainCache = map[string]cacheEntry{}
}

func StartKeychainPrefetch(store Store) {
	keychainStore, ok := store.(*KeychainStore)
	if !ok || keychainStore == nil {
		return
	}
	key := cacheKeyForKeychainStore(keychainStore)

	prefetchMu.Lock()
	if _, exists := prefetchTasks[key]; exists {
		prefetchMu.Unlock()
		return
	}
	done := make(chan struct{})
	prefetchTasks[key] = done
	prefetchMu.Unlock()

	go func() {
		defer func() {
			prefetchMu.Lock()
			delete(prefetchTasks, key)
			close(done)
			prefetchMu.Unlock()
		}()
		if _, ok := getCachedKeychainData(key); ok {
			return
		}
		data, err := keychainStore.readUncached()
		if err == nil {
			setCachedKeychainData(key, data)
		}
	}()
}

func EnsureKeychainPrefetchCompleted(ctx context.Context, store Store) error {
	keychainStore, ok := store.(*KeychainStore)
	if !ok || keychainStore == nil {
		return nil
	}
	key := cacheKeyForKeychainStore(keychainStore)
	prefetchMu.Lock()
	done := prefetchTasks[key]
	prefetchMu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func cloneData(data Data) Data {
	if data == nil {
		return nil
	}
	out := Data{}
	for k, v := range data {
		out[k] = deepCloneValue(v)
	}
	return out
}

func deepCloneValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range v {
			out[k] = deepCloneValue(item)
		}
		return out
	case Data:
		return cloneData(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = deepCloneValue(v[i])
		}
		return out
	default:
		return v
	}
}
