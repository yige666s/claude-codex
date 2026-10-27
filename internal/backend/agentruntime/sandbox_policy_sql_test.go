package agentruntime

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/backend/sandboxpolicy"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSandboxPolicySQLPublishRollback(t *testing.T) {
	dsn := os.Getenv("SANDBOX_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("set SANDBOX_POLICY_TEST_DSN for Postgres integration")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for attempts := 0; attempts < 30; attempts++ {
		if err = db.PingContext(ctx); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		t.Fatalf("postgres unavailable: %v", err)
	}
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatal(err)
	}
	store := &sandboxpolicy.Store{DB: db}
	first, err := store.Active(ctx)
	if err != nil || first.Document.Enabled {
		t.Fatalf("unsafe first policy: %+v, %v", first, err)
	}
	doc := sandboxpolicy.Document{Enabled: true, DefaultProfile: "isolated", Profiles: map[string]sandboxpolicy.Profile{
		"isolated": {Driver: "oci", NodePool: "wsl_dev", ImageDigest: "python@sha256:" + strings.Repeat("a", 64), Network: "deny", CPUs: 1, MemoryMiB: 512, PIDs: 64, TimeoutSeconds: 30, MaxOutputBytes: 1024},
	}, NodePools: map[string]sandboxpolicy.NodePool{
		"wsl_dev": {Endpoint: "https://sandbox.example.com", CredentialRef: "sandbox_dev", Environment: "development", MaxConcurrency: 1},
	}}
	draft, err := store.Draft(ctx, doc, "admin")
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.Publish(ctx, draft.ID, first.ID, "admin")
	if err != nil || active.ID != draft.ID {
		t.Fatalf("publish: %+v, %v", active, err)
	}
	if _, err := store.Publish(ctx, draft.ID, first.ID, "admin"); !errors.Is(err, sandboxpolicy.ErrRevisionConflict) {
		t.Fatalf("stale publish: %v", err)
	}
	rolled, err := store.Rollback(ctx, first.ID, active.ID, "admin")
	if err != nil || rolled.ID <= active.ID || rolled.Document.Enabled || rolled.SourceRevision == nil || *rolled.SourceRevision != first.ID {
		t.Fatalf("rollback: %+v, %v", rolled, err)
	}
	current, err := store.Active(ctx)
	if err != nil || current.ID != rolled.ID || current.Document.Enabled {
		t.Fatalf("current: %+v, %v", current, err)
	}
	history, err := store.List(ctx, 10)
	if err != nil || len(history) < 3 || history[0].ID != rolled.ID {
		t.Fatalf("history=%+v %v", history, err)
	}
	var audits int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM agent_sandbox_policy_audit WHERE operation IN ('publish','rollback')`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audit count=%d, %v", audits, err)
	}
}
