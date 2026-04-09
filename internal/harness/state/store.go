package state

import (
	"sync"
)

// Listener is a function that is called when state changes
type Listener func()

// OnChange is called when state changes with old and new state
type OnChange func(newState *AppState, oldState *AppState)

// Store manages application state with subscription support
type Store struct {
	state     *AppState
	listeners map[int]Listener
	nextID    int
	onChange  OnChange
	mu        sync.RWMutex
}

// NewStore creates a new state store
func NewStore(initialState *AppState, onChange OnChange) *Store {
	if initialState == nil {
		initialState = NewAppState()
	}

	return &Store{
		state:     initialState,
		listeners: make(map[int]Listener),
		nextID:    0,
		onChange:  onChange,
	}
}

// GetState returns the current state (thread-safe)
func (s *Store) GetState() *AppState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// SetState updates the state using an updater function
func (s *Store) SetState(updater func(prev *AppState) *AppState) {
	s.mu.Lock()

	prev := s.state
	next := updater(prev)

	// Check if state actually changed (pointer comparison)
	if next == prev {
		s.mu.Unlock()
		return
	}

	s.state = next

	// Call onChange callback if provided
	if s.onChange != nil {
		s.onChange(next, prev)
	}

	// Get listeners to notify (copy to avoid holding lock during callbacks)
	listenersCopy := make([]Listener, 0, len(s.listeners))
	for _, listener := range s.listeners {
		listenersCopy = append(listenersCopy, listener)
	}

	s.mu.Unlock()

	// Notify all listeners (outside of lock to avoid deadlocks)
	for _, listener := range listenersCopy {
		listener()
	}
}

// Subscribe adds a listener that will be called when state changes
// Returns an unsubscribe function
func (s *Store) Subscribe(listener Listener) func() {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID
	s.nextID++
	s.listeners[id] = listener

	// Return unsubscribe function
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.listeners, id)
	}
}

// GetListenerCount returns the number of active listeners (for testing)
func (s *Store) GetListenerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.listeners)
}
