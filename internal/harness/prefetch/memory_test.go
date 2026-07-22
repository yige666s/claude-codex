package prefetch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewMemoryPrefetcher(t *testing.T) {
	prefetcher := NewMemoryPrefetcher(nil)
	if prefetcher == nil {
		t.Fatal("Expected non-nil prefetcher")
	}
	if prefetcher.config == nil {
		t.Error("Expected non-nil config")
	}
}

func TestNewMemoryPrefetcher_CustomConfig(t *testing.T) {
	config := &PrefetchConfig{
		MaxSessionBytes: 50000,
		Enabled:         true,
		Timeout:         10 * time.Second,
	}
	prefetcher := NewMemoryPrefetcher(config)
	if prefetcher.config.MaxSessionBytes != 50000 {
		t.Errorf("Expected MaxSessionBytes 50000, got %d", prefetcher.config.MaxSessionBytes)
	}
}

func TestStartRelevantMemoryPrefetch_Disabled(t *testing.T) {
	config := &PrefetchConfig{
		Enabled: false,
	}
	prefetcher := NewMemoryPrefetcher(config)

	messages := []Message{
		{Type: "user", Text: "test message"},
	}

	result := prefetcher.StartRelevantMemoryPrefetch(
		context.Background(),
		messages,
		t.TempDir(),
		0,
		make(map[string]bool),
	)

	if result != nil {
		t.Error("Expected nil result when disabled")
	}
}

func TestStartRelevantMemoryPrefetch_NoUserMessage(t *testing.T) {
	prefetcher := NewMemoryPrefetcher(nil)

	messages := []Message{
		{Type: "assistant", Text: "assistant message"},
	}

	result := prefetcher.StartRelevantMemoryPrefetch(
		context.Background(),
		messages,
		t.TempDir(),
		0,
		make(map[string]bool),
	)

	if result != nil {
		t.Error("Expected nil result when no user message")
	}
}

func TestStartRelevantMemoryPrefetch_SingleWord(t *testing.T) {
	prefetcher := NewMemoryPrefetcher(nil)

	messages := []Message{
		{Type: "user", Text: "hello"},
	}

	result := prefetcher.StartRelevantMemoryPrefetch(
		context.Background(),
		messages,
		t.TempDir(),
		0,
		make(map[string]bool),
	)

	if result == nil {
		t.Fatal("expected a meaningful single-word prompt to start prefetch")
	}
	<-result.ResultChan
}

func TestStartRelevantMemoryPrefetch_MaxBytesExceeded(t *testing.T) {
	config := &PrefetchConfig{
		MaxSessionBytes: 1000,
		Enabled:         true,
	}
	prefetcher := NewMemoryPrefetcher(config)

	messages := []Message{
		{Type: "user", Text: "test message here"},
	}

	result := prefetcher.StartRelevantMemoryPrefetch(
		context.Background(),
		messages,
		t.TempDir(),
		2000, // Exceeds max
		make(map[string]bool),
	)

	if result != nil {
		t.Error("Expected nil result when max bytes exceeded")
	}
}

func TestStartRelevantMemoryPrefetch_Success(t *testing.T) {
	prefetcher := NewMemoryPrefetcher(nil)

	messages := []Message{
		{Type: "user", Text: "test message with multiple words"},
	}

	result := prefetcher.StartRelevantMemoryPrefetch(
		context.Background(),
		messages,
		t.TempDir(),
		0,
		make(map[string]bool),
	)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.ResultChan == nil {
		t.Error("Expected non-nil result channel")
	}

	if result.SettledAt() != nil {
		t.Error("Expected nil SettledAt initially")
	}

	if result.ConsumedOnIteration() != -1 {
		t.Errorf("Expected ConsumedOnIteration -1, got %d", result.ConsumedOnIteration())
	}

	// Wait for result
	select {
	case <-result.ResultChan:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for result")
	}

	// Check settled
	if result.SettledAt() == nil {
		t.Error("Expected SettledAt to be set after completion")
	}
}

