package context

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLocalISODate(t *testing.T) {
	date := GetLocalISODate()
	assert.Regexp(t, `^\d{4}/\d{2}/\d{2}$`, date)
}

func TestManager_GetUserContext(t *testing.T) {
	manager := NewManager("/test/project")

	ctx, err := manager.GetUserContext("# Test CLAUDE.md content")
	require.NoError(t, err)
	assert.NotEmpty(t, ctx.CurrentDate)
	assert.Equal(t, "# Test CLAUDE.md content", ctx.ClaudeMd)
}

func TestManager_GetUserContext_Caching(t *testing.T) {
	manager := NewManager("/test/project")

	ctx1, err := manager.GetUserContext("content1")
	require.NoError(t, err)

	// Second call should return cached value
	ctx2, err := manager.GetUserContext("content2")
	require.NoError(t, err)

	assert.Equal(t, ctx1.ClaudeMd, ctx2.ClaudeMd)
	assert.Equal(t, "content1", ctx2.ClaudeMd) // Should still be content1
}

func TestManager_GetSystemContext(t *testing.T) {
	manager := NewManager("/test/project")

	ctx, err := manager.GetSystemContext(false)
	require.NoError(t, err)
	assert.NotNil(t, ctx)
}

func TestManager_GetSystemContext_WithInjection(t *testing.T) {
	manager := NewManager("/test/project")
	manager.SetSystemPromptInjection("test-injection")

	ctx, err := manager.GetSystemContext(false)
	require.NoError(t, err)
	assert.Contains(t, ctx.CacheBreaker, "test-injection")
}

func TestManager_ClearCache(t *testing.T) {
	manager := NewManager("/test/project")

	// Cache user context
	ctx1, err := manager.GetUserContext("content1")
	require.NoError(t, err)
	assert.Equal(t, "content1", ctx1.ClaudeMd)

	// Clear cache
	manager.ClearCache()

	// Should get new content
	ctx2, err := manager.GetUserContext("content2")
	require.NoError(t, err)
	assert.Equal(t, "content2", ctx2.ClaudeMd)
}

func TestManager_GetGitStatus_NotGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	status, err := manager.getGitStatus()
	require.NoError(t, err)
	assert.Empty(t, status)
}

func TestManager_GetGitStatus_GitRepo(t *testing.T) {
	t.Skip("Skipping git integration test - requires actual git repo setup")
}

func TestManager_GetGitStatus_Caching(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	// First call
	status1, _ := manager.getGitStatus()

	// Second call should return cached value
	status2, _ := manager.getGitStatus()

	assert.Equal(t, status1, status2)
}

func TestManager_SetSystemPromptInjection(t *testing.T) {
	manager := NewManager("/test/project")

	manager.SetSystemPromptInjection("test-value")
	assert.Equal(t, "test-value", manager.GetSystemPromptInjection())

	// Should clear cache
	assert.False(t, manager.systemContextCached)
}

