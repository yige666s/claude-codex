package prompt

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewSystemPrompt(t *testing.T) {
	sections := []string{"section1", "section2", "section3"}
	sp := NewSystemPrompt(sections)

	if sp == nil {
		t.Fatal("NewSystemPrompt returned nil")
	}

	if sp.Len() != 3 {
		t.Errorf("expected length 3, got %d", sp.Len())
	}

	// Verify immutability - modifying original should not affect [REDACTED]
	sections[0] = "modified"
	result := sp.Sections()
	if result[0] == "modified" {
		t.Error("[REDACTED] was not properly copied")
	}
}

func TestSystemPromptSections(t *testing.T) {
	sp := NewSystemPrompt([]string{"a", "b", "c"})
	sections := sp.Sections()

	if len(sections) != 3 {
		t.Errorf("expected 3 sections, got %d", len(sections))
	}

	// Verify returned slice is a copy
	sections[0] = "modified"
	if sp.Sections()[0] == "modified" {
		t.Error("Sections() did not return a copy")
	}
}

func TestSystemPromptIsEmpty(t *testing.T) {
	empty := NewSystemPrompt([]string{})
	if !empty.IsEmpty() {
		t.Error("expected IsEmpty() to return true for empty prompt")
	}

	notEmpty := NewSystemPrompt([]string{"content"})
	if notEmpty.IsEmpty() {
		t.Error("expected IsEmpty() to return false for non-empty prompt")
	}
}

func TestNewSection(t *testing.T) {
	compute := func() (string, error) {
		return "test content", nil
	}

	section := NewSection("test", compute)

	if section.Name != "test" {
		t.Errorf("expected name 'test', got '%s'", section.Name)
	}

	if section.CacheBreak {
		t.Error("NewSection should not set CacheBreak to true")
	}

	content, err := section.Compute()
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}

	if content != "test content" {
		t.Errorf("expected 'test content', got '%s'", content)
	}
}

func TestNewUncachedSection(t *testing.T) {
	compute := func() (string, error) {
		return "dynamic content", nil
	}

	section := NewUncachedSection("dynamic", compute, "needs fresh data")

	if !section.CacheBreak {
		t.Error("NewUncachedSection should set CacheBreak to true")
	}
}

func TestBuilderBuildFromSections(t *testing.T) {
	builder := NewBuilder()
	ctx := context.Background()

	callCount := 0
	sections := []*SystemPromptSection{
		NewSection("section1", func() (string, error) {
			callCount++
			return "content1", nil
		}),
		NewSection("section2", func() (string, error) {
			callCount++
			return "content2", nil
		}),
	}

	// First build - should compute both sections
	sp1, err := builder.BuildFromSections(ctx, sections)
	if err != nil {
		t.Fatalf("BuildFromSections failed: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 compute calls, got %d", callCount)
	}

	if sp1.Len() != 2 {
		t.Errorf("expected 2 sections, got %d", sp1.Len())
	}

	// Second build - should use cache
	callCount = 0
	sp2, err := builder.BuildFromSections(ctx, sections)
	if err != nil {
		t.Fatalf("BuildFromSections failed: %v", err)
	}

	if callCount != 0 {
		t.Errorf("expected 0 compute calls (cached), got %d", callCount)
	}

	if sp2.Len() != 2 {
		t.Errorf("expected 2 sections, got %d", sp2.Len())
	}
}

func TestBuilderCacheBreaking(t *testing.T) {
	builder := NewBuilder()
	ctx := context.Background()

	callCount := 0
	sections := []*SystemPromptSection{
		NewUncachedSection("uncached", func() (string, error) {
			callCount++
			return "dynamic", nil
		}, "always fresh"),
	}

	// First build
	_, err := builder.BuildFromSections(ctx, sections)
	if err != nil {
		t.Fatalf("BuildFromSections failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 compute call, got %d", callCount)
	}

	// Second build - should recompute due to CacheBreak
	_, err = builder.BuildFromSections(ctx, sections)
	if err != nil {
		t.Fatalf("BuildFromSections failed: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 compute calls (no cache), got %d", callCount)
	}
}

func TestBuilderErrorHandling(t *testing.T) {
	builder := NewBuilder()
	ctx := context.Background()

	expectedErr := errors.New("compute error")
	sections := []*SystemPromptSection{
		NewSection("failing", func() (string, error) {
			return "", expectedErr
		}),
	}

	_, err := builder.BuildFromSections(ctx, sections)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error to wrap compute error")
	}
}

func TestBuilderEmptySections(t *testing.T) {
	builder := NewBuilder()
	ctx := context.Background()

	sections := []*SystemPromptSection{
		NewSection("empty", func() (string, error) {
			return "", nil
		}),
		NewSection("content", func() (string, error) {
			return "has content", nil
		}),
	}

	sp, err := builder.BuildFromSections(ctx, sections)
	if err != nil {
		t.Fatalf("BuildFromSections failed: %v", err)
	}

	// Empty sections should be filtered out
	if sp.Len() != 1 {
		t.Errorf("expected 1 section (empty filtered), got %d", sp.Len())
	}
}

