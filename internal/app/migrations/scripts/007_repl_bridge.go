package scripts

import (
	"context"
	"fmt"

	"claude-codex/internal/app/migrations"
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
	// TODO: Implement when config and settings modules are available

	// The migration should:
	// 1. Check for replBridgeEnabled in config
	// 2. Migrate to remoteControlAtStartup setting
	// 3. Log analytics event

	return fmt.Errorf("migration not yet implemented - requires config module")
}
