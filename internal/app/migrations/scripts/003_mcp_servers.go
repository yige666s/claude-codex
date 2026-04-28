package scripts

import (
	"context"

	"claude-codex/internal/app/migrations"
	"claude-codex/internal/app/settings"
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
	workingDir := workingDirFromContext()
	project, projectPath, err := loadSettings(settings.SourceProject, workingDir)
	if err != nil {
		return err
	}
	updates := settings.Document{}
	for _, key := range []string{"enableAllProjectMcpServers", "enabledMcpjsonServers", "disabledMcpjsonServers"} {
		if value, ok := project[key]; ok {
			updates[key] = value
			delete(project, key)
		}
	}
	if len(updates) > 0 {
		if err := setLocalSetting(updates); err != nil {
			return err
		}
	}
	return saveSettings(projectPath, project)
}
