package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"claude-codex/internal/backend/agentruntime"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Publish one explicitly named local Skill using the same review, version and
// release methods as Admin. Use only when the operator intentionally enables
// a previously unpublished Skill; never auto-publish at API startup.
func main() {
	name := flag.String("name", "", "Skill directory name to publish")
	actor := flag.String("actor", "", "audited operator identity")
	flag.Parse()
	if *name == "" || *actor == "" || os.Getenv("AGENT_API_SQL_DSN") == "" {
		fail(errors.New("name, actor and SQL DSN required"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", os.Getenv("AGENT_API_SQL_DSN"))
	if err != nil {
		fail(err)
	}
	defer db.Close()
	registry := agentruntime.NewSQLSkillRegistryWithDialect(db, agentruntime.SQLDialectPostgres)
	record, err := registry.GetSkill(ctx, *name)
	if err != nil {
		fail(err)
	}
	if record.Status != agentruntime.SkillStatusUnpublished {
		fail(fmt.Errorf("Skill must be unpublished; current status=%s", record.Status))
	}
	review := agentruntime.ReviewSkillForPublication(record)
	if !review.Passed {
		fail(fmt.Errorf("publication review failed: %+v", review.Issues))
	}
	record, err = registry.SetSkillStatus(ctx, *name, agentruntime.SkillStatusPublished)
	if err != nil {
		fail(err)
	}
	if err := registry.RecordSkillVersion(ctx, record, "sandbox Skill operator publication"); err != nil {
		fail(err)
	}
	if err := registry.RecordSkillRelease(ctx, record, "sandbox Skill operator publication", *actor); err != nil {
		fail(err)
	}
	fmt.Printf("Skill %s published and release recorded\n", record.Name)
}
func fail(err error) { fmt.Fprintln(os.Stderr, "publish refused:", err); os.Exit(1) }
