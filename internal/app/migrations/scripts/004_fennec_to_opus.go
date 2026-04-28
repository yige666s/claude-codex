package scripts

import (
	"context"
	"strings"

	"claude-codex/internal/app/migrations"
	"claude-codex/internal/app/settings"
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
	user, path, err := loadSettings(settings.SourceUser, workingDirFromContext())
	if err != nil {
		return err
	}
	model := strings.ToLower(stringValue(user, "model"))
	switch model {
	case "fennec-latest[1m]":
		user["model"] = "opus[1m]"
	case "fennec-latest":
		user["model"] = "opus"
	case "fennec-fast-latest":
		user["model"] = "opus[1m]"
		user["fastMode"] = true
	case "opus-4-5-fast":
		user["model"] = "opus"
		user["fastMode"] = true
	default:
		return nil
	}
	return saveSettings(path, user)
}
