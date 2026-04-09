package migrations

import (
	"context"
	"testing"
)

func TestRegistry(t *testing.T) {
	t.Run("register migration", func(t *testing.T) {
		registry := NewRegistry()

		m := Migration{
			Version:     1,
			Name:        "test_migration",
			Description: "Test migration",
			Migrate: func(ctx context.Context) error {
				return nil
			},
		}

		err := registry.Register(m)
		if err != nil {
			t.Fatalf("failed to register migration: %v", err)
		}

		migrations := registry.GetMigrations()
		if len(migrations) != 1 {
			t.Fatalf("expected 1 migration, got %d", len(migrations))
		}

		if migrations[0].Version != 1 {
			t.Errorf("expected version 1, got %d", migrations[0].Version)
		}
	})

	t.Run("duplicate version error", func(t *testing.T) {
		registry := NewRegistry()

		m1 := Migration{
			Version:     1,
			Name:        "test1",
			Description: "Test 1",
			Migrate:     func(ctx context.Context) error { return nil },
		}

		m2 := Migration{
			Version:     1,
			Name:        "test2",
			Description: "Test 2",
			Migrate:     func(ctx context.Context) error { return nil },
		}

		if err := registry.Register(m1); err != nil {
			t.Fatalf("failed to register first migration: %v", err)
		}

		if err := registry.Register(m2); err == nil {
			t.Fatal("expected error for duplicate version, got nil")
		}
	})

	t.Run("get pending migrations", func(t *testing.T) {
		registry := NewRegistry()

		for i := 1; i <= 5; i++ {
			m := Migration{
				Version:     i,
				Name:        "test",
				Description: "Test",
				Migrate:     func(ctx context.Context) error { return nil },
			}
			if err := registry.Register(m); err != nil {
				t.Fatalf("failed to register migration %d: %v", i, err)
			}
		}

		pending := registry.GetPendingMigrations(2)
		if len(pending) != 3 {
			t.Fatalf("expected 3 pending migrations, got %d", len(pending))
		}

		// Check they're sorted
		for i, m := range pending {
			expectedVersion := i + 3
			if m.Version != expectedVersion {
				t.Errorf("expected version %d at index %d, got %d", expectedVersion, i, m.Version)
			}
		}
	})

	t.Run("get migration by version", func(t *testing.T) {
		registry := NewRegistry()

		m := Migration{
			Version:     5,
			Name:        "test",
			Description: "Test",
			Migrate:     func(ctx context.Context) error { return nil },
		}

		if err := registry.Register(m); err != nil {
			t.Fatalf("failed to register migration: %v", err)
		}

		found, err := registry.GetMigration(5)
		if err != nil {
			t.Fatalf("failed to get migration: %v", err)
		}

		if found.Version != 5 {
			t.Errorf("expected version 5, got %d", found.Version)
		}

		_, err = registry.GetMigration(99)
		if err == nil {
			t.Fatal("expected error for non-existent migration, got nil")
		}
	})
}

func TestFileVersionManager(t *testing.T) {
	t.Run("get and set version", func(t *testing.T) {
		tmpDir := t.TempDir()
		vm := NewFileVersionManager(tmpDir)
		ctx := context.Background()

		// Initial version should be 0
		version, err := vm.GetCurrentVersion(ctx)
		if err != nil {
			t.Fatalf("failed to get initial version: %v", err)
		}
		if version != 0 {
			t.Errorf("expected initial version 0, got %d", version)
		}

		// Set version
		if err := vm.SetCurrentVersion(ctx, 5); err != nil {
			t.Fatalf("failed to set version: %v", err)
		}

		// Get version again
		version, err = vm.GetCurrentVersion(ctx)
		if err != nil {
			t.Fatalf("failed to get version: %v", err)
		}
		if version != 5 {
			t.Errorf("expected version 5, got %d", version)
		}
	})
}

