package tui

import (
	"context"
	"errors"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
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
	if b == nil {
		return errors.New("permission broker is not configured")
	}

	reply := make(chan permissionResult, 1)
	envelope := permissionEnvelope{
		request: request,
		reply:   reply,
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case b.requests <- envelope:
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-reply:
		return result.err
	}
}

func (b *PermissionBroker) Requests() <-chan permissionEnvelope {
	if b == nil {
		return nil
	}
	return b.requests
}
