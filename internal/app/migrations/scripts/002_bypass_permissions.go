package scripts

import (
	"context"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/app/migrations"
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
	// TODO: Implement when config and settings modules are available

	// The migration should:
	// 1. Check globalConfig.bypassPermissionsModeAccepted === true
	// 2. Check if skipDangerousModePermissionPrompt is not already set
	// 3. Set userSettings.skipDangerousModePermissionPrompt = true
	// 4. Remove bypassPermissionsModeAccepted from globalConfig
	// 5. Log analytics event

	return fmt.Errorf("migration not yet implemented - requires config module")
}
