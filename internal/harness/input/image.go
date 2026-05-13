package input

import (
	"encoding/base64"
	"fmt"
	"strings"

	"claude-codex/internal/public/types"
)

// ImageBlock represents an image content block.
type ImageBlock struct {
	Type   string      `json:"type"`
	Source ImageSource `json:"source"`
}

// ImageSource represents an image source.
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// ProcessImageContent processes image content blocks.
func ProcessImageContent(pastedContents map[int]PastedContent) ([]types.ContentBlock, []int, error) {
	if len(pastedContents) == 0 {
		return nil, nil, nil
	}

	var imageBlocks []types.ContentBlock
	var imagePasteIDs []int

	for id, content := range pastedContents {
		if content.Type == "image" {
			// Create image block with source data in Input field
			mediaType := detectMediaType(content.Data)
			imageData := base64.StdEncoding.EncodeToString(content.Data)

			source := map[string]interface{}{
				"type":       "base64",
				"media_type": mediaType,
				"data":       imageData,
			}
			imageBlock := types.ContentBlock{
				Type:   "image",
				Source: source,
				Input:  source,
			}

			imageBlocks = append(imageBlocks, imageBlock)
			imagePasteIDs = append(imagePasteIDs, id)
		}
	}

	return imageBlocks, imagePasteIDs, nil
}

// detectMediaType detects the media type from image data.
func detectMediaType(data []byte) string {
	if len(data) < 4 {
		return "image/png"
	}

	// Check PNG signature
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}

	// Check JPEG signature
	if data[0] == 0xFF && data[1] == 0xD8 {
		return "image/jpeg"
	}

	// Check GIF signature
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return "image/gif"
	}

	// Check WebP signature
	if len(data) >= 12 && data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
		if data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 {
			return "image/webp"
		}
	}

	// Default to PNG
	return "image/png"
}

// CreateImageMetadataText creates metadata text for an image.
func CreateImageMetadataText(mediaType string, size int) string {
	return fmt.Sprintf("Image metadata: type=%s, size=%d bytes", mediaType, size)
}

// IsValidImagePaste checks if a paste ID is valid for images.
func IsValidImagePaste(pasteID int, pastedContents map[int]PastedContent) bool {
	if pastedContents == nil {
		return false
	}

	content, ok := pastedContents[pasteID]
	if !ok {
		return false
	}

	return content.Type == "image"
}

// GetImagePasteIDs extracts image paste IDs from input string.
func GetImagePasteIDs(input string) []int {
	var ids []int

	// Look for [Pasted image #N] patterns
	lines := strings.Split(input, "\n")
	for _, line := range lines {
		if strings.Contains(line, "[Pasted image #") {
			var id int
			if _, err := fmt.Sscanf(line, "[Pasted image #%d]", &id); err == nil {
				ids = append(ids, id)
			}
		}
	}

	return ids
}
