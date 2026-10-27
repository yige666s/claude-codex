package sandboxpolicy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrRevisionConflict = errors.New("sandbox policy active revision changed")

type Revision struct {
	ID             int64     `json:"revision"`
	Document       Document  `json:"document"`
	State          string    `json:"state"`
	Actor          string    `json:"actor"`
	SourceRevision *int64    `json:"source_revision,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type Repository interface {
	Active(context.Context) (Revision, error)
	Draft(context.Context, Document, string) (Revision, error)
	Publish(context.Context, int64, int64, string) (Revision, error)
	Rollback(context.Context, int64, int64, string) (Revision, error)
	List(context.Context, int) ([]Revision, error)
}

type Store struct{ DB *sql.DB }

func (s *Store) Active(ctx context.Context) (Revision, error) {
	if s == nil || s.DB == nil {
		return Revision{}, errors.New("sandbox policy SQL store unavailable")
	}
	var rev Revision
	var raw []byte
	var source sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT r.revision,r.payload,r.state,r.actor,r.source_revision,r.created_at FROM agent_sandbox_policy_active a JOIN agent_sandbox_policy_revisions r ON r.revision=a.revision WHERE a.singleton=1`).Scan(&rev.ID, &raw, &rev.State, &rev.Actor, &source, &rev.CreatedAt)
	if err != nil {
		return Revision{}, err
	}
	if source.Valid {
		rev.SourceRevision = &source.Int64
	}
	if err = json.Unmarshal(raw, &rev.Document); err != nil {
		return Revision{}, err
	}
	if err = rev.Document.Validate(); err != nil {
		return Revision{}, fmt.Errorf("invalid active sandbox policy: %w", err)
	}
	return rev, nil
}

func (s *Store) Draft(ctx context.Context, doc Document, actor string) (Revision, error) {
	if s == nil || s.DB == nil {
		return Revision{}, errors.New("sandbox policy SQL store unavailable")
	}
	if err := doc.Validate(); err != nil {
		return Revision{}, err
	}
	if actor == "" || strings.TrimSpace(actor) != actor || len(actor) > 128 {
		return Revision{}, errors.New("invalid sandbox policy actor")
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return Revision{}, err
	}
	var rev Revision
	err = s.DB.QueryRowContext(ctx, `INSERT INTO agent_sandbox_policy_revisions (payload,state,actor) VALUES ($1,'draft',$2) RETURNING revision,created_at`, string(raw), actor).Scan(&rev.ID, &rev.CreatedAt)
	if err != nil {
		return Revision{}, err
	}
	rev.Document, rev.State, rev.Actor = doc, "draft", actor
	return rev, nil
}

// Publish serializes competing administrators with a row lock and CAS. Native
// cannot be published because Document.Validate currently rejects it.
func (s *Store) Publish(ctx context.Context, draftID, expectedActive int64, actor string) (Revision, error) {
	if s == nil || s.DB == nil {
		return Revision{}, errors.New("sandbox policy SQL store unavailable")
	}
	if draftID < 1 || expectedActive < 1 || actor == "" || strings.TrimSpace(actor) != actor || len(actor) > 128 {
		return Revision{}, errors.New("invalid sandbox policy publish request")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Revision{}, err
	}
	defer tx.Rollback()
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM agent_sandbox_policy_active WHERE singleton=1 FOR UPDATE`).Scan(&current); err != nil {
		return Revision{}, err
	}
	if current != expectedActive {
		return Revision{}, ErrRevisionConflict
	}
	var rev Revision
	var raw []byte
	var source sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT revision,payload,state,actor,source_revision,created_at FROM agent_sandbox_policy_revisions WHERE revision=$1`, draftID).Scan(&rev.ID, &raw, &rev.State, &rev.Actor, &source, &rev.CreatedAt)
	if err != nil {
		return Revision{}, err
	}
	if rev.State != "draft" {
		return Revision{}, ErrRevisionConflict
	}
	if source.Valid {
		rev.SourceRevision = &source.Int64
	}
	if err = json.Unmarshal(raw, &rev.Document); err != nil {
		return Revision{}, err
	}
	if err = rev.Document.Validate(); err != nil {
		return Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_sandbox_policy_revisions SET state='superseded' WHERE revision=$1`, current); err != nil {
		return Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_sandbox_policy_revisions SET state='active' WHERE revision=$1`, draftID); err != nil {
		return Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_sandbox_policy_active SET revision=$1,updated_at=now() WHERE singleton=1`, draftID); err != nil {
		return Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_sandbox_policy_audit(revision,previous_revision,actor,operation) VALUES ($1,$2,$3,$4)`, draftID, current, actor, "publish"); err != nil {
		return Revision{}, err
	}
	if err = tx.Commit(); err != nil {
		return Revision{}, err
	}
	rev.State = "active"
	return rev, nil
}

// Rollback atomically creates a new revision from history and publishes it.
// The original revision is retained for audit rather than reactivated in place.
func (s *Store) Rollback(ctx context.Context, sourceID, expectedActive int64, actor string) (Revision, error) {
	if s == nil || s.DB == nil {
		return Revision{}, errors.New("sandbox policy SQL store unavailable")
	}
	if sourceID < 1 || expectedActive < 1 || actor == "" || strings.TrimSpace(actor) != actor || len(actor) > 128 {
		return Revision{}, errors.New("invalid sandbox policy rollback request")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Revision{}, err
	}
	defer tx.Rollback()
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM agent_sandbox_policy_active WHERE singleton=1 FOR UPDATE`).Scan(&current); err != nil {
		return Revision{}, err
	}
	if current != expectedActive {
		return Revision{}, ErrRevisionConflict
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM agent_sandbox_policy_revisions WHERE revision=$1 AND state IN ('active','superseded')`, sourceID).Scan(&raw); err != nil {
		return Revision{}, err
	}
	var doc Document
	if err = json.Unmarshal(raw, &doc); err != nil {
		return Revision{}, err
	}
	if err = doc.Validate(); err != nil {
		return Revision{}, err
	}
	rev := Revision{Document: doc, State: "active", Actor: actor, SourceRevision: &sourceID}
	if err = tx.QueryRowContext(ctx, `INSERT INTO agent_sandbox_policy_revisions(payload,state,actor,source_revision) VALUES ($1,'active',$2,$3) RETURNING revision,created_at`, string(raw), actor, sourceID).Scan(&rev.ID, &rev.CreatedAt); err != nil {
		return Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_sandbox_policy_revisions SET state='superseded' WHERE revision=$1`, current); err != nil {
		return Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_sandbox_policy_active SET revision=$1,updated_at=now() WHERE singleton=1`, rev.ID); err != nil {
		return Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_sandbox_policy_audit(revision,previous_revision,actor,operation) VALUES ($1,$2,$3,'rollback')`, rev.ID, current, actor); err != nil {
		return Revision{}, err
	}
	if err = tx.Commit(); err != nil {
		return Revision{}, err
	}
	return rev, nil
}

func (s *Store) List(ctx context.Context, limit int) ([]Revision, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("sandbox policy SQL store unavailable")
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT revision,payload,state,actor,source_revision,created_at FROM agent_sandbox_policy_revisions ORDER BY revision DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Revision, 0, limit)
	for rows.Next() {
		var rev Revision
		var raw []byte
		var source sql.NullInt64
		if err = rows.Scan(&rev.ID, &raw, &rev.State, &rev.Actor, &source, &rev.CreatedAt); err != nil {
			return nil, err
		}
		if source.Valid {
			rev.SourceRevision = &source.Int64
		}
		if err = json.Unmarshal(raw, &rev.Document); err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}
