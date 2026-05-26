package agentruntime

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func requireSQLColumns(ctx context.Context, db *sql.DB, table string, columns ...string) error {
	if db == nil {
		return fmt.Errorf("sql db is required")
	}
	selectList := "1"
	if len(columns) > 0 {
		selectList = strings.Join(columns, ", ")
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE 1 = 0", selectList, table)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("required SQL schema %s missing or incompatible: %w", table, err)
	}
	return rows.Close()
}
