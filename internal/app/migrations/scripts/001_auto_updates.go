package scripts

import (
	"context"
	"os"

	"claude-codex/internal/app/migrations"
	"claude-codex/internal/app/settings"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     1,
		Name:        "auto_updates_to_settings",
		Description: "Move user-set autoUpdates preference to settings.json env var",
		Migrate:     migrateAutoUpdatesToSettings,
	})
}

// migrateAutoUpdatesToSettings moves autoUpdates from global config to settings
// Only migrates if user explicitly disabled auto-updates (not for protection)
func migrateAutoUpdatesToSettings(ctx context.Context) error {
	cfg, path, err := loadRawConfig()
	if err != nil {
		return err
	}
	if value, ok := cfg["autoUpdates"].(bool); ok && !value && !boolValue(cfg, "autoUpdatesProtectedForNative") {
		if err := setUserSetting(settings.Document{"env": map[string]any{"DISABLE_AUTOUPDATER": "1"}}); err != nil {
			return err
		}
		_ = os.Setenv("DISABLE_AUTOUPDATER", "1")
	}
	delete(cfg, "autoUpdates")
	delete(cfg, "autoUpdatesProtectedForNative")
	return saveRawConfig(path, cfg)
}
