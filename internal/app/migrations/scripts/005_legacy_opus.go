package scripts

import (
	"context"
	"time"

	"claude-codex/internal/app/migrations"
	"claude-codex/internal/app/settings"
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
	user, userPath, err := loadSettings(settings.SourceUser, workingDirFromContext())
	if err != nil {
		return err
	}
	switch stringValue(user, "model") {
	case "claude-opus-4-20250514", "claude-opus-4-1-20250805", "claude-opus-4-0", "claude-opus-4-1":
		user["model"] = "opus"
		if err := saveSettings(userPath, user); err != nil {
			return err
		}
		cfg, cfgPath, err := loadRawConfig()
		if err != nil {
			return err
		}
		cfg["legacyOpusMigrationTimestamp"] = time.Now().UnixMilli()
		return saveRawConfig(cfgPath, cfg)
	default:
		return nil
	}
}
