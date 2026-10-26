package agentruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SeedBuiltinPrompts makes built-in runtime prompts visible in Admin on startup.
// Only missing templates are seeded: published/admin-edited versions, metadata,
// environment pins, and experiments are never changed by a redeploy.
func SeedBuiltinPrompts(ctx context.Context, store PromptStore) (int, error) {
	if store == nil {
		return 0, nil
	}
	created := 0
	for _, baseline := range BuiltinSystemPromptBaselines() {
		if _, err := store.GetPrompt(ctx, baseline.Prompt.ID); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return created, fmt.Errorf("inspect built-in prompt %s: %w", baseline.Prompt.ID, err)
		}
		if seeder, ok := store.(interface {
			SeedPromptTemplateIfMissing(context.Context, PromptTemplate) (bool, error)
		}); ok {
			inserted, err := seeder.SeedPromptTemplateIfMissing(ctx, baseline.Prompt)
			if err != nil {
				return created, fmt.Errorf("seed built-in prompt %s: %w", baseline.Prompt.ID, err)
			}
			if !inserted {
				continue
			}
		} else if _, err := store.UpsertPrompt(ctx, baseline.Prompt); err != nil {
			return created, fmt.Errorf("seed built-in prompt %s: %w", baseline.Prompt.ID, err)
		}
		if _, err := store.CreatePromptVersion(ctx, baseline.Version); err != nil {
			// A concurrent startup may have inserted the immutable version.
			if _, lookupErr := store.GetPromptVersion(ctx, baseline.Prompt.ID, baseline.Version.Version); lookupErr != nil {
				return created, fmt.Errorf("seed built-in prompt %s@%s: %w", baseline.Prompt.ID, baseline.Version.Version, err)
			}
		}
		created++
	}
	return created, nil
}
