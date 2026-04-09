package hooks

import (
	"fmt"
	"sync"
)

// Registry manages hook registration and lookup.
type Registry struct {
	hooks map[HookEvent][]Hook
	mu    sync.RWMutex
}

// NewRegistry creates a new hook registry.
func NewRegistry() *Registry {
	return &Registry{
		hooks: make(map[HookEvent][]Hook),
	}
}

// Register adds a hook to the registry.
func (r *Registry) Register(hook Hook) error {
	if hook == nil {
		return fmt.Errorf("cannot register nil hook")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	event := hook.Event()
	r.hooks[event] = append(r.hooks[event], hook)

	return nil
}

// Unregister removes a hook from the registry by name and event.
func (r *Registry) Unregister(name string, event HookEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	hooks, exists := r.hooks[event]
	if !exists {
		return fmt.Errorf("no hooks registered for event %s", event)
	}

	for i, hook := range hooks {
		if hook.Name() == name {
			// Remove hook by replacing with last element and truncating
			r.hooks[event][i] = r.hooks[event][len(r.hooks[event])-1]
			r.hooks[event] = r.hooks[event][:len(r.hooks[event])-1]
			return nil
		}
	}

	return fmt.Errorf("hook %s not found for event %s", name, event)
}

// GetHooks returns all hooks registered for an event.
func (r *Registry) GetHooks(event HookEvent) []Hook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hooks := r.hooks[event]
	if len(hooks) == 0 {
		return nil
	}

	// Return a copy to prevent external modification
	result := make([]Hook, len(hooks))
	copy(result, hooks)
	return result
}

// Clear removes all hooks from the registry.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.hooks = make(map[HookEvent][]Hook)
}

// Count returns the total number of registered hooks.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, hooks := range r.hooks {
		count += len(hooks)
	}
	return count
}

// CountForEvent returns the number of hooks for a specific event.
func (r *Registry) CountForEvent(event HookEvent) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.hooks[event])
}

// Events returns all events that have registered hooks.
func (r *Registry) Events() []HookEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	events := make([]HookEvent, 0, len(r.hooks))
	for event := range r.hooks {
		events = append(events, event)
	}
	return events
}