func TestStartRelevantMemoryPrefetch_ReturnsMatchingMemoryContent(t *testing.T) {
	memoryDir := t.TempDir()
	relevantPath := filepath.Join(memoryDir, "database.md")
	if err := os.WriteFile(relevantPath, []byte("---\ndescription: database rollback procedure\ntype: runbook\n---\nUse the rollback command."), 0o600); err != nil {
		t.Fatalf("write relevant memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "frontend.md"), []byte("---\ndescription: css color palette\n---\nUse blue."), 0o600); err != nil {
		t.Fatalf("write unrelated memory: %v", err)
	}

	prefetcher := NewMemoryPrefetcher(nil)
	result := prefetcher.StartRelevantMemoryPrefetch(context.Background(), []Message{{Type: "user", Text: "database rollback"}}, memoryDir, 0, nil)
	if result == nil {
		t.Fatal("expected memory prefetch")
	}
	attachments := <-result.ResultChan
	if len(attachments) != 1 {
		t.Fatalf("expected one relevant memory, got %#v", attachments)
	}
	if attachments[0].Path != relevantPath || attachments[0].Content == "" {
		t.Fatalf("unexpected memory attachment: %#v", attachments[0])
	}
}

func TestMemoryPrefetch_IsSettled(t *testing.T) {
	now := time.Now()
	prefetch := &MemoryPrefetch{
		settledAt: &now,
	}

	if !prefetch.IsSettled() {
		t.Error("Expected IsSettled to return true")
	}

	prefetch2 := &MemoryPrefetch{
		settledAt: nil,
	}

	if prefetch2.IsSettled() {
		t.Error("Expected IsSettled to return false")
	}
}

func TestMemoryPrefetch_IsConsumed(t *testing.T) {
	prefetch := &MemoryPrefetch{
		consumedOnIteration: 0,
	}

	if !prefetch.IsConsumed() {
		t.Error("Expected IsConsumed to return true")
	}

	prefetch2 := &MemoryPrefetch{
		consumedOnIteration: -1,
	}

	if prefetch2.IsConsumed() {
		t.Error("Expected IsConsumed to return false")
	}
}

func TestMemoryPrefetch_Dispose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	prefetch := &MemoryPrefetch{
		cancel:  cancel,
		firedAt: time.Now(),
	}

	// Should not panic
	prefetch.Dispose()

	// Context should be cancelled
	select {
	case <-ctx.Done():
		// Success
	default:
		t.Error("Expected context to be cancelled")
	}
}

func TestFindLastUserMessage(t *testing.T) {
	messages := []Message{
		{Type: "user", Text: "first"},
		{Type: "assistant", Text: "response"},
		{Type: "user", Text: "second"},
		{Type: "user", IsMeta: true, Text: "meta"},
	}

	result := findLastUserMessage(messages)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Text != "second" {
		t.Errorf("Expected 'second', got '%s'", result.Text)
	}
}

func TestExtractSearchTerms(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"hello world test", 3},
		{"the quick brown fox", 3},   // "the" is filtered
		{"a b c", 0},                 // All too short
		{"testing one two three", 3}, // "one" and "two" are short but >= 3
	}

	for _, tt := range tests {
		terms := extractSearchTerms(tt.input)
		if len(terms) != tt.expected {
			t.Errorf("For input '%s', expected %d terms, got %d: %v",
				tt.input, tt.expected, len(terms), terms)
		}
	}
}

func TestFilterDuplicateMemoryAttachments(t *testing.T) {
	attachments := []MemoryAttachment{
		{Path: "/path/1", Content: "content1"},
		{Path: "/path/2", Content: "content2"},
		{Path: "/path/3", Content: "content3"},
	}

	readFileState := map[string]bool{
		"/path/2": true,
	}

	filtered := FilterDuplicateMemoryAttachments(attachments, readFileState)
	if len(filtered) != 2 {
		t.Errorf("Expected 2 filtered attachments, got %d", len(filtered))
	}

	for _, att := range filtered {
		if att.Path == "/path/2" {
			t.Error("Expected /path/2 to be filtered out")
		}
	}
}

func TestCreateMemoryAttachmentMessage(t *testing.T) {
	att := MemoryAttachment{
		Path:    "/test/memory.md",
		Content: "test content",
		MtimeMs: time.Now().Add(-1 * time.Hour).UnixMilli(),
	}

	msg := CreateMemoryAttachmentMessage(att)
	if msg.Type != "user" {
		t.Errorf("Expected type 'user', got '%s'", msg.Type)
	}
	if !msg.IsMeta {
		t.Error("Expected IsMeta to be true")
	}
	if msg.Text == "" {
		t.Error("Expected non-empty text")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		contains string
	}{
		{30 * time.Second, "just now"},
		{2 * time.Minute, "2 minutes"},
		{1 * time.Hour, "1 hour"},
		{3 * time.Hour, "3 hours"},
		{25 * time.Hour, "1 day"},
		{48 * time.Hour, "2 days"},
	}

	for _, tt := range tests {
		result := formatDuration(tt.duration)
		if result != tt.contains {
			t.Errorf("For duration %v, expected '%s', got '%s'",
				tt.duration, tt.contains, result)
		}
	}
}
