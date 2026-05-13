package agentruntime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3ObjectStore struct {
	client *minio.Client
	bucket string
	prefix string
}

type S3ObjectStoreConfig struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Prefix          string
	UseSSL          bool
}

func NewS3ObjectStore(ctx context.Context, cfg S3ObjectStoreConfig) (*S3ObjectStore, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("S3 endpoint is required")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("S3 bucket is required")
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, err
		}
	}
	return &S3ObjectStore{
		client: client,
		bucket: cfg.Bucket,
		prefix: strings.Trim(cfg.Prefix, "/"),
	}, nil
}

func (s *S3ObjectStore) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, s.key(key), bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *S3ObjectStore) Get(ctx context.Context, key string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, s.key(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

func (s *S3ObjectStore) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := s.key(prefix)
	opts := minio.ListObjectsOptions{Prefix: fullPrefix, Recursive: true}
	var keys []string
	for item := range s.client.ListObjects(ctx, s.bucket, opts) {
		if item.Err != nil {
			return nil, item.Err
		}
		keys = append(keys, s.trim(item.Key))
	}
	return keys, nil
}

func (s *S3ObjectStore) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, s.key(key), minio.RemoveObjectOptions{})
}

func (s *S3ObjectStore) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	u, err := s.client.PresignedGetObject(ctx, s.bucket, s.key(key), ttl, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (s *S3ObjectStore) key(key string) string {
	key = strings.Trim(strings.ReplaceAll(key, "\\", "/"), "/")
	if s.prefix == "" {
		return key
	}
	if key == "" {
		return s.prefix + "/"
	}
	return s.prefix + "/" + key
}

func (s *S3ObjectStore) trim(key string) string {
	if s.prefix == "" {
		return key
	}
	return strings.TrimPrefix(strings.TrimPrefix(key, s.prefix), "/")
}
