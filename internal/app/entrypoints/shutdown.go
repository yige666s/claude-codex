package entrypoints

import (
	"context"
	"fmt"
	"sync"
)

// DefaultShutdownManager implements ShutdownManager
type DefaultShutdownManager struct {
	cleanupFns []func() error
	mu         sync.Mutex
	isShutdown bool
}

// NewShutdownManager creates a new shutdown manager
func NewShutdownManager() *DefaultShutdownManager {
	return &DefaultShutdownManager{
		cleanupFns: make([]func() error, 0),
	}
}

// Setup initializes the shutdown manager
func (m *DefaultShutdownManager) Setup(ctx context.Context) error {
	// Register signal handlers for graceful shutdown
	// This would typically listen for SIGINT, SIGTERM, etc.
	return nil
}

// RegisterCleanup registers a cleanup function
func (m *DefaultShutdownManager) RegisterCleanup(fn func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isShutdown {
		// Already shutdown, execute immediately
		_ = fn()
		return
	}

	m.cleanupFns = append(m.cleanupFns, fn)
}

// Shutdown executes all cleanup functions
func (m *DefaultShutdownManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isShutdown {
		return nil
	}

	m.isShutdown = true

	// Execute cleanup functions in reverse order
	var errors []error
	for i := len(m.cleanupFns) - 1; i >= 0; i-- {
		if err := m.cleanupFns[i](); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("shutdown errors: %v", errors)
	}

	return nil
}

// NoOpConfigManager is a no-op config manager for testing
type NoOpConfigManager struct{}

func (m *NoOpConfigManager) EnableConfigs(ctx context.Context) error       { return nil }
func (m *NoOpConfigManager) ApplySafeEnvVars(ctx context.Context) error    { return nil }
func (m *NoOpConfigManager) ApplyAllEnvVars(ctx context.Context) error     { return nil }
func (m *NoOpConfigManager) ApplyCACerts(ctx context.Context) error        { return nil }

// NoOpMigrationManager is a no-op migration manager for testing
type NoOpMigrationManager struct{}

func (m *NoOpMigrationManager) ExecuteMigrations(ctx context.Context) error { return nil }

// NoOpTelemetryManager is a no-op telemetry manager for testing
type NoOpTelemetryManager struct{}

func (m *NoOpTelemetryManager) Initialize(ctx context.Context) error { return nil }
func (m *NoOpTelemetryManager) IsEnabled() bool                      { return false }

// NoOpNetworkManager is a no-op network manager for testing
type NoOpNetworkManager struct{}

func (m *NoOpNetworkManager) ConfigureProxy(ctx context.Context) error   { return nil }
func (m *NoOpNetworkManager) ConfigureMTLS(ctx context.Context) error    { return nil }
func (m *NoOpNetworkManager) PreconnectAPI(ctx context.Context) error    { return nil }

// NoOpServiceManager is a no-op service manager for testing
type NoOpServiceManager struct{}

func (m *NoOpServiceManager) InitializeOAuth(ctx context.Context) error      { return nil }
func (m *NoOpServiceManager) InitializeAnalytics(ctx context.Context) error  { return nil }
func (m *NoOpServiceManager) DetectRepository(ctx context.Context) error     { return nil }
func (m *NoOpServiceManager) DetectIDE(ctx context.Context) error            { return nil }
