package migrations

import (
	"context"
	"fmt"
	"sync"
)

// Executor executes migrations
type Executor struct {
	registry         *Registry
	versionManager   VersionManager
	analyticsLogger  AnalyticsLogger
	mu               sync.Mutex
}

// VersionManager manages migration version tracking
type VersionManager interface {
	GetCurrentVersion(ctx context.Context) (int, error)
	SetCurrentVersion(ctx context.Context, version int) error
}

// AnalyticsLogger logs migration events for analytics
type AnalyticsLogger interface {
	LogEvent(ctx context.Context, event string, metadata map[string]interface{})
	LogError(ctx context.Context, err error)
}

// NewExecutor creates a new migration executor
func NewExecutor(registry *Registry, vm VersionManager, logger AnalyticsLogger) *Executor {
	return &Executor{
		registry:        registry,
		versionManager:  vm,
		analyticsLogger: logger,
	}
}

// ExecuteOptions configures migration execution
type ExecuteOptions struct {
	// DryRun simulates migration without applying changes
	DryRun bool
	// StopOnError stops execution on first error
	StopOnError bool
	// TargetVersion runs migrations up to this version (0 = all)
	TargetVersion int
}

// Execute runs all pending migrations
func (e *Executor) Execute(ctx context.Context, opts *ExecuteOptions) (*MigrationResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if opts == nil {
		opts = &ExecuteOptions{}
	}

	// Get current version
	currentVersion, err := e.versionManager.GetCurrentVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current version: %w", err)
	}

	// Get pending migrations
	pending := e.registry.GetPendingMigrations(currentVersion)
	if len(pending) == 0 {
		return &MigrationResult{
			Applied: []int{},
			Skipped: []int{},
			Failed:  []MigrationError{},
		}, nil
	}

	result := &MigrationResult{
		Applied: make([]int, 0),
		Skipped: make([]int, 0),
		Failed:  make([]MigrationError, 0),
	}

	// Execute migrations in order
	for _, migration := range pending {
		// Check target version
		if opts.TargetVersion > 0 && migration.Version > opts.TargetVersion {
			result.Skipped = append(result.Skipped, migration.Version)
			continue
		}

		// Execute migration
		if opts.DryRun {
			e.logMigrationEvent(ctx, "migration_dry_run", migration, nil)
			result.Skipped = append(result.Skipped, migration.Version)
			continue
		}

		if err := e.executeMigration(ctx, migration); err != nil {
			migErr := MigrationError{
				Version: migration.Version,
				Name:    migration.Name,
				Err:     err,
			}
			result.Failed = append(result.Failed, migErr)

			e.logMigrationEvent(ctx, "migration_failed", migration, map[string]interface{}{
				"error": err.Error(),
			})

			if opts.StopOnError {
				return result, &migErr
			}
			continue
		}

		result.Applied = append(result.Applied, migration.Version)

		// Update version after successful migration
		if err := e.versionManager.SetCurrentVersion(ctx, migration.Version); err != nil {
			return result, fmt.Errorf("failed to update version after migration %d: %w", migration.Version, err)
		}

		e.logMigrationEvent(ctx, "migration_success", migration, nil)
	}

	return result, nil
}

// executeMigration executes a single migration
func (e *Executor) executeMigration(ctx context.Context, m Migration) error {
	defer func() {
		if r := recover(); r != nil {
			e.analyticsLogger.LogError(ctx, fmt.Errorf("migration %d panicked: %v", m.Version, r))
		}
	}()

	return m.Migrate(ctx)
}

// logMigrationEvent logs a migration event
func (e *Executor) logMigrationEvent(ctx context.Context, event string, m Migration, extra map[string]interface{}) {
	if e.analyticsLogger == nil {
		return
	}

	metadata := map[string]interface{}{
		"migration_version": m.Version,
		"migration_name":    m.Name,
	}

	for k, v := range extra {
		metadata[k] = v
	}

	e.analyticsLogger.LogEvent(ctx, event, metadata)
}

// GetStatus returns the current migration status
func (e *Executor) GetStatus(ctx context.Context) (current int, latest int, pending []Migration, err error) {
	current, err = e.versionManager.GetCurrentVersion(ctx)
	if err != nil {
		return 0, 0, nil, err
	}

	latest = CurrentMigrationVersion
	pending = e.registry.GetPendingMigrations(current)

	return current, latest, pending, nil
}
