package mcp

import (
	"encoding/json"
	"os"
	"strings"
)

type ChannelAllowlistEntry struct {
	Marketplace string `json:"marketplace"`
	Plugin      string `json:"plugin"`
}

func GetChannelAllowlist() []ChannelAllowlistEntry {
	raw := strings.TrimSpace(os.Getenv("CLAUDE_GO_MCP_CHANNEL_ALLOWLIST"))
	if raw == "" {
		return nil
	}
	var entries []ChannelAllowlistEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}
	return entries
}

func IsChannelsEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("CLAUDE_GO_MCP_CHANNELS")))
	return value == "1" || value == "true"
}

func IsChannelAllowlisted(pluginSource string) bool {
	plugin, marketplace := parsePluginIdentifier(pluginSource)
	if plugin == "" || marketplace == "" {
		return false
	}
	for _, entry := range GetChannelAllowlist() {
		if entry.Plugin == plugin && entry.Marketplace == marketplace {
			return true
		}
	}
	return false
}

func parsePluginIdentifier(source string) (name, marketplace string) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", ""
	}
	parts := strings.Split(source, "@")
	if len(parts) != 2 {
		return strings.TrimSpace(source), ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
