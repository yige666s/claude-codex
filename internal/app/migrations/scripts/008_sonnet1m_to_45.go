package scripts

import (
	"context"
	"strings"

	"claude-codex/internal/app/migrations"
	"claude-codex/internal/app/settings"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     8,
		Name:        "sonnet1m_to_sonnet45",
		Description: "Migrate Sonnet 1M users to Sonnet 4.5",
		Migrate:     migrateSonnet1mToSonnet45,
	})
}

// migrateSonnet1mToSonnet45 migrates sonnet 1M to sonnet 4.5
func migrateSonnet1mToSonnet45(ctx context.Context) error {
	user, path, err := loadSettings(settings.SourceUser, workingDirFromContext())
	if err != nil {
		return err
	}
	model := strings.ToLower(stringValue(user, "model"))
	switch model {
	case "sonnet[1m]", "claude-sonnet-4-5-20250929[1m]":
		user["model"] = "sonnet-4-5[1m]"
	case "sonnet-1m", "claude-sonnet-1m":
		user["model"] = "sonnet-4-5"
	default:
		return nil
	}
	return saveSettings(path, user)
}
