package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_AddToHistory(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, "session-1", "/test/project")
	defer manager.Close()

	entry := &HistoryEntry{
		Display: "test command",
	}

	manager.AddToHistory(entry)

	history, err := manager.GetHistory(10)
	require.NoError(t, err)
	assert.Len(t, history, 1)
	assert.Equal(t, "test command", history[0].Display)
}

func TestManager_GetHistory_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, "session-1", "/test/project")
	defer manager.Close()

	// Add duplicate entries
	manager.AddToHistory(&HistoryEntry{Display: "command 1"})
	manager.AddToHistory(&HistoryEntry{Display: "command 2"})
	manager.AddToHistory(&HistoryEntry{Display: "command 1"}) // duplicate

	history, err := manager.GetHistory(10)
	require.NoError(t, err)
	assert.Len(t, history, 2) // Should be deduplicated
}

func TestManager_RemoveLastFromHistory(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, "session-1", "/test/project")
	defer manager.Close()

	manager.AddToHistory(&HistoryEntry{Display: "command 1"})
	manager.AddToHistory(&HistoryEntry{Display: "command 2"})

	manager.RemoveLastFromHistory()

	history, err := manager.GetHistory(10)
	require.NoError(t, err)
	assert.Len(t, history, 1)
	assert.Equal(t, "command 1", history[0].Display)
}

func TestManager_Flush(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, "session-1", "/test/project")

	manager.AddToHistory(&HistoryEntry{Display: "command 1"})
	manager.AddToHistory(&HistoryEntry{Display: "command 2"})

	err := manager.Flush()
	require.NoError(t, err)

	// Verify file was created
	historyPath := filepath.Join(tmpDir, historyFileName)
	_, err = os.Stat(historyPath)
	require.NoError(t, err)

	manager.Close()

	// Create new manager to read from disk
	manager2 := NewManager(tmpDir, "session-2", "/test/project")
	defer manager2.Close()

	history, err := manager2.GetHistory(10)
	require.NoError(t, err)
	assert.Len(t, history, 2)
}

func TestManager_GetHistory_ProjectFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	manager1 := NewManager(tmpDir, "session-1", "/project1")
	manager2 := NewManager(tmpDir, "session-2", "/project2")

	manager1.AddToHistory(&HistoryEntry{Display: "project1 command"})
	manager2.AddToHistory(&HistoryEntry{Display: "project2 command"})

	manager1.Flush()
	manager2.Flush()

	manager1.Close()
	manager2.Close()

	// Read project1 history
	manager3 := NewManager(tmpDir, "session-3", "/project1")
	defer manager3.Close()
	history, err := manager3.GetHistory(10)
	require.NoError(t, err)
	assert.Len(t, history, 1)
	assert.Equal(t, "project1 command", history[0].Display)
}

func TestManager_PastedContent(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, "session-1", "/test/project")
	defer manager.Close()

	entry := &HistoryEntry{
		Display: "command with paste",
		PastedContents: map[int]*PastedContent{
			1: {
				ID:      1,
				Type:    PastedContentTypeText,
				Content: "pasted text",
			},
		},
	}

	manager.AddToHistory(entry)

	history, err := manager.GetHistory(10)
	require.NoError(t, err)
	assert.Len(t, history, 1)
	assert.NotNil(t, history[0].PastedContents)
	assert.Equal(t, "pasted text", history[0].PastedContents[1].Content)
}

func TestFormatPastedTextRef(t *testing.T) {
	tests := []struct {
		id       int
		numLines int
		expected string
	}{
		{1, 0, "[Pasted text #1]"},
		{2, 5, "[Pasted text #2 +5 lines]"},
		{10, 100, "[Pasted text #10 +100 lines]"},
	}

	for _, tt := range tests {
		result := FormatPastedTextRef(tt.id, tt.numLines)
		assert.Equal(t, tt.expected, result)
	}
}

func TestFormatImageRef(t *testing.T) {
	assert.Equal(t, "[Image #1]", FormatImageRef(1))
	assert.Equal(t, "[Image #42]", FormatImageRef(42))
}

func TestGetPastedTextRefNumLines(t *testing.T) {
	tests := []struct {
		text     string
		expected int
	}{
		{"single line", 0},
		{"line1\nline2", 1},
		{"line1\nline2\nline3", 2},
		{"", 0},
	}

	for _, tt := range tests {
		result := GetPastedTextRefNumLines(tt.text)
		assert.Equal(t, tt.expected, result)
	}
}

func TestExpandPastedTextRefs(t *testing.T) {
	pastedContents := map[int]*PastedContent{
		1: {
			ID:      1,
			Type:    PastedContentTypeText,
			Content: "actual content",
		},
		2: {
			ID:       2,
			Type:     PastedContentTypeImage,
			Filename: "image.png",
		},
	}

	input := "Here is [Pasted text #1] and [Image #2]"
	result := ExpandPastedTextRefs(input, pastedContents)

	assert.Contains(t, result, "actual content")
	assert.Contains(t, result, "[Image #2]") // Images not expanded
}

func TestManager_ClearPendingEntries(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, "session-1", "/test/project")
	defer manager.Close()

	manager.AddToHistory(&HistoryEntry{Display: "command 1"})
	manager.AddToHistory(&HistoryEntry{Display: "command 2"})

	manager.ClearPendingEntries()

	history, err := manager.GetHistory(10)
	require.NoError(t, err)
	assert.Len(t, history, 0)
}

func TestManager_AutoFlush(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, "session-1", "/test/project")

	manager.AddToHistory(&HistoryEntry{Display: "command 1"})

	// Wait for auto-flush
	time.Sleep(6 * time.Second)

	historyPath := filepath.Join(tmpDir, historyFileName)
	_, err := os.Stat(historyPath)
	require.NoError(t, err)

	manager.Close()
}
