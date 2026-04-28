package permissions

import (
	"context"
	"fmt"
)

// DecisionEnvelope is the channel payload used by bridge or remote approval
// adapters that need to resolve permissions outside the current goroutine.
type DecisionEnvelope struct {
	Request Request
	Reply   chan DecisionResponse
}

// DecisionResponse is returned through a DecisionEnvelope reply channel.
type DecisionResponse struct {
	Decision Decision
	Handled  bool
	Err      error
}

// ChannelDecisionResolver sends permission requests to an external channel and
// waits for a typed response.
type ChannelDecisionResolver struct {
	Requests chan<- DecisionEnvelope
}

func (r ChannelDecisionResolver) ResolvePermission(ctx context.Context, request Request) (Decision, bool, error) {
	if r.Requests == nil {
		return Decision{}, false, nil
	}
	reply := make(chan DecisionResponse, 1)
	envelope := DecisionEnvelope{
		Request: request,
		Reply:   reply,
	}
	select {
	case r.Requests <- envelope:
	case <-ctx.Done():
		return Decision{}, false, ctx.Err()
	}
	select {
	case response := <-reply:
		if response.Err != nil {
			return Decision{}, response.Handled, response.Err
		}
		return response.Decision, response.Handled, nil
	case <-ctx.Done():
		return Decision{}, false, fmt.Errorf("permission decision cancelled: %w", ctx.Err())
	}
}
