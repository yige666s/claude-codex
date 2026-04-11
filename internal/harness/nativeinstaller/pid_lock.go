package nativeinstaller

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type VersionLockContent struct {
	PID        int    `json:"pid"`
	Version    string `json:"version"`
	ExecPath   string `json:"exec_path"`
	AcquiredAt int64  `json:"acquired_at"`
}

var processExists = func(pid int) bool {
	if pid <= 1 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func ReadLockContent(path string) (*VersionLockContent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var content VersionLockContent
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, err
	}
	if content.PID == 0 || content.Version == "" || content.ExecPath == "" {
		return nil, errors.New("invalid lock content")
	}
	return &content, nil
}

func IsLockActive(path string) bool {
	content, err := ReadLockContent(path)
	if err != nil {
		return false
	}
	return processExists(content.PID)
}

func WriteLockFile(path string, content VersionLockContent) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if content.AcquiredAt == 0 {
		content.AcquiredAt = time.Now().UnixMilli()
	}
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
