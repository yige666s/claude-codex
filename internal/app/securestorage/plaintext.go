package securestorage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"claude-codex/internal/public/fsutil"
)

const plaintextWarning = "warning: storing credentials in plaintext"

type PlaintextStore struct {
	path string
}

func NewPlaintextStore(path string) *PlaintextStore {
	return &PlaintextStore{path: path}
}

func (s *PlaintextStore) Name() string {
	return "plaintext"
}

func (s *PlaintextStore) Path() string {
	return s.path
}

func (s *PlaintextStore) Read() (Data, error) {
	payload, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return Data{}, nil
	}

	var data Data
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, err
	}
	if data == nil {
		return Data{}, nil
	}
	return data, nil
}

func (s *PlaintextStore) Write(data Data) (WriteResult, error) {
	if data == nil {
		data = Data{}
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return WriteResult{}, err
	}
	if err := fsutil.WriteFileAtomic(s.path, payload, 0o600); err != nil {
		return WriteResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Warning: plaintextWarning}, nil
}

func (s *PlaintextStore) Delete() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
