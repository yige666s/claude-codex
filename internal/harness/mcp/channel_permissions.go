package mcp

import (
	"encoding/json"
	"hash/fnv"
	"os"
	"strings"
)

type ChannelPermissionResponse struct {
	Behavior   string
	FromServer string
}

type ChannelPermissionCallbacks struct {
	pending map[string]func(ChannelPermissionResponse)
}

func IsChannelPermissionRelayEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("CLAUDE_GO_MCP_CHANNEL_PERMISSIONS")))
	return value == "1" || value == "true"
}

const permissionAlphabet = "abcdefghijkmnopqrstuvwxyz"

var idAvoidSubstrings = []string{
	"fuck", "shit", "cunt", "cock", "dick", "twat", "piss", "crap", "bitch",
	"whore", "ass", "tit", "cum", "fag", "dyke", "nig", "kike", "rape", "nazi", "damn", "poo", "pee", "wank", "anus",
}

func ShortRequestID(toolUseID string) string {
	candidate := hashToID(toolUseID)
	for salt := 0; salt < 10; salt++ {
		if !containsBlockedSubstring(candidate) {
			return candidate
		}
		candidate = hashToID(toolUseID + ":" + string(rune('0'+salt)))
	}
	return candidate
}

func hashToID(input string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(input))
	value := h.Sum32()
	var out strings.Builder
	for i := 0; i < 5; i++ {
		out.WriteByte(permissionAlphabet[value%25])
		value /= 25
	}
	return out.String()
}

func containsBlockedSubstring(value string) bool {
	for _, blocked := range idAvoidSubstrings {
		if strings.Contains(value, blocked) {
			return true
		}
	}
	return false
}

func TruncateForPreview(input any) string {
	data, err := json.Marshal(input)
	if err != nil {
		return "(unserializable)"
	}
	if len(data) > 200 {
		return string(data[:200]) + "…"
	}
	return string(data)
}

func CreateChannelPermissionCallbacks() *ChannelPermissionCallbacks {
	return &ChannelPermissionCallbacks{pending: make(map[string]func(ChannelPermissionResponse))}
}

func (c *ChannelPermissionCallbacks) OnResponse(requestID string, handler func(ChannelPermissionResponse)) func() {
	key := strings.ToLower(strings.TrimSpace(requestID))
	c.pending[key] = handler
	return func() { delete(c.pending, key) }
}

func (c *ChannelPermissionCallbacks) Resolve(requestID, behavior, fromServer string) bool {
	key := strings.ToLower(strings.TrimSpace(requestID))
	handler := c.pending[key]
	if handler == nil {
		return false
	}
	delete(c.pending, key)
	handler(ChannelPermissionResponse{Behavior: behavior, FromServer: fromServer})
	return true
}

type PermissionRelayClient interface {
	ConnectionType() string
	Name() string
	ExperimentalCapabilities() map[string]any
}

func FilterPermissionRelayClients[T PermissionRelayClient](clients []T, allowlisted func(string) bool) []T {
	result := make([]T, 0, len(clients))
	for _, client := range clients {
		if client.ConnectionType() != "connected" || !allowlisted(client.Name()) {
			continue
		}
		experimental := client.ExperimentalCapabilities()
		if experimental["claude/channel"] == nil || experimental["claude/channel/permission"] == nil {
			continue
		}
		result = append(result, client)
	}
	return result
}
