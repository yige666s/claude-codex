package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"claude-codex/internal/public/fsutil"
)

type ObjectStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
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
	Client  interface {
		Do(req *http.Request) (*http.Response, error)
	}
}

func (s *HTTPObjectStore) Put(ctx context.Context, key string, data []byte, contentType string) error {
	req, err := s.request(ctx, http.MethodPut, key, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return s.doNoBody(req)
}

func (s *HTTPObjectStore) Get(ctx context.Context, key string) ([]byte, error) {
	req, err := s.request(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("object get failed: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (s *HTTPObjectStore) List(ctx context.Context, prefix string) ([]string, error) {
	req, err := s.request(ctx, http.MethodGet, strings.TrimRight(prefix, "/")+"/?list=1", nil)
	if err != nil {
		return nil, err
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("object list failed: %s", resp.Status)
	}
	var keys []string
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *HTTPObjectStore) Delete(ctx context.Context, key string) error {
	req, err := s.request(ctx, http.MethodDelete, key, nil)
	if err != nil {
		return err
	}
	return s.doNoBody(req)
}

func (s *HTTPObjectStore) request(ctx context.Context, method, key string, body io.Reader) (*http.Request, error) {
	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("object store base URL is required")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/"+strings.TrimLeft(key, "/"), body)
	if err != nil {
		return nil, err
	}
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	return req, nil
}

func (s *HTTPObjectStore) doNoBody(req *http.Request) error {
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("object request failed: %s", resp.Status)
	}
	return nil
}
