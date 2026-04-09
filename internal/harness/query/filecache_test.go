package query

import (
	"testing"
)

func TestNewFileStateCache(t *testing.T) {
	cache := NewFileStateCache(10, 1024)
	if cache == nil {
		t.Fatal("Expected non-nil cache")
	}

	if cache.Size() != 0 {
		t.Errorf("Expected size 0, got %d", cache.Size())
	}

	if cache.CurrentSizeBytes() != 0 {
		t.Errorf("Expected current size 0, got %d", cache.CurrentSizeBytes())
	}
}

func TestFileStateCache_SetAndGet(t *testing.T) {
	cache := NewFileStateCache(10, 1024)

	state := &FileState{
		Content:   "test content",
		Timestamp: 12345,
	}

	cache.Set("/path/to/file.txt", state)

	retrieved, ok := cache.Get("/path/to/file.txt")
	if !ok {
		t.Fatal("Expected to find cached state")
	}

	if retrieved.Content != state.Content {
		t.Errorf("Expected content %q, got %q", state.Content, retrieved.Content)
	}

	if retrieved.Timestamp != state.Timestamp {
		t.Errorf("Expected timestamp %d, got %d", state.Timestamp, retrieved.Timestamp)
	}
}

func TestFileStateCache_PathNormalization(t *testing.T) {
	cache := NewFileStateCache(10, 1024)

	state := &FileState{
		Content:   "test content",
		Timestamp: 12345,
	}

	// Set with one path
	cache.Set("/path/to/../to/file.txt", state)

	// Get with normalized path
	retrieved, ok := cache.Get("/path/to/file.txt")
	if !ok {
		t.Fatal("Expected to find cached state with normalized path")
	}

	if retrieved.Content != state.Content {
		t.Errorf("Expected content %q, got %q", state.Content, retrieved.Content)
	}
}

func TestFileStateCache_Has(t *testing.T) {
	cache := NewFileStateCache(10, 1024)

	if cache.Has("/path/to/file.txt") {
		t.Error("Expected Has to return false for non-existent key")
	}

	state := &FileState{
		Content:   "test content",
		Timestamp: 12345,
	}
	cache.Set("/path/to/file.txt", state)

	if !cache.Has("/path/to/file.txt") {
		t.Error("Expected Has to return true for existing key")
	}
}

func TestFileStateCache_Delete(t *testing.T) {
	cache := NewFileStateCache(10, 1024)

	state := &FileState{
		Content:   "test content",
		Timestamp: 12345,
	}
	cache.Set("/path/to/file.txt", state)

	if !cache.Delete("/path/to/file.txt") {
		t.Error("Expected Delete to return true")
	}

	if cache.Has("/path/to/file.txt") {
		t.Error("Expected key to be deleted")
	}

	if !cache.Delete("/nonexistent") {
		// Delete of non-existent key should return false
	}
}

func TestFileStateCache_Clear(t *testing.T) {
	cache := NewFileStateCache(10, 1024)

	state1 := &FileState{Content: "content1", Timestamp: 1}
	state2 := &FileState{Content: "content2", Timestamp: 2}

	cache.Set("/file1.txt", state1)
	cache.Set("/file2.txt", state2)

	if cache.Size() != 2 {
		t.Errorf("Expected size 2, got %d", cache.Size())
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", cache.Size())
	}

	if cache.CurrentSizeBytes() != 0 {
		t.Errorf("Expected current size 0 after clear, got %d", cache.CurrentSizeBytes())
	}
}

func TestFileStateCache_LRUEviction(t *testing.T) {
	// Create cache with max 3 entries
	cache := NewFileStateCache(3, 10000)

	state1 := &FileState{Content: "content1", Timestamp: 1}
	state2 := &FileState{Content: "content2", Timestamp: 2}
	state3 := &FileState{Content: "content3", Timestamp: 3}
	state4 := &FileState{Content: "content4", Timestamp: 4}

	cache.Set("/file1.txt", state1)
	cache.Set("/file2.txt", state2)
	cache.Set("/file3.txt", state3)

	if cache.Size() != 3 {
		t.Errorf("Expected size 3, got %d", cache.Size())
	}

	// Adding 4th entry should evict the oldest (file1)
	cache.Set("/file4.txt", state4)

	if cache.Size() != 3 {
		t.Errorf("Expected size 3 after eviction, got %d", cache.Size())
	}

	if cache.Has("/file1.txt") {
		t.Error("Expected file1 to be evicted")
	}

	if !cache.Has("/file2.txt") {
		t.Error("Expected file2 to still be in cache")
	}

	if !cache.Has("/file3.txt") {
		t.Error("Expected file3 to still be in cache")
	}

	if !cache.Has("/file4.txt") {
		t.Error("Expected file4 to be in cache")
	}
}

