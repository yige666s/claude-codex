package mcp

import "strings"

const (
	ClaudeInChromeServerName = "claude-in-chrome"
	ComputerUseServerName    = "computer-use"
	IDEServerName            = "ide"
)

var allowedIDETools = map[string]bool{
	"mcp__ide__executeCode":    true,
	"mcp__ide__getDiagnostics": true,
}

func normalizeMCPName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}

func IsClaudeInChromeServer(name string) bool {
	return normalizeMCPName(name) == ClaudeInChromeServerName
}

func IsComputerUseServer(name string) bool {
	return normalizeMCPName(name) == ComputerUseServerName
}

func IsIDEServer(name string) bool {
	return normalizeMCPName(name) == IDEServerName
}

func IsIncludedTool(serverName, toolName string) bool {
	if !IsIDEServer(serverName) {
		return true
	}
	return allowedIDETools[toolName]
}
