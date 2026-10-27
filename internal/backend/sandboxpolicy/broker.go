package sandboxpolicy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"claude-codex/internal/backend/sandboxnode"
)

type ActiveReader interface {
	Active(context.Context) (Revision, error)
}
type CredentialResolver func(context.Context, string) (string, error)

// SimulationBroker uses the active SQL policy and an external secret resolver.
// It may only submit identity-only dry-runs to a development node; no command
// or native execution method is reachable through this client.
type SimulationBroker struct {
	Policy            ActiveReader
	ResolveCredential CredentialResolver
	Client            *http.Client
}

func (b *SimulationBroker) Prepare(ctx context.Context, jobID, userID, idempotencyKey string, deadline time.Time) (sandboxnode.LeaseSnapshot, error) {
	if b == nil || b.Policy == nil || b.ResolveCredential == nil {
		return sandboxnode.LeaseSnapshot{}, errors.New("sandbox simulation broker is not configured")
	}
	active, err := b.Policy.Active(ctx)
	if err != nil {
		return sandboxnode.LeaseSnapshot{}, err
	}
	if err = active.Document.Validate(); err != nil {
		return sandboxnode.LeaseSnapshot{}, err
	}
	if !active.Document.Enabled {
		return sandboxnode.LeaseSnapshot{}, errors.New("sandbox policy is disabled")
	}
	profile := active.Document.Profiles[active.Document.DefaultProfile]
	pool := active.Document.NodePools[profile.NodePool]
	if pool.Environment != "development" || profile.Driver != "oci" {
		return sandboxnode.LeaseSnapshot{}, errors.New("simulation requires a development OCI pool")
	}
	token, err := b.ResolveCredential(ctx, pool.CredentialRef)
	if err != nil {
		return sandboxnode.LeaseSnapshot{}, err
	}
	client, err := sandboxnode.NewProbeClient(pool.Endpoint, token, b.Client)
	if err != nil {
		return sandboxnode.LeaseSnapshot{}, err
	}
	if err = client.Health(ctx); err != nil {
		return sandboxnode.LeaseSnapshot{}, err
	}
	caps, err := client.Capabilities(ctx)
	if err != nil {
		return sandboxnode.LeaseSnapshot{}, err
	}
	if caps.AcceptingJobs || len(caps.Drivers) > 0 {
		return sandboxnode.LeaseSnapshot{}, errors.New("simulation node must not accept execution")
	}
	spec := sandboxnode.PrepareSpec{JobID: jobID, UserID: userID, PolicyRevision: fmt.Sprintf("%d", active.ID), IdempotencyKey: idempotencyKey, Deadline: deadline}
	return client.SimulatePrepare(ctx, spec)
}
