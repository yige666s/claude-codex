package hooks

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Executor executes hooks with proper error handling and timeout.
type Executor struct {
	registry *Registry
	timeout  time.Duration
}

// NewExecutor creates a new hook executor.
func NewExecutor(registry *Registry) *Executor {
	return &Executor{
		registry: registry,
		timeout:  DefaultTimeout,
	}
}

// NewExecutorWithTimeout creates a new hook executor with custom timeout.
func NewExecutorWithTimeout(registry *Registry, timeout time.Duration) *Executor {
	return &Executor{
		registry: registry,
		timeout:  timeout,
	}
}

// Execute runs all hooks for an event and returns aggregated results.
func (e *Executor) Execute(ctx context.Context, event HookEvent, input *HookInput) (*AggregatedResult, error) {
	hooks := e.registry.GetHooks(event)
	if len(hooks) == 0 {
		return &AggregatedResult{Continue: true}, nil
	}

	// Separate sync and async hooks
	var syncHooks, asyncHooks []Hook
	for _, hook := range hooks {
		if hook.IsAsync() {
			asyncHooks = append(asyncHooks, hook)
		} else {
			syncHooks = append(syncHooks, hook)
		}
	}

	// Execute sync hooks sequentially
	var results []*HookResult
	for _, hook := range syncHooks {
		result, err := e.executeHook(ctx, hook, input)
		if err != nil {
			// Log error but continue with other hooks
			result = &HookResult{
				Continue:      true,
				BlockingError: err.Error(),
			}
		}
		results = append(results, result)

		// Stop if hook says not to continue
		if !result.Continue {
			break
		}
	}

	// Execute async hooks in background
	if len(asyncHooks) > 0 {
		e.executeAsyncHooks(ctx, asyncHooks, input)
	}

	// Aggregate results
	return e.aggregateResults(results), nil
}

// executeHook runs a single hook with timeout and panic recovery.
func (e *Executor) executeHook(ctx context.Context, hook Hook, input *HookInput) (result *HookResult, err error) {
	// Recover from panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hook %s panicked: %v", hook.Name(), r)
			result = &HookResult{
				Continue:      true,
				BlockingError: err.Error(),
			}
		}
	}()

	// Use hook's timeout if specified, otherwise use executor's default
	timeout := hook.Timeout()
	if timeout == 0 {
		timeout = e.timeout
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute hook
	result, err = hook.Execute(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("hook %s failed: %w", hook.Name(), err)
	}

	return result, nil
}

// executeAsyncHooks runs async hooks in background goroutines.
func (e *Executor) executeAsyncHooks(ctx context.Context, hooks []Hook, input *HookInput) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, MaxAsyncHooks)

	for _, hook := range hooks {
		wg.Add(1)
		go func(h Hook) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Execute hook (ignore result for async hooks)
			_, _ = e.executeHook(ctx, h, input)
		}(hook)
	}

	// Don't wait for async hooks to complete
	go func() {
		wg.Wait()
	}()
}

// aggregateResults combines results from multiple hooks.
func (e *Executor) aggregateResults(results []*HookResult) *AggregatedResult {
	aggregated := &AggregatedResult{
		Continue:           true,
		UpdatedInput:       make(map[string]any),
		AdditionalContexts: []string{},
		BlockingErrors:     []string{},
		WatchPaths:         []string{},
	}

	for _, result := range results {
		// Any hook can stop execution
		if !result.Continue {
			aggregated.Continue = false
			if result.StopReason != "" {
				aggregated.StopReason = result.StopReason
			}
		}

		// Collect system messages (last one wins)
		if result.SystemMessage != "" {
			aggregated.SystemMessage = result.SystemMessage
		}

		// Collect additional contexts
		if result.AdditionalContext != "" {
			aggregated.AdditionalContexts = append(aggregated.AdditionalContexts, result.AdditionalContext)
		}

		// Merge updated input (later hooks override earlier ones)
		if result.UpdatedInput != nil {
			for k := range result.UpdatedInput {
				aggregated.UpdatedInput[k] = result.UpdatedInput[k]
			}
		}

		// Permission decision (first deny wins, then ask, then allow)
		if result.PermissionDecision != nil {
			if aggregated.PermissionBehavior == "" {
				aggregated.PermissionBehavior = result.PermissionDecision.Behavior
				aggregated.PermissionDecisionReason = result.PermissionDecision.Reason
			} else if result.PermissionDecision.Behavior == "deny" {
				aggregated.PermissionBehavior = "deny"
				aggregated.PermissionDecisionReason = result.PermissionDecision.Reason
			} else if result.PermissionDecision.Behavior == "ask" && aggregated.PermissionBehavior == "allow" {
				aggregated.PermissionBehavior = "ask"
				aggregated.PermissionDecisionReason = result.PermissionDecision.Reason
			}
			if len(result.PermissionDecision.UpdatedPermissions) > 0 {
				aggregated.PermissionUpdates = append(aggregated.PermissionUpdates, result.PermissionDecision.UpdatedPermissions...)
			}
		}

		// Collect MCP tool output (last one wins)
		if result.UpdatedMCPToolOutput != nil {
			aggregated.UpdatedMCPToolOutput = result.UpdatedMCPToolOutput
		}

		// Collect blocking errors
		if result.BlockingError != "" {
			aggregated.BlockingErrors = append(aggregated.BlockingErrors, result.BlockingError)
		}

		// Collect initial user message (first one wins)
		if result.InitialUserMessage != "" && aggregated.InitialUserMessage == "" {
			aggregated.InitialUserMessage = result.InitialUserMessage
		}

		// Collect watch paths
		if len(result.WatchPaths) > 0 {
			aggregated.WatchPaths = append(aggregated.WatchPaths, result.WatchPaths...)
		}

		// Retry flag (any hook can trigger retry)
		if result.Retry {
			aggregated.Retry = true
		}
	}

	return aggregated
}
