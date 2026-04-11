package scripts

import (
	"context"
	"fmt"

	"claude-codex/internal/app/migrations"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     3,
		Name:        "mcp_servers_to_settings",
		Description: "Move MCP server approval fields from project config to local settings",
		Migrate:     migrateEnableAllProjectMcpServersToSettings,
	})
}

// migrateEnableAllProjectMcpServersToSettings moves MCP approval fields to settings
func migrateEnableAllProjectMcpServersToSettings(ctx context.Context) error {
	// TODO: Implement when config and settings modules are available

	// The migration should:
	// 1. Check projectConfig for enableAllProjectMcpServers, enabledMcpjsonServers, disabledMcpjsonServers
	// 2. Migrate to localSettings (merge arrays, avoid duplicates)
	// 3. Remove fields from projectConfig
	// 4. Log analytics event

	return fmt.Errorf("migration not yet implemented - requires config module")
}
