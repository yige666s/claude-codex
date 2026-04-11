package scripts

import (
	"context"
	"fmt"

	"claude-codex/internal/app/migrations"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     6,
		Name:        "opus_to_opus1m",
		Description: "Migrate Opus users to Opus 1M context window",
		Migrate:     migrateOpusToOpus1m,
	})
}

// migrateOpusToOpus1m migrates opus to opus[1m]
func migrateOpusToOpus1m(ctx context.Context) error {
	// TODO: Implement when config and settings modules are available

	// The migration should:
	// 1. Check for opus model strings that need 1M context upgrade
	// 2. Update to opus[1m] variant
	// 3. Log analytics event

	return fmt.Errorf("migration not yet implemented - requires config module")
}
