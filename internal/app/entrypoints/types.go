package entrypoints

import (
	"context"
	"time"
)

// InitPhase represents different initialization phases
type InitPhase string

const (
	PhaseConfigSystem   InitPhase = "config_system"
	PhaseBackgroundSvc  InitPhase = "background_services"
	PhaseRemoteSettings InitPhase = "remote_settings"
	PhaseNetwork        InitPhase = "network"
	PhaseTrustGranted   InitPhase = "trust_granted"
)

// InitOptions configures the initialization process
type InitOptions struct {
	// ConfigDir is the configuration directory path
	ConfigDir string

	// IsNonInteractive indicates if this is a non-interactive session
	IsNonInteractive bool

	// SkipTelemetry disables telemetry initialization
	SkipTelemetry bool

	// SkipMigrations disables migration execution
	SkipMigrations bool

	// EnableRemoteSettings enables remote managed settings
	EnableRemoteSettings bool

	// EnablePolicyLimits enables policy limits
	EnablePolicyLimits bool
}

// InitResult contains the result of initialization
type InitResult struct {
	// Success indicates if initialization succeeded
	Success bool

	// Phase is the last completed phase
	Phase InitPhase

	// Duration is the total initialization time
	Duration time.Duration

	// Error contains any error that occurred
	Error error
}

// PhaseResult contains the result of a single phase
type PhaseResult struct {
	Phase    InitPhase
	Duration time.Duration
	Error    error
}

// InitContext holds initialization state
type InitContext struct {
	ctx     context.Context
	options *InitOptions
	results []PhaseResult
	startTime time.Time
}

// ConfigManager manages configuration loading and validation
type ConfigManager interface {
	EnableConfigs(ctx context.Context) error
	ApplySafeEnvVars(ctx context.Context) error
	ApplyAllEnvVars(ctx context.Context) error
	ApplyCACerts(ctx context.Context) error
}

// MigrationManager manages configuration migrations
type MigrationManager interface {
	ExecuteMigrations(ctx context.Context) error
}

// TelemetryManager manages telemetry initialization
type TelemetryManager interface {
	Initialize(ctx context.Context) error
	IsEnabled() bool
}

// NetworkManager manages network configuration
type NetworkManager interface {
	ConfigureProxy(ctx context.Context) error
	ConfigureMTLS(ctx context.Context) error
	PreconnectAPI(ctx context.Context) error
}

// ServiceManager manages background services
type ServiceManager interface {
	InitializeOAuth(ctx context.Context) error
	InitializeAnalytics(ctx context.Context) error
	DetectRepository(ctx context.Context) error
	DetectIDE(ctx context.Context) error
}

// ShutdownManager manages graceful shutdown
type ShutdownManager interface {
	Setup(ctx context.Context) error
	RegisterCleanup(fn func() error)
	Shutdown(ctx context.Context) error
}

// Initializer is the main initialization coordinator
type Initializer struct {
	config    ConfigManager
	migration MigrationManager
	telemetry TelemetryManager
	network   NetworkManager
	service   ServiceManager
	shutdown  ShutdownManager
}

// NewInitializer creates a new initializer
func NewInitializer(
	config ConfigManager,
	migration MigrationManager,
	telemetry TelemetryManager,
	network NetworkManager,
	service ServiceManager,
	shutdown ShutdownManager,
) *Initializer {
	return &Initializer{
		config:    config,
		migration: migration,
		telemetry: telemetry,
		network:   network,
		service:   service,
		shutdown:  shutdown,
	}
}
