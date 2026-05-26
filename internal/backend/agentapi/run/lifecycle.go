package run

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"claude-codex/internal/backend/agentruntime"
	workerlifecycle "claude-codex/internal/backend/workers"
)

var httpListenAndServe = func(addr string, handler *agentruntime.Server, shutdownTimeout time.Duration) error {
	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	serverGroup := workerlifecycle.New(context.Background(), nil)
	serverGroup.Start("http_server", func(context.Context) error {
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	})
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverGroup.Done():
		return err
	case <-signalCtx.Done():
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = 30 * time.Second
	}
	logInfof("shutdown signal received; draining active requests and jobs for up to %s", shutdownTimeout)
	if handler != nil {
		handler.BeginShutdown()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	runtimeGroup := workerlifecycle.New(shutdownCtx, nil)
	if handler != nil {
		runtimeGroup.Start("runtime_shutdown", func(ctx context.Context) error {
			return handler.Shutdown(ctx)
		})
	}
	httpErr := httpServer.Shutdown(shutdownCtx)
	runtimeErr := <-runtimeGroup.Done()
	serverErr := <-serverGroup.Done()
	if errors.Is(httpErr, http.ErrServerClosed) {
		httpErr = nil
	}
	if httpErr != nil || runtimeErr != nil || serverErr != nil {
		return errors.Join(httpErr, runtimeErr, serverErr)
	}
	logInfof("graceful shutdown complete")
	return nil
}

func startRetentionPruneWorker(group *workerlifecycle.Group, runtime *agentruntime.Runtime, authService *agentruntime.AuthService, retentionDays int) bool {
	if retentionDays <= 0 {
		return false
	}
	if group == nil {
		runRetentionPrune(context.Background(), runtime, authService, retentionDays)
		return true
	}
	return group.Start(workerRetentionPrune, func(ctx context.Context) error {
		runRetentionPrune(ctx, runtime, authService, retentionDays)
		return nil
	})
}

func runRetentionPrune(ctx context.Context, runtime *agentruntime.Runtime, authService *agentruntime.AuthService, retentionDays int) {
	if ctx == nil {
		ctx = context.Background()
	}
	if retentionDays <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	counts, err := runtime.PruneBefore(ctx, cutoff)
	if err != nil {
		logInfof("warning: retention prune failed: %v", err)
	} else {
		logInfof("retention prune complete: sessions=%d memories=%d", counts["sessions"], counts["memories"])
	}
	if authService != nil {
		count, err := authService.PruneExpiredRefreshTokens(ctx, cutoff)
		if err != nil {
			logInfof("warning: refresh token prune failed: %v", err)
		} else {
			logInfof("refresh token prune complete: tokens=%d", count)
		}
	}
}

func startLocalUploadedArtifactPruneWorker(group *workerlifecycle.Group, runtime *agentruntime.Runtime, retention, interval time.Duration) bool {
	if runtime == nil || retention <= 0 || interval <= 0 {
		return false
	}
	if group == nil {
		runLocalUploadedArtifactPrune(context.Background(), runtime, retention)
		return false
	}
	return group.Start(workerLocalArtifactPrune, func(ctx context.Context) error {
		runLocalUploadedArtifactPrune(ctx, runtime, retention)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				runLocalUploadedArtifactPrune(ctx, runtime, retention)
			}
		}
	})
}

func runLocalUploadedArtifactPrune(ctx context.Context, runtime *agentruntime.Runtime, retention time.Duration) {
	if runtime == nil || retention <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := runtime.PruneLocalUploadedArtifacts(ctx, retention)
	if err != nil {
		logInfof("warning: local artifact staging prune failed: checked=%d deleted=%d skipped=%d errors=%d err=%v", result.Checked, result.Deleted, result.Skipped, result.Errors, err)
		return
	}
	if result.Checked > 0 || result.Deleted > 0 || result.Errors > 0 {
		logInfof("local artifact staging prune complete: checked=%d deleted=%d skipped=%d errors=%d", result.Checked, result.Deleted, result.Skipped, result.Errors)
	}
}
