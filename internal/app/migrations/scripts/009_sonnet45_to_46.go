package scripts

import (
	"context"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/app/migrations"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     9,
		Name:        "sonnet45_to_sonnet46",
		Description: "Migrate Pro/Max/Team Premium users off explicit Sonnet 4.5 to sonnet alias (4.6)",
		Migrate:     migrateSonnet45ToSonnet46,
	})
}

// migrateSonnet45ToSonnet46 migrates sonnet 4.5 to sonnet 4.6 alias
func migrateSonnet45ToSonnet46(ctx context.Context) error {
	// TODO: Implement when config and settings modules are available

	// The migration should:
	// 1. Check API provider === "firstParty"
	// 2. Check user is Pro/Max/Team Premium subscriber
	// 3. Check userSettings.model for Sonnet 4.5 strings:
	//    - claude-sonnet-4-5-20250929
	//    - claude-sonnet-4-5-20250929[1m]
	//    - sonnet-4-5-20250929
	//    - sonnet-4-5-20250929[1m]
	// 4. Update to "sonnet" or "sonnet[1m]" alias
	// 5. Set globalConfig.sonnet45To46MigrationTimestamp (skip for new users)
	// 6. Log analytics event with from_model and has_1m

	return fmt.Errorf("migration not yet implemented - requires config module")
}
