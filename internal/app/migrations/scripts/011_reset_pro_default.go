package scripts

import (
	"context"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/app/migrations"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     11,
		Name:        "reset_pro_to_opus_default",
		Description: "Reset Pro users to Opus default model",
		Migrate:     resetProToOpusDefault,
	})
}

// resetProToOpusDefault resets Pro users to opus default
func resetProToOpusDefault(ctx context.Context) error {
	// TODO: Implement when config and settings modules are available

	// The migration should:
	// 1. Check if user is Pro subscriber
	// 2. Reset model to opus default if needed
	// 3. Log analytics event

	return fmt.Errorf("migration not yet implemented - requires config module")
}
