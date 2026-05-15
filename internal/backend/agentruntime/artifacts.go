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

type PresignedAttachmentUpload struct {
	AttachmentID string            `json:"attachment_id"`
	UploadURL    string            `json:"upload_url"`
	Method       string            `json:"method"`
	Headers      map[string]string `json:"headers,omitempty"`
	ExpiresAt    time.Time         `json:"expires_at"`
	ObjectKey    string            `json:"object_key,omitempty"`
	MaxBytes     int64             `json:"max_bytes"`
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

type uploadedArtifactLister interface {
	ListUploadedArtifactsBefore(ctx context.Context, cutoff time.Time) ([]*Artifact, error)
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

func (s *ArtifactService) PresignAttachmentUpload(ctx context.Context, userID, sessionID, filename, contentType string, sizeBytes int64, ttl time.Duration) (*PresignedAttachmentUpload, error) {
	if s == nil || s.Store == nil || s.Objects == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	presigner, ok := s.Objects.(PresignPutObjectStore)
	if !ok {
		return nil, fmt.Errorf("object store does not support presigned uploads")
	}
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	filename, contentType, err := s.Policy.ValidateUpload(filename, contentType, sizeBytes)
	if err != nil {
		return nil, err
	}
	id, err := newArtifactID()
	if err != nil {
		return nil, err
	}
	key := joinObjectKey(s.Prefix, "users", userPathID(userID), AssetKindAttachment+"s", id, filepath.Base(filename))
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	uploadURL, err := presigner.PresignPut(ctx, key, ttl, contentType)
	if err != nil {
		return nil, err
	}
	return &PresignedAttachmentUpload{
		AttachmentID: id,
		UploadURL:    uploadURL,
		Method:       "PUT",
		Headers:      map[string]string{"Content-Type": contentType},
		ExpiresAt:    time.Now().UTC().Add(ttl),
		ObjectKey:    key,
		MaxBytes:     s.Policy.withDefaults().MaxBytes,
	}, nil
}

func (s *ArtifactService) ConfirmAttachmentUpload(ctx context.Context, userID, sessionID, attachmentID, filename, contentType string, sizeBytes int64) (*Artifact, error) {
	if s == nil || s.Store == nil || s.Objects == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	header, ok := s.Objects.(HeadObjectStore)
	if !ok {
		return nil, fmt.Errorf("object store does not support upload confirmation")
	}
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	attachmentID = strings.TrimSpace(attachmentID)
	if attachmentID == "" || strings.ContainsAny(attachmentID, `/\`) {
		return nil, fmt.Errorf("attachment ID is required")
	}
	if existing, err := s.Store.Get(ctx, userID, attachmentID, AssetKindAttachment); err == nil {
		return existing, nil
	}
	filename, contentType, err := s.Policy.ValidateUpload(filename, contentType, sizeBytes)
	if err != nil {
		return nil, err
	}
	key := joinObjectKey(s.Prefix, "users", userPathID(userID), AssetKindAttachment+"s", attachmentID, filepath.Base(filename))
	info, err := header.Head(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("uploaded object not found: %w", err)
	}
	if info.SizeBytes < 0 {
		return nil, fmt.Errorf("uploaded object size is unavailable")
	}
	maxBytes := s.Policy.withDefaults().MaxBytes
	if maxBytes > 0 && info.SizeBytes > maxBytes {
		_ = s.Objects.Delete(ctx, key)
		return nil, fmt.Errorf("file exceeds max size of %d bytes", maxBytes)
	}
	if sizeBytes > 0 && info.SizeBytes != sizeBytes {
		return nil, fmt.Errorf("uploaded object size mismatch: got %d want %d", info.SizeBytes, sizeBytes)
	}
	if normalized := normalizedContentType(info.ContentType); normalized != "" && normalized != contentType {
		return nil, fmt.Errorf("uploaded object content type mismatch: got %q want %q", normalized, contentType)
	}
	artifact := &Artifact{
		ID:          attachmentID,
		Kind:        AssetKindAttachment,
		UserID:      userID,
		SessionID:   sessionID,
		JobID:       jobIDFromContext(ctx),
		ObjectKey:   key,
		Filename:    filepath.Base(filename),
		ContentType: contentType,
		SizeBytes:   info.SizeBytes,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, artifact); err != nil {
		if existing, getErr := s.Store.Get(ctx, userID, attachmentID, AssetKindAttachment); getErr == nil {
			return existing, nil
		}
		return nil, err
	}
	return artifact, nil
}

func (s *ArtifactService) Get(ctx context.Context, userID, artifactID, kind string) (*Artifact, []byte, error) {
	if s == nil || s.Store == nil || s.Objects == nil {
		return nil, nil, fmt.Errorf("artifact service is not configured")
	}
	artifact, err := s.GetMetadata(ctx, userID, artifactID, kind)
	if err != nil {
		return nil, nil, err
	}
	data, err := s.Objects.Get(ctx, artifact.ObjectKey)
	if err != nil {
		return nil, nil, err
	}
	return artifact, data, nil
}

func (s *ArtifactService) GetMetadata(ctx context.Context, userID, artifactID, kind string) (*Artifact, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	return s.Store.Get(ctx, userID, artifactID, normalizeAssetKind(kind))
}

func (s *ArtifactService) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, bool, error) {
	if s == nil || s.Objects == nil {
		return "", false, nil
	}
	presigner, ok := s.Objects.(PresignObjectStore)
	if !ok {
		return "", false, nil
	}
	url, err := presigner.PresignGet(ctx, key, ttl)
	if err != nil {
		return "", true, err
	}
	return url, true, nil
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
