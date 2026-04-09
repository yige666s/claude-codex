package entrypoints

import (
	"context"
	"fmt"
	"time"
)

// Initialize runs the complete initialization sequence
func (i *Initializer) Initialize(ctx context.Context, opts *InitOptions) (*InitResult, error) {
	if opts == nil {
		opts = &InitOptions{}
	}

	initCtx := &InitContext{
		ctx:       ctx,
		options:   opts,
		results:   make([]PhaseResult, 0),
		startTime: time.Now(),
	}

	// Phase 1: Config System
	if err := i.runPhase(initCtx, PhaseConfigSystem, i.initConfigSystem); err != nil {
		return i.buildResult(initCtx, PhaseConfigSystem, err), err
	}

	// Phase 2: Background Services
	if err := i.runPhase(initCtx, PhaseBackgroundSvc, i.initBackgroundServices); err != nil {
		return i.buildResult(initCtx, PhaseBackgroundSvc, err), err
	}

	// Phase 3: Remote Settings (if enabled)
	if opts.EnableRemoteSettings || opts.EnablePolicyLimits {
		if err := i.runPhase(initCtx, PhaseRemoteSettings, i.initRemoteSettings); err != nil {
			return i.buildResult(initCtx, PhaseRemoteSettings, err), err
		}
	}

	// Phase 4: Network Configuration
	if err := i.runPhase(initCtx, PhaseNetwork, i.initNetwork); err != nil {
		return i.buildResult(initCtx, PhaseNetwork, err), err
	}

	return i.buildResult(initCtx, PhaseTrustGranted, nil), nil
}

// InitializeAfterTrust runs initialization steps that require user trust
func (i *Initializer) InitializeAfterTrust(ctx context.Context, opts *InitOptions) error {
	if opts == nil {
		opts = &InitOptions{}
	}

	// Apply full environment variables (including remote settings)
	if err := i.config.ApplyAllEnvVars(ctx); err != nil {
		return fmt.Errorf("failed to apply environment variables: %w", err)
	}

	// Initialize telemetry if enabled
	if !opts.SkipTelemetry && i.telemetry.IsEnabled() {
		if err := i.telemetry.Initialize(ctx); err != nil {
			// Log but don't fail on telemetry errors
			return fmt.Errorf("telemetry initialization failed: %w", err)
		}
	}

	return nil
}

// runPhase executes a single initialization phase
func (i *Initializer) runPhase(
	initCtx *InitContext,
	phase InitPhase,
	fn func(context.Context, *InitOptions) error,
) error {
	start := time.Now()
	err := fn(initCtx.ctx, initCtx.options)
	duration := time.Since(start)

	initCtx.results = append(initCtx.results, PhaseResult{
		Phase:    phase,
		Duration: duration,
		Error:    err,
	})

	return err
}

// initConfigSystem initializes the configuration system
func (i *Initializer) initConfigSystem(ctx context.Context, opts *InitOptions) error {
	// Enable configuration system
	if err := i.config.EnableConfigs(ctx); err != nil {
		return fmt.Errorf("failed to enable configs: %w", err)
	}

	// Apply safe environment variables (before trust dialog)
	if err := i.config.ApplySafeEnvVars(ctx); err != nil {
		return fmt.Errorf("failed to apply safe env vars: %w", err)
	}

	// Apply CA certificates early (before any TLS connections)
	if err := i.config.ApplyCACerts(ctx); err != nil {
		return fmt.Errorf("failed to apply CA certs: %w", err)
	}

	// Setup graceful shutdown
	if err := i.shutdown.Setup(ctx); err != nil {
		return fmt.Errorf("failed to setup graceful shutdown: %w", err)
	}

	// Run migrations if enabled
	if !opts.SkipMigrations && i.migration != nil {
		if err := i.migration.ExecuteMigrations(ctx); err != nil {
			// Log but don't fail on migration errors
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}

// initBackgroundServices initializes background services
func (i *Initializer) initBackgroundServices(ctx context.Context, opts *InitOptions) error {
	// Initialize analytics (fire and forget)
	go func() {
		if err := i.service.InitializeAnalytics(ctx); err != nil {
			// Log error but don't fail
		}
	}()

	// Populate OAuth account info (fire and forget)
	go func() {
		if err := i.service.InitializeOAuth(ctx); err != nil {
			// Log error but don't fail
		}
	}()

	// Detect IDE (fire and forget)
	go func() {
		if err := i.service.DetectIDE(ctx); err != nil {
			// Log error but don't fail
		}
	}()

	// Detect repository (fire and forget)
	go func() {
		if err := i.service.DetectRepository(ctx); err != nil {
			// Log error but don't fail
		}
	}()

	return nil
}

// initRemoteSettings initializes remote settings loading
func (i *Initializer) initRemoteSettings(ctx context.Context, opts *InitOptions) error {
	// This is a placeholder for remote settings initialization
	// The actual implementation would start loading remote settings
	// and policy limits in the background
	return nil
}

// initNetwork initializes network configuration
func (i *Initializer) initNetwork(ctx context.Context, opts *InitOptions) error {
	// Configure mTLS
	if err := i.network.ConfigureMTLS(ctx); err != nil {
		return fmt.Errorf("failed to configure mTLS: %w", err)
	}

	// Configure proxy
	if err := i.network.ConfigureProxy(ctx); err != nil {
		return fmt.Errorf("failed to configure proxy: %w", err)
	}

	// Preconnect to API (fire and forget)
	go func() {
		if err := i.network.PreconnectAPI(ctx); err != nil {
			// Log error but don't fail
		}
	}()

	return nil
}

// buildResult builds the final initialization result
func (i *Initializer) buildResult(
	initCtx *InitContext,
	lastPhase InitPhase,
	err error,
) *InitResult {
	return &InitResult{
		Success:  err == nil,
		Phase:    lastPhase,
		Duration: time.Since(initCtx.startTime),
		Error:    err,
	}
}

// GetPhaseResults returns the results of all completed phases
func (r *InitResult) GetPhaseResults() []PhaseResult {
	// This would be stored in InitContext and returned here
	return nil
}
