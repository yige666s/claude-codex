package migrations

import (
	"context"
	"fmt"
)

// Migration represents a single migration operation
type Migration struct {
	Version     int
	Name        string
	Description string
	Migrate     func(ctx context.Context) error
}

// MigrationError represents a migration execution error
type MigrationError struct {
	Version int
	Name    string
	Err     error
}

func (e *MigrationError) Error() string {
	return fmt.Sprintf("migration %d (%s) failed: %v", e.Version, e.Name, e.Err)
}

func (e *MigrationError) Unwrap() error {
	return e.Err
}

// MigrationResult represents the result of running migrations
type MigrationResult struct {
	Applied []int
	Skipped []int
	Failed  []MigrationError
}

// CurrentMigrationVersion is the latest migration version
// This should be incremented when adding new migrations
const CurrentMigrationVersion = 11
