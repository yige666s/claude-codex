package tasks

import "context"

type attachedAgentTasksKey struct{}

// WithAttachedAgentTasks makes background agent tasks follow the parent run's
// cancellation. The default CLI behavior remains detached across parent turns.
func WithAttachedAgentTasks(ctx context.Context) context.Context {
	return context.WithValue(ctx, attachedAgentTasksKey{}, true)
}

func agentTaskParentContext(ctx context.Context) context.Context {
	if attached, _ := ctx.Value(attachedAgentTasksKey{}).(bool); attached {
		return ctx
	}
	return context.WithoutCancel(ctx)
}
