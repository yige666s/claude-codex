package scripts

import (
	"context"

	"claude-codex/internal/app/migrations"
	"claude-codex/internal/app/settings"
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
	user, path, err := loadSettings(settings.SourceUser, workingDirFromContext())
	if err != nil {
		return err
	}
	switch stringValue(user, "model") {
	case "claude-opus-4-20250514", "claude-opus-4-1-20250805", "opus-4-5-fast":
		user["model"] = "opus"
		return saveSettings(path, user)
	default:
		return nil
	}
}
