package input

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/public/types"
)

func TestProcessAttachments(t *testing.T) {
	tests := []struct {
		name        string
		opts        *ProcessUserInputOptions
		expectCount int
		expectError bool
	}{
		{
			name: "skip attachments",
			opts: &ProcessUserInputOptions{
				SkipAttachments: true,
			},
			expectCount: 0,
			expectError: false,
		},
		{
			name: "no context",
			opts: &ProcessUserInputOptions{
				SkipAttachments: false,
			},
			expectCount: 0,
			expectError: false,
		},
		{
			name: "with IDE selection",
			opts: &ProcessUserInputOptions{
				SkipAttachments: false,
				Context: &ProcessUserInputContext{
					IDESelection: &IDESelection{
						FilePath:  "test.go",
						StartLine: 1,
						EndLine:   10,
						Content:   "package main\n\nfunc main() {}",
					},
				},
			},
			expectCount: 1,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProcessAttachments(tt.opts)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if len(result) != tt.expectCount {
				t.Errorf("Expected %d attachments, got %d", tt.expectCount, len(result))
			}
		})
	}
}

func TestCreateIDESelectionAttachment(t *testing.T) {
	tests := []struct {
		name      string
		selection *IDESelection
		expectNil bool
	}{
		{
			name:      "nil selection",
			selection: nil,
			expectNil: true,
		},
		{
			name: "empty file path",
			selection: &IDESelection{
				FilePath: "",
			},
			expectNil: true,
		},
		{
			name: "with content",
			selection: &IDESelection{
				FilePath:  "test.go",
				StartLine: 1,
				EndLine:   5,
				Content:   "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}",
			},
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := createIDESelectionAttachment(tt.selection)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if tt.expectNil && result != nil {
				t.Error("Expected nil result")
			}
			if !tt.expectNil && result == nil {
				t.Error("Expected non-nil result")
			}
			if result != nil {
				if result.Attachment.Type != AttachmentTypeIDESelection {
					t.Errorf("Expected type %s, got %s", AttachmentTypeIDESelection, result.Attachment.Type)
				}
				if len(result.Content) == 0 {
					t.Error("Expected content blocks")
				}
			}
		})
	}
}

func TestCreateFileAttachment(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello, World!"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name        string
		filePath    string
		expectError bool
		expectType  AttachmentType
	}{
		{
			name:        "valid file",
			filePath:    testFile,
			expectError: false,
			expectType:  AttachmentTypeFile,
		},
		{
			name:        "directory",
			filePath:    tmpDir,
			expectError: false,
			expectType:  AttachmentTypeDirectory,
		},
		{
			name:        "non-existent file",
			filePath:    filepath.Join(tmpDir, "nonexistent.txt"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CreateFileAttachment(tt.filePath)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tt.expectError && result != nil {
				if result.Attachment.Type != tt.expectType {
					t.Errorf("Expected type %s, got %s", tt.expectType, result.Attachment.Type)
				}
			}
		})
	}
}

func TestCreateAgentMentionAttachment(t *testing.T) {
	agentType := "test-agent"
	result := CreateAgentMentionAttachment(agentType)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Attachment.Type != AttachmentTypeAgentMention {
		t.Errorf("Expected type %s, got %s", AttachmentTypeAgentMention, result.Attachment.Type)
	}

	if len(result.Content) == 0 {
		t.Error("Expected content blocks")
	}

	// Check that agent type is in the data
	if data, ok := result.Attachment.Data["agentType"]; !ok || data != agentType {
		t.Errorf("Expected agentType %s in data", agentType)
	}
}

func TestParseAttachmentReferences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "no references",
			input:    "Hello world",
			expected: nil,
		},
		{
			name:     "single file reference",
			input:    "@file:test.go",
			expected: []string{"test.go"},
		},
		{
			name:     "multiple references",
			input:    "@file:test.go\n@file:main.go",
			expected: []string{"test.go", "main.go"},
		},
		{
			name:     "with other text",
			input:    "Check this file:\n@file:test.go\nAnd this one:\n@file:main.go",
			expected: []string{"test.go", "main.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseAttachmentReferences(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d references, got %d", len(tt.expected), len(result))
				return
			}
			for i, ref := range result {
				if ref != tt.expected[i] {
					t.Errorf("Expected reference %s at position %d, got %s", tt.expected[i], i, ref)
				}
			}
		})
	}
}

func TestProcessUserInputWithAttachments(t *testing.T) {
	ctx := context.Background()

	opts := &ProcessUserInputOptions{
		Input: "Test message",
		Mode:  "prompt",
		Context: &ProcessUserInputContext{
			WorkingDir: "/test",
			SessionID:  "test-session",
			IDESelection: &IDESelection{
				FilePath:  "test.go",
				StartLine: 1,
				EndLine:   5,
				Content:   "package main",
			},
		},
		UUID: "test-uuid",
	}

	result, err := ProcessUserInput(ctx, opts)
	if err != nil {
		t.Fatalf("ProcessUserInput failed: %v", err)
	}

	if !result.ShouldQuery {
		t.Error("Expected ShouldQuery to be true")
	}

	// Should have at least user message and attachment message
	if len(result.Messages) < 2 {
		t.Errorf("Expected at least 2 messages, got %d", len(result.Messages))
	}
}

func TestProcessUserInputWithImages(t *testing.T) {
	ctx := context.Background()

	// Create test image data (PNG signature)
	imageData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

	opts := &ProcessUserInputOptions{
		Input: "Check this image:\n[Pasted image #1]",
		Mode:  "prompt",
		Context: &ProcessUserInputContext{
			WorkingDir: "/test",
			SessionID:  "test-session",
			PastedContents: map[int]PastedContent{
				1: {
					Type: "image",
					Data: imageData,
				},
			},
		},
		UUID: "test-uuid",
	}

	result, err := ProcessUserInput(ctx, opts)
	if err != nil {
		t.Fatalf("ProcessUserInput failed: %v", err)
	}

	if !result.ShouldQuery {
		t.Error("Expected ShouldQuery to be true")
	}

	// Should have user message with image content
	if len(result.Messages) == 0 {
		t.Fatal("Expected at least one message")
	}

	userMsg := result.Messages[0]
	if userMsg.Type != types.MessageTypeUser {
		t.Errorf("Expected user message, got %s", userMsg.Type)
	}

	// Check for image content block
	hasImage := false
	for _, block := range userMsg.Content {
		if block.Type == "image" {
			hasImage = true
			break
		}
	}
	if !hasImage {
		t.Error("Expected image content block in user message")
	}
}
