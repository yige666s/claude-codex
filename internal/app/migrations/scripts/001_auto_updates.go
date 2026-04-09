package scripts

import (
	"context"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/app/migrations"
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
	// TODO: Implement when config and settings modules are available
	// This is a placeholder that will be implemented after config module is ready

	// The migration should:
	// 1. Check globalConfig.autoUpdates === false
	// 2. Check globalConfig.autoUpdatesProtectedForNative !== true
	// 3. Set userSettings.env.DISABLE_AUTOUPDATER = "1"
	// 4. Set process.env.DISABLE_AUTOUPDATER = "1"
	// 5. Remove autoUpdates and autoUpdatesProtectedForNative from globalConfig
	// 6. Log analytics event

	return fmt.Errorf("migration not yet implemented - requires config module")
}
