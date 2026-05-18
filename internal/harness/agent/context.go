package agent

import "context"

type agentContextKey struct{}

// AgentContext carries subagent/teammate metadata through nested harness calls.
// It is the Go equivalent of Claude Code's AsyncLocalStorage agent context,
// expressed through context.Context.
type AgentContext struct {
	AgentID         string
	ParentSessionID string
	AgentType       string
	SubagentName    string
	TeamName        string
	InvocationKind  AgentInvocationKind
	InvocationID    string
	SessionMetadata map[string]string
	RecentMessages  []string
}

func WithAgentContext(ctx context.Context, value AgentContext) context.Context {
	return context.WithValue(ctx, agentContextKey{}, value)
}

func AgentContextFrom(ctx context.Context) (AgentContext, bool) {
	value, ok := ctx.Value(agentContextKey{}).(AgentContext)
	return value, ok
}

func BuildAgentContext(agentID string, opts AgentRunOptions) AgentContext {
	kind := opts.InvocationKind
	if kind == "" {
		kind = InvocationSubagent
	}
	agentType := ""
	if opts.Definition != nil {
		agentType = string(opts.Definition.AgentType)
	}
	return AgentContext{
		AgentID:         agentID,
		ParentSessionID: opts.ParentSessionID,
		AgentType:       agentType,
		SubagentName:    opts.AgentName,
		TeamName:        opts.TeamName,
		InvocationKind:  kind,
		InvocationID:    GenerateRequestID(string(kind), AgentID(agentID)),
	}
}