func TestExecutor(t *testing.T) {
	t.Run("execute migrations", func(t *testing.T) {
		registry := NewRegistry()
		tmpDir := t.TempDir()
		vm := NewFileVersionManager(tmpDir)
		logger := &NoOpAnalyticsLogger{}
		executor := NewExecutor(registry, vm, logger)

		ctx := context.Background()

		// Register test migrations
		executed := make([]int, 0)
		for i := 1; i <= 3; i++ {
			version := i
			m := Migration{
				Version:     version,
				Name:        "test",
				Description: "Test",
				Migrate: func(ctx context.Context) error {
					executed = append(executed, version)
					return nil
				},
			}
			if err := registry.Register(m); err != nil {
				t.Fatalf("failed to register migration %d: %v", i, err)
			}
		}

		// Execute migrations
		result, err := executor.Execute(ctx, nil)
		if err != nil {
			t.Fatalf("failed to execute migrations: %v", err)
		}

		if len(result.Applied) != 3 {
			t.Errorf("expected 3 applied migrations, got %d", len(result.Applied))
		}

		if len(result.Failed) != 0 {
			t.Errorf("expected 0 failed migrations, got %d", len(result.Failed))
		}

		// Check version was updated
		version, err := vm.GetCurrentVersion(ctx)
		if err != nil {
			t.Fatalf("failed to get version: %v", err)
		}
		if version != 3 {
			t.Errorf("expected version 3, got %d", version)
		}
	})

	t.Run("dry run", func(t *testing.T) {
		registry := NewRegistry()
		tmpDir := t.TempDir()
		vm := NewFileVersionManager(tmpDir)
		logger := &NoOpAnalyticsLogger{}
		executor := NewExecutor(registry, vm, logger)

		ctx := context.Background()

		m := Migration{
			Version:     1,
			Name:        "test",
			Description: "Test",
			Migrate: func(ctx context.Context) error {
				return nil
			},
		}
		if err := registry.Register(m); err != nil {
			t.Fatalf("failed to register migration: %v", err)
		}

		// Execute with dry run
		result, err := executor.Execute(ctx, &ExecuteOptions{DryRun: true})
		if err != nil {
			t.Fatalf("failed to execute migrations: %v", err)
		}

		if len(result.Applied) != 0 {
			t.Errorf("expected 0 applied migrations in dry run, got %d", len(result.Applied))
		}

		if len(result.Skipped) != 1 {
			t.Errorf("expected 1 skipped migration in dry run, got %d", len(result.Skipped))
		}

		// Version should still be 0
		version, err := vm.GetCurrentVersion(ctx)
		if err != nil {
			t.Fatalf("failed to get version: %v", err)
		}
		if version != 0 {
			t.Errorf("expected version 0 after dry run, got %d", version)
		}
	})

	t.Run("stop on error", func(t *testing.T) {
		registry := NewRegistry()
		tmpDir := t.TempDir()
		vm := NewFileVersionManager(tmpDir)
		logger := &NoOpAnalyticsLogger{}
		executor := NewExecutor(registry, vm, logger)

		ctx := context.Background()

		// Register migrations with one that fails
		for i := 1; i <= 3; i++ {
			version := i
			m := Migration{
				Version:     version,
				Name:        "test",
				Description: "Test",
				Migrate: func(ctx context.Context) error {
					if version == 2 {
						return context.Canceled
					}
					return nil
				},
			}
			if err := registry.Register(m); err != nil {
				t.Fatalf("failed to register migration %d: %v", i, err)
			}
		}

		// Execute with stop on error
		result, err := executor.Execute(ctx, &ExecuteOptions{StopOnError: true})
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if len(result.Applied) != 1 {
			t.Errorf("expected 1 applied migration, got %d", len(result.Applied))
		}

		if len(result.Failed) != 1 {
			t.Errorf("expected 1 failed migration, got %d", len(result.Failed))
		}

		// Version should be 1 (last successful)
		version, err := vm.GetCurrentVersion(ctx)
		if err != nil {
			t.Fatalf("failed to get version: %v", err)
		}
		if version != 1 {
			t.Errorf("expected version 1, got %d", version)
		}
	})
}