func TestBuilderClearCache(t *testing.T) {
	builder := NewBuilder()
	ctx := context.Background()

	callCount := 0
	sections := []*SystemPromptSection{
		NewSection("test", func() (string, error) {
			callCount++
			return "content", nil
		}),
	}

	// Build once
	_, err := builder.BuildFromSections(ctx, sections)
	if err != nil {
		t.Fatalf("BuildFromSections failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 compute call, got %d", callCount)
	}

	// Clear cache
	builder.ClearCache()

	// Build again - should recompute
	callCount = 0
	_, err = builder.BuildFromSections(ctx, sections)
	if err != nil {
		t.Fatalf("BuildFromSections failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 compute call after cache clear, got %d", callCount)
	}
}

func TestSectionCache(t *testing.T) {
	cache := NewSectionCache()

	// Test Set and Get
	cache.Set("key1", "value1")
	value, found := cache.Get("key1")
	if !found {
		t.Error("expected to find key1")
	}
	if value != "value1" {
		t.Errorf("expected 'value1', got '%s'", value)
	}

	// Test Has
	if !cache.Has("key1") {
		t.Error("Has() should return true for existing key")
	}
	if cache.Has("nonexistent") {
		t.Error("Has() should return false for non-existent key")
	}

	// Test Size
	cache.Set("key2", "value2")
	if cache.Size() != 2 {
		t.Errorf("expected size 2, got %d", cache.Size())
	}

	// Test Delete
	cache.Delete("key1")
	if cache.Has("key1") {
		t.Error("key1 should be deleted")
	}

	// Test Clear
	cache.Clear()
	if cache.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", cache.Size())
	}
}

func TestSectionCacheStats(t *testing.T) {
	cache := NewSectionCache()

	cache.Set("section1", "content1")
	time.Sleep(10 * time.Millisecond)
	cache.Set("section2", "content2")

	stats := cache.Stats()

	if stats.Size != 2 {
		t.Errorf("expected size 2, got %d", stats.Size)
	}

	if len(stats.SectionNames) != 2 {
		t.Errorf("expected 2 section names, got %d", len(stats.SectionNames))
	}

	if stats.OldestEntry.IsZero() || stats.NewestEntry.IsZero() {
		t.Error("timestamps should not be zero")
	}

	if !stats.NewestEntry.After(stats.OldestEntry) {
		t.Error("newest entry should be after oldest entry")
	}
}

func TestSectionCacheInvalidateOlderThan(t *testing.T) {
	cache := NewSectionCache()

	cache.Set("old", "value1")
	time.Sleep(100 * time.Millisecond)
	cache.Set("new", "value2")

	removed := cache.InvalidateOlderThan(50 * time.Millisecond)

	if removed != 1 {
		t.Errorf("expected 1 removed entry, got %d", removed)
	}

	if cache.Has("old") {
		t.Error("old entry should be removed")
	}

	if !cache.Has("new") {
		t.Error("new entry should still exist")
	}
}

func TestMergePrompts(t *testing.T) {
	sp1 := NewSystemPrompt([]string{"a", "b"})
	sp2 := NewSystemPrompt([]string{"c", "d"})
	sp3 := NewSystemPrompt([]string{"e"})

	merged := MergePrompts(sp1, sp2, sp3)

	if merged.Len() != 5 {
		t.Errorf("expected length 5, got %d", merged.Len())
	}

	sections := merged.Sections()
	expected := []string{"a", "b", "c", "d", "e"}
	for i, exp := range expected {
		if sections[i] != exp {
			t.Errorf("at index %d: expected '%s', got '%s'", i, exp, sections[i])
		}
	}
}

func TestMergePromptsWithNil(t *testing.T) {
	sp1 := NewSystemPrompt([]string{"a"})
	merged := MergePrompts(sp1, nil, nil)

	if merged.Len() != 1 {
		t.Errorf("expected length 1, got %d", merged.Len())
	}
}

func TestPromptContext(t *testing.T) {
	ctx := NewPromptContext()

	if ctx.UserContext == nil {
		t.Error("UserContext should be initialized")
	}

	if ctx.SystemContext == nil {
		t.Error("SystemContext should be initialized")
	}

	if ctx.AdditionalWorkingDirectories == nil {
		t.Error("AdditionalWorkingDirectories should be initialized")
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	builder := NewBuilder()
	ctx := context.Background()

	promptCtx := NewPromptContext()
	promptCtx.CustomSystemPrompt = "custom prompt"
	promptCtx.AppendSystemPrompt = "appended content"

	sp, err := builder.BuildSystemPrompt(ctx, promptCtx)
	if err != nil {
		t.Fatalf("BuildSystemPrompt failed: %v", err)
	}

	sections := sp.Sections()
	if len(sections) != 2 {
		t.Errorf("expected 2 sections, got %d", len(sections))
	}

	if sections[0] != "custom prompt" {
		t.Errorf("expected 'custom prompt', got '%s'", sections[0])
	}

	if sections[1] != "appended content" {
		t.Errorf("expected 'appended content', got '%s'", sections[1])
	}
}

func TestBuildFromStrings(t *testing.T) {
	builder := NewBuilder()
	sections := []string{"section1", "section2"}

	sp := builder.BuildFromStrings(sections)

	if sp.Len() != 2 {
		t.Errorf("expected 2 sections, got %d", sp.Len())
	}

	result := sp.Sections()
	if result[0] != "section1" || result[1] != "section2" {
		t.Error("sections do not match input")
	}
}
