package websandbox

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Runtime struct {
	scope    Scope
	identity *IdentityBroker
	docker   *DockerExecutor
	audit    AuditSink
}

func NewRuntime(scope Scope, opts RuntimeOptions) *Runtime {
	audit := opts.AuditSink
	if audit == nil {
		audit = LogAuditSink{}
	}
	identity := opts.IdentityBroker
	if identity == nil {
		identity = NewIdentityBroker(15*time.Minute, audit)
	}
	return &Runtime{
		scope:    scope,
		identity: identity,
		docker:   NewDockerExecutor(opts, audit),
		audit:    audit,
	}
}

func (r *Runtime) ValidateCommand(command string) error {
	_, err := ParseAction(r.scope, command)
	if err != nil {
		r.audit.Record(AuditEvent{
			Timestamp: time.Now().UTC(),
			Event:     "command_denied",
			SessionID: r.scope.SessionID,
			SkillName: r.scope.SkillName,
			Command:   command,
		})
	}
	return err
}

func (r *Runtime) ExecuteCommand(ctx context.Context, command string) (string, error) {
	action, err := ParseAction(r.scope, command)
	if err != nil {
		r.audit.Record(AuditEvent{
			Timestamp: time.Now().UTC(),
			Event:     "command_denied",
			SessionID: r.scope.SessionID,
			SkillName: r.scope.SkillName,
			Command:   command,
		})
		return "", err
	}
	lease, err := r.identity.Issue(ctx, r.scope, action)
	if err != nil {
		return "", err
	}
	defer r.identity.Revoke(lease.ID, r.scope)

	output, err := r.docker.Execute(ctx, r.scope, action, lease)
	if err != nil {
		r.audit.Record(AuditEvent{
			Timestamp: time.Now().UTC(),
			Event:     "command_failed",
			SessionID: r.scope.SessionID,
			SkillName: r.scope.SkillName,
			AgentID:   lease.AgentID,
			TaskID:    lease.TaskID,
			Command:   command,
			Action:    string(action.Type),
		})
		return "", err
	}
	r.audit.Record(AuditEvent{
		Timestamp: time.Now().UTC(),
		Event:     "command_completed",
		SessionID: r.scope.SessionID,
		SkillName: r.scope.SkillName,
		AgentID:   lease.AgentID,
		TaskID:    lease.TaskID,
		Command:   command,
		Action:    string(action.Type),
		Metadata: map[string]string{
			"output_chars": fmt.Sprintf("%d", len(strings.TrimSpace(output))),
		},
	})
	return output, nil
}
