package tui

import (
	"context"
	"errors"

	"claude-codex/internal/harness/permissions"
)

type PermissionBroker struct {
	requests chan permissionEnvelope
}

func NewPermissionBroker() *PermissionBroker {
	return &PermissionBroker{
		requests: make(chan permissionEnvelope),
	}
}

func (b *PermissionBroker) Authorize(ctx context.Context, request permissions.Request) error {
	decision, err := b.AuthorizeDecision(ctx, request)
	if err != nil {
		return err
	}
	if decision.Behavior == permissions.BehaviorDeny {
		if decision.Reason == "" {
			decision.Reason = "permission denied"
		}
		return errors.New(decision.Reason)
	}
	return nil
}

func (b *PermissionBroker) AuthorizeDecision(ctx context.Context, request permissions.Request) (permissions.Decision, error) {
	if b == nil {
		return permissions.Decision{}, errors.New("permission broker is not configured")
	}

	reply := make(chan permissionResult, 1)
	envelope := permissionEnvelope{
		request: request,
		reply:   reply,
	}

	select {
	case <-ctx.Done():
		return permissions.Decision{}, ctx.Err()
	case b.requests <- envelope:
	}

	select {
	case <-ctx.Done():
		return permissions.Decision{}, ctx.Err()
	case result := <-reply:
		if result.err != nil {
			return permissions.Decision{}, result.err
		}
		if result.decision.Behavior == "" {
			result.decision.Behavior = permissions.BehaviorAllow
		}
		return result.decision, nil
	}
}

func (b *PermissionBroker) Requests() <-chan permissionEnvelope {
	if b == nil {
		return nil
	}
	return b.requests
}
