package agentruntime

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeSQLStoresDoNotOwnSchemaDDL(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test file")
	}
	dir := filepath.Dir(file)
	forbidden := []string{
		"CREATE TABLE IF NOT EXISTS",
		"ALTER TABLE",
		"CREATE INDEX IF NOT EXISTS",
		"CREATE EXTENSION IF NOT EXISTS",
		"RunSQLMigrations",
		"SQLMigration",
		"initPostgresStoreSchema",
		"ensureReadableTimeColumns",
		"postgresTimeColumns",
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range forbidden {
			if strings.Contains(string(content), needle) {
				t.Fatalf("%s contains runtime schema DDL marker %q; put schema changes in goose migrations", name, needle)
			}
		}
	}
}
