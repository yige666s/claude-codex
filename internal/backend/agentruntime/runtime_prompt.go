package agentruntime

import "context"

// renderManagedPrompt resolves the published admin version, with the built-in
// baseline as a fail-open fallback if the prompt store is temporarily offline.
func (r *Runtime) renderManagedPrompt(ctx context.Context, promptID, userID, sessionID, mode string, variables map[string]any) (PromptRenderResult, error) {
	resolver := NewPromptResolver(nil, nil)
	if r != nil {
		resolver = r.promptResolver
	}
	request := PromptResolveRequest{PromptID: promptID, Environment: PromptEnvironmentProduction, UserID: userID, SessionID: sessionID, RuntimeMode: mode}
	resolution, err := resolver.Resolve(ctx, request)
	if err != nil {
		resolution, err = NewPromptResolver(nil, nil).Resolve(ctx, request)
	}
	if err != nil {
		return PromptRenderResult{}, err
	}
	return RenderPrompt(resolution, variables)
}
