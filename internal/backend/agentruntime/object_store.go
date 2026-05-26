package agentruntime

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"claude-codex/internal/backend/httpclient"
	"claude-codex/internal/public/fsutil"
)

type ObjectStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
}

type ObjectInfo struct {
	Key         string
	SizeBytes   int64
	ContentType string
	ETag        string
}

type PresignObjectStore interface {
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

type PresignPutObjectStore interface {
	PresignPut(ctx context.Context, key string, ttl time.Duration, contentType string) (string, error)
}

type HeadObjectStore interface {
	Head(ctx context.Context, key string) (ObjectInfo, error)
}

type FileObjectStore struct {
	root string
}

func NewFileObjectStore(root string) *FileObjectStore {
	return &FileObjectStore{root: root}
}

func (s *FileObjectStore) Put(_ context.Context, key string, data []byte, _ string) error {
	return fsutil.WriteFileAtomic(s.path(key), data, 0o644)
}

func (s *FileObjectStore) Get(_ context.Context, key string) ([]byte, error) {
	return os.ReadFile(s.path(key))
}

func (s *FileObjectStore) Head(_ context.Context, key string) (ObjectInfo, error) {
	info, err := os.Stat(s.path(key))
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Key: key, SizeBytes: info.Size()}, nil
}

func (s *FileObjectStore) List(_ context.Context, prefix string) ([]string, error) {
	root := s.path(prefix)
	var keys []string
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return keys, nil
		}
		return nil, err
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(keys)
	return keys, err
}

func (s *FileObjectStore) Delete(_ context.Context, key string) error {
	err := os.Remove(s.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *FileObjectStore) path(key string) string {
	key = filepath.Clean(strings.TrimPrefix(filepath.ToSlash(key), "/"))
	if key == "." || strings.HasPrefix(key, "../") || key == ".." {
		key = "_invalid"
	}
	return filepath.Join(s.root, filepath.FromSlash(key))
}

type HTTPObjectStore struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (s *HTTPObjectStore) Put(ctx context.Context, key string, data []byte, contentType string) error {
	url, err := s.url(key)
	if err != nil {
		return err
	}
	headers := s.headers()
	if contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	_, _, _, err = s.client().Data(ctx, http.MethodPut, url, data, httpclient.WithHeaders(headers))
	return err
}

func (s *HTTPObjectStore) Get(ctx context.Context, key string) ([]byte, error) {
	url, err := s.url(key)
	if err != nil {
		return nil, err
	}
	_, body, _, err := s.client().Data(ctx, http.MethodGet, url, nil, httpclient.WithHeaders(s.headers()))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (s *HTTPObjectStore) List(ctx context.Context, prefix string) ([]string, error) {
	url, err := s.url(strings.TrimRight(prefix, "/") + "/?list=1")
	if err != nil {
		return nil, err
	}
	var keys []string
	err = s.client().JSON(ctx, http.MethodGet, url, nil, &keys, httpclient.WithHeaders(s.headers()))
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *HTTPObjectStore) Delete(ctx context.Context, key string) error {
	url, err := s.url(key)
	if err != nil {
		return err
	}
	_, _, _, err = s.client().Data(ctx, http.MethodDelete, url, nil, httpclient.WithHeaders(s.headers()))
	return err
}

func (s *HTTPObjectStore) url(key string) (string, error) {
	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		return "", fmt.Errorf("object store base URL is required")
	}
	return base + "/" + strings.TrimLeft(key, "/"), nil
}

func (s *HTTPObjectStore) headers() http.Header {
	headers := make(http.Header)
	if s.Token != "" {
		headers.Set("Authorization", "Bearer "+s.Token)
	}
	return headers
}

func (s *HTTPObjectStore) client() *httpclient.Client {
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	return httpclient.New(httpclient.WithHTTPClient(client), httpclient.WithComponent("object_store"))
}
