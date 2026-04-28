package scripts

import (
	"context"

	"claude-codex/internal/app/migrations"
	"claude-codex/internal/app/settings"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     7,
		Name:        "repl_bridge_to_remote_control",
		Description: "Migrate replBridgeEnabled to remoteControlAtStartup",
		Migrate:     migrateReplBridgeEnabledToRemoteControlAtStartup,
	})
}

// migrateReplBridgeEnabledToRemoteControlAtStartup migrates REPL bridge setting
func migrateReplBridgeEnabledToRemoteControlAtStartup(ctx context.Context) error {
	cfg, path, err := loadRawConfig()
	if err != nil {
		return err
	}
	if value, ok := cfg["replBridgeEnabled"].(bool); ok {
		if err := setUserSetting(settings.Document{"remoteControlAtStartup": value}); err != nil {
			return err
		}
		delete(cfg, "replBridgeEnabled")
		return saveRawConfig(path, cfg)
	}
	return nil
}
