package memdir

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryAgeDays(t *testing.T) {
	now := time.Now().UnixMilli()

	tests := []struct {
		name     string
		mtimeMs  int64
		expected int
	}{
		{"today", now, 0},
		{"yesterday", now - 86400000, 1},
		{"2 days ago", now - 2*86400000, 2},
		{"future (clock skew)", now + 86400000, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MemoryAgeDays(tt.mtimeMs)
			if result != tt.expected {
				t.Errorf("MemoryAgeDays(%d) = %d, want %d", tt.mtimeMs, result, tt.expected)
			}
		})
	}
}

func TestMemoryAge(t *testing.T) {
	now := time.Now().UnixMilli()

	tests := []struct {
		name     string
		mtimeMs  int64
		expected string
	}{
		{"today", now, "today"},
		{"yesterday", now - 86400000, "yesterday"},
		{"5 days ago", now - 5*86400000, "5 days ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MemoryAge(tt.mtimeMs)
			if result != tt.expected {
				t.Errorf("MemoryAge(%d) = %s, want %s", tt.mtimeMs, result, tt.expected)
			}
		})
	}
}

func TestMemoryFreshnessText(t *testing.T) {
	now := time.Now().UnixMilli()

	tests := []struct {
		name     string
		mtimeMs  int64
		wantText bool
	}{
		{"today - no warning", now, false},
		{"yesterday - no warning", now - 86400000, false},
		{"2 days ago - warning", now - 2*86400000, true},
		{"10 days ago - warning", now - 10*86400000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MemoryFreshnessText(tt.mtimeMs)
			hasText := result != ""
			if hasText != tt.wantText {
				t.Errorf("MemoryFreshnessText(%d) hasText=%v, want %v", tt.mtimeMs, hasText, tt.wantText)
			}
			if hasText && !strings.Contains(result, "days old") {
				t.Errorf("Expected warning to contain 'days old', got: %s", result)
			}
		})
	}
}

func TestIsAutoMemoryEnabled(t *testing.T) {
	// Save original env
	origDisable := os.Getenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY")
	origSimple := os.Getenv("CLAUDE_CODE_SIMPLE")
	defer func() {
		os.Setenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY", origDisable)
		os.Setenv("CLAUDE_CODE_SIMPLE", origSimple)
	}()

	tests := []struct {
		name            string
		disableAutoMem  string
		simple          string
		expectedEnabled bool
	}{
		{"default enabled", "", "", true},
		{"explicitly disabled", "1", "", false},
		{"explicitly enabled", "0", "", true},
		{"simple mode", "", "1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY", tt.disableAutoMem)
			os.Setenv("CLAUDE_CODE_SIMPLE", tt.simple)

			result := IsAutoMemoryEnabled()
			if result != tt.expectedEnabled {
				t.Errorf("IsAutoMemoryEnabled() = %v, want %v", result, tt.expectedEnabled)
			}
		})
	}
}

func TestValidateMemoryPath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		expandTilde bool
		wantValid   bool
	}{
		{"empty path", "", false, false},
		{"absolute path", "/tmp/memory", false, true},
		{"relative path", "relative/path", false, false},
		{"root path", "/", false, false},
		{"short path", "/a", false, false},
		{"null byte", "/tmp/mem\x00ory", false, false},
		{"UNC path", "\\\\server\\share", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, valid := ValidateMemoryPath(tt.path, tt.expandTilde)
			if valid != tt.wantValid {
				t.Errorf("ValidateMemoryPath(%q) valid=%v, want %v", tt.path, valid, tt.wantValid)
			}
		})
	}
}

func TestSanitizePathKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid key", "memory.md", false},
		{"valid nested", "user/profile.md", false},
		{"null byte", "mem\x00ory.md", true},
		{"backslash", "mem\\ory.md", true},
		{"absolute path", "/etc/passwd", true},
		{"url encoded traversal", "..%2f..%2fetc%2fpasswd", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SanitizePathKey(tt.key)
			hasErr := err != nil
			if hasErr != tt.wantErr {
				t.Errorf("SanitizePathKey(%q) error=%v, want error=%v", tt.key, hasErr, tt.wantErr)
			}
		})
	}
}

