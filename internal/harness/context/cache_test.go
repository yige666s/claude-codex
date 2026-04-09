package context

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContextCache(t *testing.T) {
	cache := NewContextCache()

	t.Run("Set and Get", func(t *testing.T) {
		cache.Set("key1", "value1", 0)
		value, exists := cache.Get("key1")
		if !exists {
			t.Error("expected key to exist")
		}
		if value != "value1" {
			t.Errorf("expected 'value1', got %v", value)
		}
	})

	t.Run("Get non-existent key", func(t *testing.T) {
		_, exists := cache.Get("nonexistent")
		if exists {
			t.Error("expected key to not exist")
		}
	})

	t.Run("TTL expiration", func(t *testing.T) {
		cache.Set("ttl_key", "ttl_value", 100*time.Millisecond)

		// Should exist immediately
		value, exists := cache.Get("ttl_key")
		if !exists || value != "ttl_value" {
			t.Error("expected key to exist immediately")
		}

		// Wait for expiration
		time.Sleep(150 * time.Millisecond)

		// Should not exist after TTL
		_, exists = cache.Get("ttl_key")
		if exists {
			t.Error("expected key to be expired")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		cache.Set("delete_key", "delete_value", 0)
		cache.Delete("delete_key")

		_, exists := cache.Get("delete_key")
		if exists {
			t.Error("expected key to be deleted")
		}
	})

	t.Run("Clear", func(t *testing.T) {
		cache.Set("key1", "value1", 0)
		cache.Set("key2", "value2", 0)

		cache.Clear()

		if cache.Size() != 0 {
			t.Errorf("expected cache to be empty, got size %d", cache.Size())
		}
	})

	t.Run("Size", func(t *testing.T) {
		cache.Clear()
		cache.Set("key1", "value1", 0)
		cache.Set("key2", "value2", 0)
		cache.Set("key3", "value3", 0)

		if cache.Size() != 3 {
			t.Errorf("expected size 3, got %d", cache.Size())
		}
	})

	t.Run("Keys", func(t *testing.T) {
		cache.Clear()
		cache.Set("key1", "value1", 0)
		cache.Set("key2", "value2", 0)

		keys := cache.Keys()
		if len(keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(keys))
		}
	})

	t.Run("Invalidate", func(t *testing.T) {
		cache.Clear()
		cache.Set("permanent", "value", 0)
		cache.Set("temporary", "value", 50*time.Millisecond)

		time.Sleep(100 * time.Millisecond)
		cache.Invalidate()

		// Permanent should still exist
		_, exists := cache.Get("permanent")
		if !exists {
			t.Error("expected permanent key to exist")
		}

		// Temporary should be removed
		_, exists = cache.Get("temporary")
		if exists {
			t.Error("expected temporary key to be invalidated")
		}
	})
}

func TestGlobalCache(t *testing.T) {
	cache := GetGlobalCache()
	if cache == nil {
		t.Error("expected global cache to exist")
	}

	cache.Set("test", "value", 0)
	value, exists := cache.Get("test")
	if !exists || value != "value" {
		t.Error("expected to set and get from global cache")
	}

	cache.Clear()
}

func TestClearAllCaches(t *testing.T) {
	// Set up some cached data
	SetSystemPromptInjection("test")
	GetGlobalCache().Set("test", "value", 0)

	// Clear all caches
	ClearAllCaches()

	// Verify global cache is cleared
	if GetGlobalCache().Size() != 0 {
		t.Error("expected global cache to be cleared")
	}

	// Note: SetSystemPromptInjection clears context caches but doesn't reset the injection value itself
	// The injection value is only cleared when explicitly set to ""
	SetSystemPromptInjection("")
}

func TestCollectorOptions(t *testing.T) {
	opts := DefaultCollectorOptions()

	if !opts.IncludeGit {
		t.Error("expected IncludeGit to be true by default")
	}

	if !opts.IncludeClaudeMd {
		t.Error("expected IncludeClaudeMd to be true by default")
	}

	if opts.IncludeDirectoryMap {
		t.Error("expected IncludeDirectoryMap to be false by default")
	}

	if opts.DirectoryDepth != 2 {
		t.Errorf("expected DirectoryDepth to be 2, got %d", opts.DirectoryDepth)
	}

	if opts.MaxStatusChars != MaxStatusChars {
		t.Errorf("expected MaxStatusChars to be %d, got %d", MaxStatusChars, opts.MaxStatusChars)
	}
}

func TestCollectWithOptions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a CLAUDE.md file
	claudeMdPath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMdPath, []byte("# Test Project"), 0644); err != nil {
		t.Fatalf("failed to write CLAUDE.md: %v", err)
	}

	t.Run("Collect with all options enabled", func(t *testing.T) {
		opts := &CollectorOptions{
			IncludeGit:          false, // Skip git for non-git directory
			IncludeClaudeMd:     true,
			IncludeDirectoryMap: true,
			DirectoryDepth:      1,
			MaxStatusChars:      MaxStatusChars,
		}

		ctx := CollectWithOptions(tmpDir, opts)

		if ctx.WorkingDir != tmpDir {
			t.Errorf("expected WorkingDir to be %s, got %s", tmpDir, ctx.WorkingDir)
		}

		if ctx.Platform == "" {
			t.Error("expected Platform to be set")
		}

		if ctx.ClaudeMD == "" {
			t.Error("expected ClaudeMD to be loaded")
		}

		if ctx.DirectoryMap == "" {
			t.Error("expected DirectoryMap to be generated")
		}
	})

	t.Run("Collect with minimal options", func(t *testing.T) {
		opts := &CollectorOptions{
			IncludeGit:          false,
			IncludeClaudeMd:     false,
			IncludeDirectoryMap: false,
			DirectoryDepth:      0,
			MaxStatusChars:      MaxStatusChars,
		}

		ctx := CollectWithOptions(tmpDir, opts)

		if ctx.ClaudeMD != "" {
			t.Error("expected ClaudeMD to be empty")
		}

		if ctx.DirectoryMap != "" {
			t.Error("expected DirectoryMap to be empty")
		}
	})
}

func TestWorkspaceContextToMap(t *testing.T) {
	ctx := WorkspaceContext{
		WorkingDir: "/test/dir",
		Platform:   "darwin",
		OSVersion:  "Darwin 21.0.0",
		Shell:      "zsh",
		GitBranch:  "main",
		GitStatus:  "M file.go",
		ClaudeMD:   "# Test",
		IsGitRepo:  true,
	}

	m := ctx.ToMap()

	if m["workingDir"] != "/test/dir" {
		t.Error("expected workingDir in map")
	}

	if m["platform"] != "darwin" {
		t.Error("expected platform in map")
	}

	if m["osVersion"] != "Darwin 21.0.0" {
		t.Error("expected osVersion in map")
	}

	if m["shell"] != "zsh" {
		t.Error("expected shell in map")
	}

	if m["gitBranch"] != "main" {
		t.Error("expected gitBranch in map")
	}

	if m["gitStatus"] != "M file.go" {
		t.Error("expected gitStatus in map")
	}

	if m["claudeMd"] != "# Test" {
		t.Error("expected claudeMd in map")
	}
}

func TestDetectPlatform(t *testing.T) {
	platform := detectPlatform()
	if platform == "" {
		t.Error("expected platform to be detected")
	}
}

func TestDetectShell(t *testing.T) {
	shell := detectShell()
	// Shell might be empty in some environments, so just check it doesn't panic
	_ = shell
}

func TestDetectOSVersion(t *testing.T) {
	version := detectOSVersion()
	// Version might be empty in some environments, so just check it doesn't panic
	_ = version
}
