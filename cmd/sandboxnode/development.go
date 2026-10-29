package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxartifact"
	"claude-codex/internal/backend/agentruntime/sandboxnode"
	"claude-codex/internal/backend/agentruntime/sandboxoci"
	"claude-codex/internal/backend/agentruntime/sandboxpolicy"
	"claude-codex/internal/backend/agentruntime/sandboxstaging"
	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

type developmentConfig struct {
	Revision           string
	Profile            sandboxpolicy.Profile
	TaskRoot           string
	CacheRoot          string
	JournalPath        string
	NoncePath          string
	NodeKeyFile        string
	SourceURL          string
	SourceKeyFile      string
	LocalArtifactStore string
}

func privateFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("secret file path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("secret file must be owner-only and regular")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(content) < 32 || len(content) > 4096 {
		return nil, errors.New("invalid secret length")
	}
	for len(content) > 0 && (content[len(content)-1] == '\n' || content[len(content)-1] == '\r') {
		content = content[:len(content)-1]
	}
	if len(content) < 32 {
		return nil, errors.New("secret too short")
	}
	return content, nil
}
func serveDevelopment(ctx context.Context, addr, configPath string) error {
	if err := validateLoopbackAddress(addr); err != nil {
		return err
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	info, err := os.Lstat(configPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || len(raw) > 64<<10 {
		return errors.New("development config must be private and bounded")
	}
	var cfg developmentConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	if cfg.Revision == "" || sandboxpolicy.ValidateFileOCIProfile(cfg.Profile) != nil || cfg.TaskRoot == "" || cfg.CacheRoot == "" || cfg.JournalPath == "" || cfg.NoncePath == "" {
		return errors.New("invalid development policy")
	}
	nodeKey, err := privateFile(cfg.NodeKeyFile)
	if err != nil {
		return err
	}
	sourceKey, err := privateFile(cfg.SourceKeyFile)
	if err != nil {
		return err
	}
	for _, dir := range []string{cfg.TaskRoot, cfg.CacheRoot, filepath.Dir(cfg.JournalPath), filepath.Dir(cfg.NoncePath)} {
		if !filepath.IsAbs(dir) {
			return errors.New("development paths must be absolute")
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	lim := sandboxtransfer.DevLimits()
	maxArchive := int64(lim.MaxBytes) + int64(lim.MaxFiles)*4096 + 2048
	source, err := sandboxartifact.NewHTTPFetcher(cfg.SourceURL, string(sourceKey), maxArchive, nil)
	if err != nil {
		return err
	}
	cache := &sandboxartifact.Cache{Dir: cfg.CacheRoot, Limits: lim, Source: source, MaxEntries: 64}
	stager := sandboxnode.Stager{Root: cfg.TaskRoot, Cache: cache, Limits: lim}
	docker := sandboxoci.ExecCommandRunner{}
	recovery, err := sandboxstaging.ReconcileDevelopmentStartup(ctx, stager, cfg.JournalPath, docker)
	if err != nil {
		return err
	}
	defer recovery.Close()
	if _, journalErr := os.Lstat(cfg.JournalPath); journalErr == nil {
		if _, nonceErr := os.Lstat(cfg.NoncePath); nonceErr != nil {
			return errors.New("nonce journal missing for existing lease state")
		}
	}
	nonces, err := sandboxnode.OpenFileNonceStore(cfg.NoncePath, time.Now())
	if err != nil {
		return err
	}
	signer, err := sandboxnode.NewPersistentTaskSigner(nodeKey, nonces)
	if err != nil {
		return err
	}
	service := &sandboxstaging.StagingService{Stager: stager, Journal: recovery.Journal, Revision: cfg.Revision, Profile: cfg.Profile, MaxActive: 1}
	worker := &sandboxstaging.DevelopmentWorker{Recovery: recovery, Service: service, Driver: sandboxoci.SessionDriver{Planner: sandboxoci.StagedMountPlanner{SkillCacheRoot: cfg.CacheRoot, TaskRoot: cfg.TaskRoot}, Runner: docker}}
	handler, err := sandboxstaging.NewRecoveredDevelopmentHandler(nodeKey, worker, signer)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/tasks/", handler)
	mux.Handle("/v1/dev/", handler)
	if cfg.LocalArtifactStore != "" {
		store := sandboxartifact.Store{Dir: cfg.LocalArtifactStore, Builder: sandboxtransfer.Builder{Limits: lim}}
		artifacts, artifactErr := sandboxartifact.Handler(store, string(sourceKey))
		if artifactErr != nil {
			return artifactErr
		}
		mux.Handle("/v1/skills/", artifacts)
	}
	probe, err := sandboxnode.NewProbeHandler(nodeKey)
	if err != nil {
		return err
	}
	mux.Handle("/", probe)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	workerCtx, stop := context.WithCancel(ctx)
	defer stop()
	failures := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				if _, err := service.ReapExpired(time.Now()); err != nil {
					failures <- fmt.Errorf("reap sandbox tasks: %w", err)
					return
				}
			}
		}
	}()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	var result error
	select {
	case result = <-failures:
	case result = <-serveErr:
	case <-ctx.Done():
		result = ctx.Err()
	}
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	_, reapErr := service.ReapExpired(time.Now().Add(2 * time.Hour))
	if result == http.ErrServerClosed || errors.Is(result, context.Canceled) {
		result = nil
	}
	return errors.Join(result, shutdownErr, reapErr)
}
