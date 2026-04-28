package scripts

import (
	"context"

	"claude-codex/internal/app/migrations"
	"claude-codex/internal/app/settings"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     2,
		Name:        "bypass_permissions_to_settings",
		Description: "Move bypassPermissionsModeAccepted from global config to settings.json",
		Migrate:     migrateBypassPermissionsAcceptedToSettings,
	})
}

// migrateBypassPermissionsAcceptedToSettings moves bypass permissions flag to settings
func migrateBypassPermissionsAcceptedToSettings(ctx context.Context) error {
	cfg, path, err := loadRawConfig()
	if err != nil {
		return err
	}
	if boolValue(cfg, "bypassPermissionsModeAccepted") {
		if err := setUserSetting(settings.Document{"skipDangerousModePermissionPrompt": true}); err != nil {
			return err
		}
	}
	delete(cfg, "bypassPermissionsModeAccepted")
	return saveRawConfig(path, cfg)
}
