package agentruntime

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sync"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/postgres/*.sql
var postgresMigrationFS embed.FS

var gooseMigrationMu sync.Mutex

func RunPostgresGooseMigrations(ctx context.Context, db *sql.DB, dialect SQLDialect) error {
	if db == nil {
		return fmt.Errorf("sql db is required")
	}
	if dialect != SQLDialectPostgres {
		return fmt.Errorf("goose session store migrations require postgres dialect")
	}
	gooseMigrationMu.Lock()
	defer gooseMigrationMu.Unlock()
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(postgresMigrationFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if _, err := goose.EnsureDBVersionContext(ctx, db); err != nil {
		return err
	}
	if err := seedGooseVersionsFromLegacyMigrations(ctx, db); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, "migrations/postgres", goose.WithAllowMissing())
}

func seedGooseVersionsFromLegacyMigrations(ctx context.Context, db *sql.DB) error {
	const query = `
DO $$
BEGIN
	IF to_regclass('agent_schema_migrations') IS NOT NULL THEN
		INSERT INTO goose_db_version (version_id, is_applied)
		SELECT legacy.version, true
		FROM agent_schema_migrations legacy
		WHERE legacy.version > 0
		  AND NOT EXISTS (
			SELECT 1
			FROM goose_db_version goose
			WHERE goose.version_id = legacy.version
		  );
	END IF;
END $$`
	_, err := db.ExecContext(ctx, query)
	return err
}
