package run

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
)

type artifactConfig struct {
	store       string
	dataDir     string
	sql         storeConfig
	s3Endpoint  string
	s3AccessKey string
	s3SecretKey string
	s3Bucket    string
	s3Prefix    string
	s3SSL       bool
	maxBytes    int64
}

func buildArtifactService(cfg artifactConfig) *agentruntime.ArtifactService {
	if !strings.EqualFold(strings.TrimSpace(cfg.sql.backend), "sql") {
		logInfof("warning: artifact metadata requires sql store; artifacts disabled")
		return nil
	}
	db := openSQLDB(cfg.sql)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sql.sqlDialect, cfg.sql.sqlDriver))
	meta := agentruntime.NewSQLArtifactStoreWithDialect(db, dialect)
	if err := meta.Init(ctx); err != nil {
		logFatalf("init sql artifact store: %v", err)
	}
	var objects agentruntime.ObjectStore
	switch strings.ToLower(strings.TrimSpace(cfg.store)) {
	case "s3", "minio":
		store, err := agentruntime.NewS3ObjectStore(ctx, agentruntime.S3ObjectStoreConfig{
			Endpoint:        cfg.s3Endpoint,
			AccessKeyID:     cfg.s3AccessKey,
			SecretAccessKey: cfg.s3SecretKey,
			Bucket:          cfg.s3Bucket,
			Prefix:          cfg.s3Prefix,
			UseSSL:          cfg.s3SSL,
		})
		if err != nil {
			logFatalf("init artifact s3 store: %v", err)
		}
		objects = store
	default:
		objects = agentruntime.NewFileObjectStore(filepath.Join(cfg.dataDir, "artifacts"))
	}
	return agentruntime.NewArtifactServiceWithPolicy(meta, objects, "", agentruntime.AssetPolicy{MaxBytes: cfg.maxBytes})
}
