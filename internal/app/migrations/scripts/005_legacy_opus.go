package scripts

import (
	"context"
	"fmt"

	"claude-codex/internal/app/migrations"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     5,
		Name:        "legacy_opus_to_current",
		Description: "Migrate first-party users off explicit Opus 4.0/4.1 model strings",
		Migrate:     migrateLegacyOpusToCurrent,
	})
}

// migrateLegacyOpusToCurrent migrates legacy opus model strings to current alias
func migrateLegacyOpusToCurrent(ctx context.Context) error {
	// TODO: Implement when config and settings modules are available

	// The migration should:
	// 1. Check API provider === "firstParty"
	// 2. Check if legacy model remap is enabled
	// 3. Check userSettings.model for legacy opus strings:
	//    - claude-opus-4-20250514
	//    - claude-opus-4-1-20250805
	//    - claude-opus-4-0
	//    - claude-opus-4-1
	// 4. Update to "opus" alias
	// 5. Set globalConfig.legacyOpusMigrationTimestamp
	// 6. Log analytics event

	return fmt.Errorf("migration not yet implemented - requires config module")
}
