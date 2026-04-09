package migrations

import (
	"fmt"
	"sort"
	"sync"
)

// Registry manages all available migrations
type Registry struct {
	migrations []Migration
	mu         sync.RWMutex
}

// NewRegistry creates a new migration registry
func NewRegistry() *Registry {
	return &Registry{
		migrations: make([]Migration, 0),
	}
}

// Register adds a migration to the registry
func (r *Registry) Register(m Migration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for duplicate version
	for _, existing := range r.migrations {
		if existing.Version == m.Version {
			return fmt.Errorf("migration version %d already registered", m.Version)
		}
	}

	r.migrations = append(r.migrations, m)
	return nil
}

// GetMigrations returns all migrations sorted by version
func (r *Registry) GetMigrations() []Migration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Create a copy and sort by version
	result := make([]Migration, len(r.migrations))
	copy(result, r.migrations)

	sort.Slice(result, func(i, j int) bool {
		return result[i].Version < result[j].Version
	})

	return result
}

// GetMigration returns a specific migration by version
func (r *Registry) GetMigration(version int) (*Migration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, m := range r.migrations {
		if m.Version == version {
			return &m, nil
		}
	}

	return nil, fmt.Errorf("migration version %d not found", version)
}

// GetPendingMigrations returns migrations that need to be applied
func (r *Registry) GetPendingMigrations(currentVersion int) []Migration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pending := make([]Migration, 0)
	for _, m := range r.migrations {
		if m.Version > currentVersion {
			pending = append(pending, m)
		}
	}

	// Sort by version
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].Version < pending[j].Version
	})

	return pending
}

// DefaultRegistry is the global migration registry
var DefaultRegistry = NewRegistry()

// Register registers a migration in the default registry
func Register(m Migration) error {
	return DefaultRegistry.Register(m)
}

// MustRegister registers a migration and panics on error
func MustRegister(m Migration) {
	if err := Register(m); err != nil {
		panic(err)
	}
}

// init registers all migrations
func init() {
	// Migrations are registered in their individual files
	// This ensures they're loaded when the package is imported
}
