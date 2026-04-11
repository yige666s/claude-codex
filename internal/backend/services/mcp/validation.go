package mcp

import "encoding/json"

func GetContentSizeEstimate(content any) int {
	switch v := content.(type) {
	case string:
		return len(v) / 4
	case []byte:
		return len(v) / 4
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return 0
		}
		return len(data) / 4
	}
}

func ContentNeedsTruncation(content any, maxChars int) bool {
	switch v := content.(type) {
	case string:
		return len(v) > maxChars
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return false
		}
		return len(data) > maxChars
	}
}

func TruncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}
	return content[:maxChars] + "\n\n[OUTPUT TRUNCATED]"
}
