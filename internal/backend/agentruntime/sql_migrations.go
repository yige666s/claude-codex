package agentruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type SQLMigration struct {
	Version    int
	Statements []string
}

func RunSQLMigrations(ctx context.Context, db *sql.DB, dialect SQLDialect, migrations []SQLMigration) error {
	if db == nil {
		return fmt.Errorf("sql db is required")
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agent_schema_migrations (
	version INTEGER PRIMARY KEY,
	applied_at `+dialect.TimeType()+` NOT NULL
)`); err != nil {
		return err
	}
	if err := ensureReadableTimeColumns(ctx, db, dialect, "agent_schema_migrations", "applied_at"); err != nil {
		return err
	}
	for _, migration := range migrations {
		if migration.Version <= 0 {
			return fmt.Errorf("migration version must be positive")
		}
		applied, err := sqlMigrationApplied(ctx, db, dialect, migration.Version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, stmt := range migration.Statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %d: %w", migration.Version, err)
			}
		}
		if _, err := tx.ExecContext(ctx, dialect.Bind(`INSERT INTO agent_schema_migrations (version, applied_at) VALUES (?, ?)`), migration.Version, sqlTimeValue(time.Now(), dialect)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func sqlMigrationApplied(ctx context.Context, db *sql.DB, dialect SQLDialect, version int) (bool, error) {
	var existing int
	err := db.QueryRowContext(ctx, dialect.Bind(`SELECT version FROM agent_schema_migrations WHERE version = ?`), version).Scan(&existing)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}