func TestFileStateCache_SizeEviction(t *testing.T) {
	// Create cache with max size 50 bytes
	cache := NewFileStateCache(10, 50)

	state1 := &FileState{Content: "12345678901234567890", Timestamp: 1} // 20 bytes
	state2 := &FileState{Content: "12345678901234567890", Timestamp: 2} // 20 bytes
	state3 := &FileState{Content: "12345678901234567890", Timestamp: 3} // 20 bytes

	cache.Set("/file1.txt", state1)
	cache.Set("/file2.txt", state2)

	if cache.CurrentSizeBytes() != 40 {
		t.Errorf("Expected current size 40, got %d", cache.CurrentSizeBytes())
	}

	// Adding 3rd entry (20 bytes) should evict file1 to stay under 50 bytes
	cache.Set("/file3.txt", state3)

	if cache.Has("/file1.txt") {
		t.Error("Expected file1 to be evicted due to size limit")
	}

	if !cache.Has("/file2.txt") {
		t.Error("Expected file2 to still be in cache")
	}

	if !cache.Has("/file3.txt") {
		t.Error("Expected file3 to be in cache")
	}
}

func TestFileStateCache_Keys(t *testing.T) {
	cache := NewFileStateCache(10, 1024)

	state1 := &FileState{Content: "content1", Timestamp: 1}
	state2 := &FileState{Content: "content2", Timestamp: 2}

	cache.Set("/file1.txt", state1)
	cache.Set("/file2.txt", state2)

	keys := cache.Keys()
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(keys))
	}

	// Check that both keys are present
	keyMap := make(map[string]bool)
	for _, k := range keys {
		keyMap[k] = true
	}

	if !keyMap["/file1.txt"] {
		t.Error("Expected /file1.txt in keys")
	}

	if !keyMap["/file2.txt"] {
		t.Error("Expected /file2.txt in keys")
	}
}

func TestFileStateCache_Entries(t *testing.T) {
	cache := NewFileStateCache(10, 1024)

	state1 := &FileState{Content: "content1", Timestamp: 1}
	state2 := &FileState{Content: "content2", Timestamp: 2}

	cache.Set("/file1.txt", state1)
	cache.Set("/file2.txt", state2)

	entries := cache.Entries()
	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(entries))
	}

	if entries["/file1.txt"].Content != "content1" {
		t.Error("Expected content1 for file1")
	}

	if entries["/file2.txt"].Content != "content2" {
		t.Error("Expected content2 for file2")
	}
}

func TestFileStateCache_UpdateExisting(t *testing.T) {
	cache := NewFileStateCache(10, 1024)

	state1 := &FileState{Content: "original", Timestamp: 1}
	cache.Set("/file.txt", state1)

	if cache.CurrentSizeBytes() != 8 { // len("original")
		t.Errorf("Expected size 8, got %d", cache.CurrentSizeBytes())
	}

	state2 := &FileState{Content: "updated content", Timestamp: 2}
	cache.Set("/file.txt", state2)

	if cache.Size() != 1 {
		t.Errorf("Expected size 1, got %d", cache.Size())
	}

	if cache.CurrentSizeBytes() != 15 { // len("updated content")
		t.Errorf("Expected size 15, got %d", cache.CurrentSizeBytes())
	}

	retrieved, ok := cache.Get("/file.txt")
	if !ok {
		t.Fatal("Expected to find cached state")
	}

	if retrieved.Content != "updated content" {
		t.Errorf("Expected updated content, got %q", retrieved.Content)
	}
}

func TestFileStateCache_PartialView(t *testing.T) {
	cache := NewFileStateCache(10, 1024)

	state := &FileState{
		Content:       "partial content",
		Timestamp:     12345,
		IsPartialView: true,
	}

	cache.Set("/file.txt", state)

	retrieved, ok := cache.Get("/file.txt")
	if !ok {
		t.Fatal("Expected to find cached state")
	}

	if !retrieved.IsPartialView {
		t.Error("Expected IsPartialView to be true")
	}
}

func TestFileStateCache_OffsetAndLimit(t *testing.T) {
	cache := NewFileStateCache(10, 1024)

	offset := 10
	limit := 100
	state := &FileState{
		Content:   "content",
		Timestamp: 12345,
		Offset:    &offset,
		Limit:     &limit,
	}

	cache.Set("/file.txt", state)

	retrieved, ok := cache.Get("/file.txt")
	if !ok {
		t.Fatal("Expected to find cached state")
	}

	if retrieved.Offset == nil || *retrieved.Offset != 10 {
		t.Error("Expected Offset to be 10")
	}

	if retrieved.Limit == nil || *retrieved.Limit != 100 {
		t.Error("Expected Limit to be 100")
	}
}
