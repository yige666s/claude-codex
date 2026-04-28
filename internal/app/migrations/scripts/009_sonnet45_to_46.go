package scripts

import (
	"context"
	"strings"
	"time"

	"claude-codex/internal/app/migrations"
	"claude-codex/internal/app/settings"
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
	user, userPath, err := loadSettings(settings.SourceUser, workingDirFromContext())
	if err != nil {
		return err
	}
	model := strings.ToLower(stringValue(user, "model"))
	switch model {
	case "claude-sonnet-4-5-20250929", "sonnet-4-5-20250929", "sonnet-4-5":
		user["model"] = "sonnet"
	case "claude-sonnet-4-5-20250929[1m]", "sonnet-4-5-20250929[1m]", "sonnet-4-5[1m]":
		user["model"] = "sonnet[1m]"
	default:
		return nil
	}
	if err := saveSettings(userPath, user); err != nil {
		return err
	}
	cfg, cfgPath, err := loadRawConfig()
	if err != nil {
		return err
	}
	cfg["sonnet45To46MigrationTimestamp"] = time.Now().UnixMilli()
	return saveRawConfig(cfgPath, cfg)
}
