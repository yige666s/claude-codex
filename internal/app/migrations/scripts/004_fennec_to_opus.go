package scripts

import (
	"context"
	"fmt"

	"claude-codex/internal/app/migrations"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     4,
		Name:        "fennec_to_opus",
		Description: "Migrate users on removed fennec model aliases to Opus 4.6 aliases",
		Migrate:     migrateFennecToOpus,
	})
}

// migrateFennecToOpus migrates fennec model aliases to opus
func migrateFennecToOpus(ctx context.Context) error {
	// TODO: Implement when config and settings modules are available

	// The migration should:
	// 1. Check USER_TYPE === "ant"
	// 2. Check userSettings.model for fennec patterns:
	//    - fennec-latest[1m] → opus[1m]
	//    - fennec-latest → opus
	//    - fennec-fast-latest → opus[1m] + fastMode: true
	//    - opus-4-5-fast → opus + fastMode: true
	// 3. Update userSettings.model and fastMode accordingly
	// 4. Only touches userSettings (idempotent)

	return fmt.Errorf("migration not yet implemented - requires config module")
}
