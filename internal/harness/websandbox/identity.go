package websandbox

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type IdentityBroker struct {
	ttl   time.Duration
	audit AuditSink
	mu    sync.Mutex
	live  map[string]Lease
}

func NewIdentityBroker(ttl time.Duration, audit AuditSink) *IdentityBroker {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	if audit == nil {
		audit = NoopAuditSink{}
	}
	return &IdentityBroker{
		ttl:   ttl,
		audit: audit,
		live:  make(map[string]Lease),
	}
}

func (b *IdentityBroker) Issue(ctx context.Context, scope Scope, action Action) (Lease, error) {
	if b == nil {
		return Lease{}, fmt.Errorf("identity broker is required")
	}
	allowed := make(map[string]struct{}, len(scope.AllowedEnv))
	for _, key := range scope.AllowedEnv {
		key = strings.TrimSpace(key)
		if key != "" {
			allowed[key] = struct{}{}
		}
	}
	env := make(map[string]string, len(allowed))
	for key := range allowed {
		if value, ok := action.Env[key]; ok {
			env[key] = value
			continue
		}
		if value, ok := os.LookupEnv(key); ok {
			env[key] = value
		}
	}
	for key := range action.Env {
		if _, ok := allowed[key]; !ok {
			return Lease{}, fmt.Errorf("web sandbox denied env %q: not declared for this skill", key)
		}
	}

	lease := Lease{
		ID:        fmt.Sprintf("lease-%d", time.Now().UnixNano()),
		AgentID:   buildAgentID(scope),
		TaskID:    fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Scopes:    []string{string(action.Type)},
		Env:       env,
		ExpiresAt: time.Now().Add(b.ttl),
	}

	b.mu.Lock()
	b.live[lease.ID] = lease
	b.mu.Unlock()
	b.audit.Record(AuditEvent{
		Timestamp: time.Now().UTC(),
		Event:     "identity_issued",
		SessionID: scope.SessionID,
		SkillName: scope.SkillName,
		AgentID:   lease.AgentID,
		TaskID:    lease.TaskID,
		Action:    string(action.Type),
		Metadata: map[string]string{
			"expires_at": lease.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})

	return lease, nil
}

func (b *IdentityBroker) Revoke(leaseID string, scope Scope) {
	if b == nil || strings.TrimSpace(leaseID) == "" {
		return
	}
	b.mu.Lock()
	lease, ok := b.live[leaseID]
	if ok {
		delete(b.live, leaseID)
	}
	b.mu.Unlock()
	if !ok {
		return
	}
	b.audit.Record(AuditEvent{
		Timestamp: time.Now().UTC(),
		Event:     "identity_revoked",
		SessionID: scope.SessionID,
		SkillName: scope.SkillName,
		AgentID:   lease.AgentID,
		TaskID:    lease.TaskID,
	})
}

func buildAgentID(scope Scope) string {
	switch {
	case strings.TrimSpace(scope.SkillName) != "":
		return "web-skill:" + scope.SkillName
	case strings.TrimSpace(scope.SessionID) != "":
		return "web-session:" + scope.SessionID
	default:
		return "web-agent"
	}
}
