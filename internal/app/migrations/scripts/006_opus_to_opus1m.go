package scripts

import (
	"context"
	"strings"

	"claude-codex/internal/app/migrations"
	"claude-codex/internal/app/settings"
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
	user, path, err := loadSettings(settings.SourceUser, workingDirFromContext())
	if err != nil {
		return err
	}
	model := strings.ToLower(stringValue(user, "model"))
	if model == "opus" || model == "claude-opus-4-1" || model == "claude-opus-4-0" {
		user["model"] = "opus[1m]"
		return saveSettings(path, user)
	}
	return nil
}