func TestTruncateEntrypointContent(t *testing.T) {
	tests := []struct {
		name             string
		content          string
		expectTruncation bool
	}{
		{
			name:             "short content",
			content:          "# Memory\n\nSome content",
			expectTruncation: false,
		},
		{
			name:             "too many lines",
			content:          strings.Repeat("line\n", MaxEntrypointLines+10),
			expectTruncation: true,
		},
		{
			name:             "too many bytes",
			content:          strings.Repeat("x", MaxEntrypointBytes+1000),
			expectTruncation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateEntrypointContent(tt.content)

			wasTruncated := result.WasLineTruncated || result.WasByteTruncated
			if wasTruncated != tt.expectTruncation {
				t.Errorf("Expected truncation=%v, got %v", tt.expectTruncation, wasTruncated)
			}

			if wasTruncated && !strings.Contains(result.Content, "WARNING") {
				t.Error("Expected warning message in truncated content")
			}

			// Verify limits are enforced
			lines := strings.Split(result.Content, "\n")
			if len(lines) > MaxEntrypointLines+5 { // +5 for warning
				t.Errorf("Content has %d lines, exceeds limit", len(lines))
			}
		})
	}
}

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantName    string
		wantType    string
		wantErr     bool
	}{
		{
			name: "valid frontmatter",
			content: `---
name: test memory
description: A test memory
type: user
---

Content here`,
			wantName: "test memory",
			wantType: "user",
			wantErr:  false,
		},
		{
			name:    "no frontmatter",
			content: "Just content",
			wantErr: true,
		},
		{
			name: "unclosed frontmatter",
			content: `---
name: test
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, err := parseFrontmatter(tt.content)
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Errorf("parseFrontmatter() error=%v, want error=%v", hasErr, tt.wantErr)
			}

			if !hasErr {
				if fm.Name != tt.wantName {
					t.Errorf("Name = %q, want %q", fm.Name, tt.wantName)
				}
				if fm.Type != tt.wantType {
					t.Errorf("Type = %q, want %q", fm.Type, tt.wantType)
				}
			}
		})
	}
}

func TestScanMemoryFiles(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create test memory files
	files := []struct {
		name    string
		content string
	}{
		{
			name: "user_profile.md",
			content: `---
name: User Profile
description: User information
type: user
---

User is a senior engineer`,
		},
		{
			name: "feedback_testing.md",
			content: `---
name: Testing Feedback
description: Testing preferences
type: feedback
---

Always write tests first`,
		},
		{
			name: "MEMORY.md",
			content: "- [User Profile](user_profile.md)\n- [Testing](feedback_testing.md)",
		},
	}

	for _, f := range files {
		path := filepath.Join(tmpDir, f.name)
		if err := os.WriteFile(path, []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Scan files
	ctx := context.Background()
	headers, err := ScanMemoryFiles(tmpDir, ctx)
	if err != nil {
		t.Fatalf("ScanMemoryFiles() error: %v", err)
	}

	// Should find 2 files (excluding MEMORY.md)
	if len(headers) != 2 {
		t.Errorf("Found %d files, want 2", len(headers))
	}

	// Check that files have descriptions
	for _, h := range headers {
		if h.Description == "" {
			t.Errorf("File %s has no description", h.Filename)
		}
		if h.Type == "" {
			t.Errorf("File %s has no type", h.Filename)
		}
	}
}

func TestFormatMemoryManifest(t *testing.T) {
	now := time.Now().UnixMilli()

	headers := []MemoryHeader{
		{
			Filename:    "user.md",
			FilePath:    "/tmp/user.md",
			MtimeMs:     now,
			Description: "User profile",
			Type:        "user",
		},
		{
			Filename:    "feedback.md",
			FilePath:    "/tmp/feedback.md",
			MtimeMs:     now - 86400000,
			Description: "Testing feedback",
			Type:        "feedback",
		},
	}

	manifest := FormatMemoryManifest(headers)

	// Check format
	if !strings.Contains(manifest, "[user]") {
		t.Error("Manifest should contain [user] tag")
	}
	if !strings.Contains(manifest, "[feedback]") {
		t.Error("Manifest should contain [feedback] tag")
	}
	if !strings.Contains(manifest, "User profile") {
		t.Error("Manifest should contain description")
	}

	lines := strings.Split(manifest, "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 lines, got %d", len(lines))
	}
}

func TestBuildMemoryPrompt(t *testing.T) {
	// Save original env
	origDisable := os.Getenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY")
	defer os.Setenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY", origDisable)

	os.Setenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY", "0")

	tmpDir := t.TempDir()
	prompt, err := BuildMemoryPrompt(tmpDir, nil, false)
	if err != nil {
		t.Fatalf("BuildMemoryPrompt() error: %v", err)
	}

	// Check key sections
	if !strings.Contains(prompt, "auto memory") {
		t.Error("Prompt should contain 'auto memory'")
	}
	if !strings.Contains(prompt, "Types of memory") {
		t.Error("Prompt should contain types section")
	}
	if !strings.Contains(prompt, "How to save memories") {
		t.Error("Prompt should contain save instructions")
	}
	if !strings.Contains(prompt, "Before recommending from memory") {
		t.Error("Prompt should contain trusting recall section")
	}
}

func TestGetAutoMemPath(t *testing.T) {
	projectRoot := "/home/user/project"
	path := GetAutoMemPath(projectRoot)

	if !strings.Contains(path, "memory") {
		t.Errorf("Path should contain 'memory', got: %s", path)
	}
	if !strings.HasSuffix(path, string(filepath.Separator)) {
		t.Error("Path should end with separator")
	}
}

func TestGetTeamMemPath(t *testing.T) {
	projectRoot := "/home/user/project"
	path := GetTeamMemPath(projectRoot)

	if !strings.Contains(path, "team") {
		t.Errorf("Path should contain 'team', got: %s", path)
	}
	if !strings.HasSuffix(path, string(filepath.Separator)) {
		t.Error("Path should end with separator")
	}
}

func TestIsAutoMemPath(t *testing.T) {
	projectRoot := "/home/user/project"
	autoMemPath := GetAutoMemPath(projectRoot)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"inside auto mem", filepath.Join(autoMemPath, "user.md"), true},
		{"outside auto mem", "/tmp/other.md", false},
		{"parent of auto mem", filepath.Dir(strings.TrimSuffix(autoMemPath, string(filepath.Separator))), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAutoMemPath(tt.path, projectRoot)
			if result != tt.expected {
				t.Errorf("IsAutoMemPath(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}
