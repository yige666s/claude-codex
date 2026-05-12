package agentruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type Artifact struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	UserID      string     `json:"user_id,omitempty"`
	SessionID   string     `json:"session_id,omitempty"`
	JobID       string     `json:"job_id,omitempty"`
	ObjectKey   string     `json:"object_key,omitempty"`
	Filename    string     `json:"filename"`
	ContentType string     `json:"content_type"`
	SizeBytes   int64      `json:"size_bytes"`
	CreatedAt   time.Time  `json:"created_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

const (
	AssetKindArtifact   = "artifact"
	AssetKindAttachment = "attachment"
)

type ArtifactStore interface {
	Create(ctx context.Context, artifact *Artifact) error
	Get(ctx context.Context, userID, artifactID, kind string) (*Artifact, error)
	List(ctx context.Context, userID, sessionID, kind string) ([]*Artifact, error)
	MarkDeleted(ctx context.Context, userID, artifactID, kind string, at time.Time) error
	DeleteSession(ctx context.Context, userID, sessionID string) ([]*Artifact, error)
	DeleteUser(ctx context.Context, userID string) ([]*Artifact, error)
	PruneDeletedBefore(ctx context.Context, cutoff time.Time) (int, error)
}

type ArtifactService struct {
	Store   ArtifactStore
	Objects ObjectStore
	Prefix  string
	Policy  AssetPolicy
}

func NewArtifactService(store ArtifactStore, objects ObjectStore, prefix string) *ArtifactService {
	return NewArtifactServiceWithPolicy(store, objects, prefix, AssetPolicy{})
}

func NewArtifactServiceWithPolicy(store ArtifactStore, objects ObjectStore, prefix string, policy AssetPolicy) *ArtifactService {
	return &ArtifactService{Store: store, Objects: objects, Prefix: strings.Trim(prefix, "/"), Policy: policy.withDefaults()}
}

func (s *ArtifactService) MaxBytes() int64 {
	if s == nil {
		return DefaultMaxAssetBytes
	}
	return s.Policy.withDefaults().MaxBytes
}

func (s *ArtifactService) Create(ctx context.Context, kind, userID, sessionID, filename, contentType string, data []byte) (*Artifact, error) {
	return s.CreateWithJob(ctx, kind, userID, sessionID, jobIDFromContext(ctx), filename, contentType, data)
}

func (s *ArtifactService) CreateWithJob(ctx context.Context, kind, userID, sessionID, jobID, filename, contentType string, data []byte) (*Artifact, error) {
	if s == nil || s.Store == nil || s.Objects == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	kind = normalizeAssetKind(kind)
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if strings.TrimSpace(filename) == "" {
		filename = kind + ".txt"
	}
	filename, contentType, err := s.Policy.Validate(filename, contentType, data)
	if err != nil {
		return nil, err
	}
	id, err := newArtifactID()
	if err != nil {
		return nil, err
	}
	key := joinObjectKey(s.Prefix, "users", userPathID(userID), kind+"s", id, filepath.Base(filename))
	artifact := &Artifact{
		ID:          id,
		Kind:        kind,
		UserID:      userID,
		SessionID:   sessionID,
		JobID:       jobID,
		ObjectKey:   key,
		Filename:    filepath.Base(filename),
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.Objects.Put(ctx, key, data, contentType); err != nil {
		return nil, err
	}
	if err := s.Store.Create(ctx, artifact); err != nil {
		_ = s.Objects.Delete(ctx, key)
		return nil, err
	}
	return artifact, nil
}

func (s *ArtifactService) Get(ctx context.Context, userID, artifactID, kind string) (*Artifact, []byte, error) {
	if s == nil || s.Store == nil || s.Objects == nil {
		return nil, nil, fmt.Errorf("artifact service is not configured")
	}
	artifact, err := s.Store.Get(ctx, userID, artifactID, normalizeAssetKind(kind))
	if err != nil {
		return nil, nil, err
	}
	data, err := s.Objects.Get(ctx, artifact.ObjectKey)
	if err != nil {
		return nil, nil, err
	}
	return artifact, data, nil
}

func (s *ArtifactService) List(ctx context.Context, userID, sessionID, kind string) ([]*Artifact, error) {
	if s == nil || s.Store == nil {
		return []*Artifact{}, nil
	}
	return s.Store.List(ctx, userID, sessionID, normalizeAssetKind(kind))
}

func (s *ArtifactService) Delete(ctx context.Context, userID, artifactID, kind string) error {
	if s == nil || s.Store == nil || s.Objects == nil {
		return nil
	}
	kind = normalizeAssetKind(kind)
	artifact, err := s.Store.Get(ctx, userID, artifactID, kind)
	if err != nil {
		return err
	}
	if err := s.Objects.Delete(ctx, artifact.ObjectKey); err != nil {
		return err
	}
	return s.Store.MarkDeleted(ctx, userID, artifactID, kind, time.Now().UTC())
}

func (s *ArtifactService) DeleteSession(ctx context.Context, userID, sessionID string) error {
	if s == nil || s.Store == nil || s.Objects == nil {
		return nil
	}
	items, err := s.Store.DeleteSession(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.Objects.Delete(ctx, item.ObjectKey); err != nil {
			return err
		}
	}
	return nil
}

func (s *ArtifactService) DeleteUser(ctx context.Context, userID string) error {
	if s == nil || s.Store == nil || s.Objects == nil {
		return nil
	}
	items, err := s.Store.DeleteUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.Objects.Delete(ctx, item.ObjectKey); err != nil {
			return err
		}
	}
	return nil
}

func (s *ArtifactService) PruneDeletedBefore(ctx context.Context, cutoff time.Time) (int, error) {
	if s == nil || s.Store == nil {
		return 0, nil
	}
	return s.Store.PruneDeletedBefore(ctx, cutoff)
}

func newArtifactID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(data[:8]), nil
}

func normalizeAssetKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case AssetKindAttachment:
		return AssetKindAttachment
	default:
		return AssetKindArtifact
	}
}
