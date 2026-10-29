package bridge

import (
	"encoding/base64"
	"strings"
)

type InboundMessage struct {
	Type    string          `json:"type"`
	UUID    string          `json:"uuid,omitempty"`
	Message *InboundPayload `json:"message,omitempty"`
}

type InboundPayload struct {
	Content any `json:"content,omitempty"`
}

type InboundMessageFields struct {
	Content any
	UUID    string
}

func ExtractInboundMessageFields(msg InboundMessage) (*InboundMessageFields, bool) {
	if msg.Type != "user" || msg.Message == nil || msg.Message.Content == nil {
		return nil, false
	}

	switch content := msg.Message.Content.(type) {
	case string:
		if strings.TrimSpace(content) == "" {
			return nil, false
		}
		return &InboundMessageFields{Content: content, UUID: msg.UUID}, true
	case []map[string]any:
		normalized := NormalizeImageBlocks(content)
		if len(normalized) == 0 {
			return nil, false
		}
		return &InboundMessageFields{Content: normalized, UUID: msg.UUID}, true
	case []any:
		blocks := make([]map[string]any, 0, len(content))
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			blocks = append(blocks, block)
		}
		if len(blocks) == 0 {
			return nil, false
		}
		normalized := NormalizeImageBlocks(blocks)
		return &InboundMessageFields{Content: normalized, UUID: msg.UUID}, true
	default:
		return nil, false
	}
}

func NormalizeImageBlocks(blocks []map[string]any) []map[string]any {
	if len(blocks) == 0 {
		return blocks
	}

	out := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		if !isMalformedBase64Image(block) {
			out = append(out, block)
			continue
		}

		normalized := cloneMap(block)
		source, _ := block["source"].(map[string]any)
		src := cloneMap(source)
		data, _ := src["data"].(string)
		if mediaType, ok := src["mediaType"].(string); ok && mediaType != "" {
			src["media_type"] = mediaType
		} else {
			src["media_type"] = DetectImageFormatFromBase64(data)
		}
		delete(src, "mediaType")
		normalized["source"] = src
		out = append(out, normalized)
	}

	return out
}

func DetectImageFormatFromBase64(data string) string {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(data)
	}
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(data)
	}
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(data)
	}
	if err != nil || len(decoded) < 4 {
		return "image/png"
	}

	switch {
	case len(decoded) >= 8 &&
		decoded[0] == 0x89 &&
		decoded[1] == 0x50 &&
		decoded[2] == 0x4E &&
		decoded[3] == 0x47:
		return "image/png"
	case decoded[0] == 0xFF && decoded[1] == 0xD8:
		return "image/jpeg"
	case decoded[0] == 0x47 && decoded[1] == 0x49 && decoded[2] == 0x46:
		return "image/gif"
	case len(decoded) >= 12 &&
		string(decoded[0:4]) == "RIFF" &&
		string(decoded[8:12]) == "WEBP":
		return "image/webp"
	default:
		return "image/png"
	}
}

func isMalformedBase64Image(block map[string]any) bool {
	if block["type"] != "image" {
		return false
	}
	source, ok := block["source"].(map[string]any)
	if !ok || source["type"] != "base64" {
		return false
	}
	_, hasSnake := source["media_type"]
	_, hasCamel := source["mediaType"]
	return !hasSnake && hasCamel
}

func cloneMap[T any](in map[string]T) map[string]T {
	if in == nil {
		return nil
	}
	out := make(map[string]T, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
