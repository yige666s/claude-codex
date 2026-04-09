package input

import (
	"testing"
)

func TestDetectMediaType(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name:     "PNG signature",
			data:     []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			expected: "image/png",
		},
		{
			name:     "JPEG signature",
			data:     []byte{0xFF, 0xD8, 0xFF, 0xE0},
			expected: "image/jpeg",
		},
		{
			name:     "GIF signature",
			data:     []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61},
			expected: "image/gif",
		},
		{
			name:     "WebP signature",
			data:     []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50},
			expected: "image/webp",
		},
		{
			name:     "unknown format",
			data:     []byte{0x00, 0x00, 0x00, 0x00},
			expected: "image/png",
		},
		{
			name:     "too short",
			data:     []byte{0x00},
			expected: "image/png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectMediaType(tt.data)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestProcessImageContent(t *testing.T) {
	tests := []struct {
		name           string
		pastedContents map[int]PastedContent
		expectBlocks   int
		expectIDs      int
	}{
		{
			name:           "nil contents",
			pastedContents: nil,
			expectBlocks:   0,
			expectIDs:      0,
		},
		{
			name:           "empty contents",
			pastedContents: map[int]PastedContent{},
			expectBlocks:   0,
			expectIDs:      0,
		},
		{
			name: "single image",
			pastedContents: map[int]PastedContent{
				1: {
					Type: "image",
					Data: []byte{0x89, 0x50, 0x4E, 0x47},
				},
			},
			expectBlocks: 1,
			expectIDs:    1,
		},
		{
			name: "multiple images",
			pastedContents: map[int]PastedContent{
				1: {
					Type: "image",
					Data: []byte{0x89, 0x50, 0x4E, 0x47},
				},
				2: {
					Type: "image",
					Data: []byte{0xFF, 0xD8, 0xFF, 0xE0},
				},
			},
			expectBlocks: 2,
			expectIDs:    2,
		},
		{
			name: "mixed content",
			pastedContents: map[int]PastedContent{
				1: {
					Type:    "text",
					Content: "some text",
				},
				2: {
					Type: "image",
					Data: []byte{0x89, 0x50, 0x4E, 0x47},
				},
			},
			expectBlocks: 1,
			expectIDs:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks, ids, err := ProcessImageContent(tt.pastedContents)
			if err != nil {
				t.Fatalf("ProcessImageContent failed: %v", err)
			}

			if len(blocks) != tt.expectBlocks {
				t.Errorf("Expected %d blocks, got %d", tt.expectBlocks, len(blocks))
			}

			if len(ids) != tt.expectIDs {
				t.Errorf("Expected %d IDs, got %d", tt.expectIDs, len(ids))
			}

			// Verify block structure
			for _, block := range blocks {
				if block.Type != "image" {
					t.Errorf("Expected block type 'image', got '%s'", block.Type)
				}
				if block.Input == nil {
					t.Error("Expected Input to be set")
				}
			}
		})
	}
}

func TestCreateImageMetadataText(t *testing.T) {
	result := CreateImageMetadataText("image/png", 1024)
	expected := "Image metadata: type=image/png, size=1024 bytes"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestIsValidImagePaste(t *testing.T) {
	pastedContents := map[int]PastedContent{
		1: {Type: "image", Data: []byte{0x89, 0x50}},
		2: {Type: "text", Content: "text"},
	}

	tests := []struct {
		name     string
		pasteID  int
		contents map[int]PastedContent
		expected bool
	}{
		{
			name:     "valid image paste",
			pasteID:  1,
			contents: pastedContents,
			expected: true,
		},
		{
			name:     "text paste",
			pasteID:  2,
			contents: pastedContents,
			expected: false,
		},
		{
			name:     "non-existent paste",
			pasteID:  3,
			contents: pastedContents,
			expected: false,
		},
		{
			name:     "nil contents",
			pasteID:  1,
			contents: nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidImagePaste(tt.pasteID, tt.contents)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetImagePasteIDs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []int
	}{
		{
			name:     "no images",
			input:    "Hello world",
			expected: nil,
		},
		{
			name:     "single image",
			input:    "Check this out:\n[Pasted image #1]",
			expected: []int{1},
		},
		{
			name:     "multiple images",
			input:    "[Pasted image #1]\nSome text\n[Pasted image #2]",
			expected: []int{1, 2},
		},
		{
			name:     "invalid format",
			input:    "[Pasted image #abc]",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetImagePasteIDs(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d IDs, got %d", len(tt.expected), len(result))
				return
			}
			for i, id := range result {
				if id != tt.expected[i] {
					t.Errorf("Expected ID %d at position %d, got %d", tt.expected[i], i, id)
				}
			}
		})
	}
}
