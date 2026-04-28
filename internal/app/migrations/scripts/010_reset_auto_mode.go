package scripts

import (
	"context"

	"claude-codex/internal/app/migrations"
)

func init() {
	migrations.MustRegister(migrations.Migration{
		Version:     10,
		Name:        "reset_auto_mode_opt_in",
		Description: "Reset auto mode opt-in for default offer",
		Migrate:     resetAutoModeOptInForDefaultOffer,
	})
}

// resetAutoModeOptInForDefaultOffer resets auto mode opt-in flag
func resetAutoModeOptInForDefaultOffer(ctx context.Context) error {
	cfg, path, err := loadRawConfig()
	if err != nil {
		return err
	}
	for _, key := range []string{
		"autoModeOptInAccepted",
		"autoModeDefaultOfferAccepted",
		"autoModeDefaultOfferRejected",
		"autoModeDefaultOfferDismissed",
	} {
		delete(cfg, key)
	}
	return saveRawConfig(path, cfg)
}
