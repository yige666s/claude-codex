package scripts

import (
	"context"
	"fmt"

	"claude-codex/internal/app/migrations"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     10,
		Name:        "reset_auto_mode_opt_in",
		Description: "Reset auto mode opt-in for default offer",
		Migrate:     resetAutoModeOptInForDefaultOffer,
	})
}

// resetAutoModeOptInForDefaultOffer resets auto mode opt-in flag
func resetAutoModeOptInForDefaultOffer(ctx context.Context) error {
	// TODO: Implement when config and settings modules are available

	// The migration should:
	// 1. Reset auto mode opt-in flags in config
	// 2. Allow users to see the default offer again
	// 3. Log analytics event

	return fmt.Errorf("migration not yet implemented - requires config module")
}
