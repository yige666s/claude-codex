package entrypoints

import (
	"context"
	"testing"
	"time"
)

func TestInitializer(t *testing.T) {
	t.Run("successful initialization", func(t *testing.T) {
		initializer := NewInitializer(
			&NoOpConfigManager{},
			&NoOpMigrationManager{},
			&NoOpTelemetryManager{},
			&NoOpNetworkManager{},
			&NoOpServiceManager{},
			NewShutdownManager(),
		)

		ctx := context.Background()
		opts := &InitOptions{
			ConfigDir:        "/tmp/test",
			IsNonInteractive: false,
			SkipTelemetry:    false,
			SkipMigrations:   false,
		}

		result, err := initializer.Initialize(ctx, opts)
		if err != nil {
			t.Fatalf("initialization failed: %v", err)
		}

		if !result.Success {
			t.Error("expected success=true")
		}

		if result.Phase != PhaseTrustGranted {
			t.Errorf("expected phase=%s, got %s", PhaseTrustGranted, result.Phase)
		}

		if result.Duration == 0 {
			t.Error("expected non-zero duration")
		}
	})

	t.Run("initialization with remote settings", func(t *testing.T) {
		initializer := NewInitializer(
			&NoOpConfigManager{},
			&NoOpMigrationManager{},
			&NoOpTelemetryManager{},
			&NoOpNetworkManager{},
			&NoOpServiceManager{},
			NewShutdownManager(),
		)

		ctx := context.Background()
		opts := &InitOptions{
			EnableRemoteSettings: true,
			EnablePolicyLimits:   true,
		}

		result, err := initializer.Initialize(ctx, opts)
		if err != nil {
			t.Fatalf("initialization failed: %v", err)
		}

		if !result.Success {
			t.Error("expected success=true")
		}
	})

	t.Run("initialization with skip options", func(t *testing.T) {
		initializer := NewInitializer(
			&NoOpConfigManager{},
			&NoOpMigrationManager{},
			&NoOpTelemetryManager{},
			&NoOpNetworkManager{},
			&NoOpServiceManager{},
			NewShutdownManager(),
		)

		ctx := context.Background()
		opts := &InitOptions{
			SkipTelemetry:  true,
			SkipMigrations: true,
		}

		result, err := initializer.Initialize(ctx, opts)
		if err != nil {
			t.Fatalf("initialization failed: %v", err)
		}

		if !result.Success {
			t.Error("expected success=true")
		}
	})

	t.Run("after trust initialization", func(t *testing.T) {
		initializer := NewInitializer(
			&NoOpConfigManager{},
			&NoOpMigrationManager{},
			&NoOpTelemetryManager{},
			&NoOpNetworkManager{},
			&NoOpServiceManager{},
			NewShutdownManager(),
		)

		ctx := context.Background()
		opts := &InitOptions{
			SkipTelemetry: false,
		}

		err := initializer.InitializeAfterTrust(ctx, opts)
		if err != nil {
			t.Fatalf("after trust initialization failed: %v", err)
		}
	})
}

func TestShutdownManager(t *testing.T) {
	t.Run("register and execute cleanup", func(t *testing.T) {
		manager := NewShutdownManager()
		ctx := context.Background()

		executed := make([]int, 0)

		// Register cleanup functions
		manager.RegisterCleanup(func() error {
			executed = append(executed, 1)
			return nil
		})
		manager.RegisterCleanup(func() error {
			executed = append(executed, 2)
			return nil
		})
		manager.RegisterCleanup(func() error {
			executed = append(executed, 3)
			return nil
		})

		// Execute shutdown
		err := manager.Shutdown(ctx)
		if err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}

		// Check execution order (reverse)
		if len(executed) != 3 {
			t.Fatalf("expected 3 executions, got %d", len(executed))
		}

		if executed[0] != 3 || executed[1] != 2 || executed[2] != 1 {
			t.Errorf("expected reverse order [3,2,1], got %v", executed)
		}
	})

	t.Run("double shutdown is safe", func(t *testing.T) {
		manager := NewShutdownManager()
		ctx := context.Background()

		executed := 0
		manager.RegisterCleanup(func() error {
			executed++
			return nil
		})

		// First shutdown
		err := manager.Shutdown(ctx)
		if err != nil {
			t.Fatalf("first shutdown failed: %v", err)
		}

		// Second shutdown should be no-op
		err = manager.Shutdown(ctx)
		if err != nil {
			t.Fatalf("second shutdown failed: %v", err)
		}

		if executed != 1 {
			t.Errorf("expected 1 execution, got %d", executed)
		}
	})

	t.Run("register after shutdown executes immediately", func(t *testing.T) {
		manager := NewShutdownManager()
		ctx := context.Background()

		// Shutdown first
		err := manager.Shutdown(ctx)
		if err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}

		// Register after shutdown
		executed := false
		manager.RegisterCleanup(func() error {
			executed = true
			return nil
		})

		// Give it a moment to execute
		time.Sleep(10 * time.Millisecond)

		if !executed {
			t.Error("expected cleanup to execute immediately after shutdown")
		}
	})
}

func TestInitPhases(t *testing.T) {
	phases := []InitPhase{
		PhaseConfigSystem,
		PhaseBackgroundSvc,
		PhaseRemoteSettings,
		PhaseNetwork,
		PhaseTrustGranted,
	}

	for _, phase := range phases {
		if phase == "" {
			t.Errorf("phase should not be empty")
		}
	}
}

func TestInitOptions(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		opts := &InitOptions{}

		if opts.ConfigDir != "" {
			t.Error("expected empty config dir")
		}

		if opts.IsNonInteractive {
			t.Error("expected interactive mode")
		}

		if opts.SkipTelemetry {
			t.Error("expected telemetry enabled")
		}

		if opts.SkipMigrations {
			t.Error("expected migrations enabled")
		}
	})

	t.Run("custom options", func(t *testing.T) {
		opts := &InitOptions{
			ConfigDir:            "/custom/path",
			IsNonInteractive:     true,
			SkipTelemetry:        true,
			SkipMigrations:       true,
			EnableRemoteSettings: true,
			EnablePolicyLimits:   true,
		}

		if opts.ConfigDir != "/custom/path" {
			t.Errorf("expected /custom/path, got %s", opts.ConfigDir)
		}

		if !opts.IsNonInteractive {
			t.Error("expected non-interactive mode")
		}

		if !opts.SkipTelemetry {
			t.Error("expected telemetry disabled")
		}

		if !opts.SkipMigrations {
			t.Error("expected migrations disabled")
		}

		if !opts.EnableRemoteSettings {
			t.Error("expected remote settings enabled")
		}

		if !opts.EnablePolicyLimits {
			t.Error("expected policy limits enabled")
		}
	})
}
