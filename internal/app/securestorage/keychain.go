package securestorage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/app/config"
)

var ErrUnsupportedPlatform = errors.New("secure storage keychain backend is only supported on darwin")

type SecurityRunner func(args ...string) ([]byte, error)

type KeychainStore struct {
	serviceName string
	accountName string
	run         SecurityRunner
}

func NewKeychainStore(serviceName, accountName string) *KeychainStore {
	return &KeychainStore{
		serviceName: serviceName,
		accountName: accountName,
		run:         runSecurityCommand,
	}
}

func (s *KeychainStore) Name() string {
	return "keychain"
}

func (s *KeychainStore) Read() (Data, error) {
	output, err := s.run(
		"find-generic-password",
		"-a", s.accountName,
		"-w",
		"-s", s.serviceName,
	)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(output))) == 0 {
		return nil, nil
	}

	var data Data
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, err
	}
	if data == nil {
		return Data{}, nil
	}
	return data, nil
}

func (s *KeychainStore) Write(data Data) (WriteResult, error) {
	if data == nil {
		data = Data{}
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return WriteResult{}, err
	}

	if _, err := s.run(
		"add-generic-password",
		"-U",
		"-a", s.accountName,
		"-s", s.serviceName,
		"-w", string(payload),
	); err != nil {
		return WriteResult{}, err
	}

	return WriteResult{}, nil
}

func (s *KeychainStore) Delete() error {
	_, err := s.run(
		"delete-generic-password",
		"-a", s.accountName,
		"-s", s.serviceName,
	)
	return err
}

func NewDefaultStore() (Store, error) {
	path, err := DefaultCredentialsPath()
	if err != nil {
		return nil, err
	}

	plaintext := NewPlaintextStore(path)
	if runtime.GOOS != "darwin" {
		return plaintext, nil
	}

	return NewFallbackStore(
		NewKeychainStore(DefaultServiceName(), DefaultAccountName()),
		plaintext,
	), nil
}

func DefaultCredentialsPath() (string, error) {
	appHome, err := config.AppHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(appHome, ".credentials.json"), nil
}

func DefaultServiceName() string {
	appHome, err := config.AppHome()
	if err != nil {
		return "Claude Go"
	}

	defaultHome := defaultAppHome()
	if appHome == defaultHome {
		return "Claude Go"
	}

	sum := sha256.Sum256([]byte(appHome))
	return fmt.Sprintf("Claude Go-%s", hex.EncodeToString(sum[:4]))
}

func DefaultAccountName() string {
	if current := os.Getenv("USER"); strings.TrimSpace(current) != "" {
		return current
	}
	if current, err := user.Current(); err == nil && strings.TrimSpace(current.Username) != "" {
		return current.Username
	}
	return "claude-go-user"
}

func defaultAppHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude-go")
}

func runSecurityCommand(args ...string) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, ErrUnsupportedPlatform
	}

	cmd := exec.Command("security", args...)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if strings.Contains(stderr, "could not be found") || strings.Contains(stderr, "The specified item could not be found") {
				if len(args) > 0 && args[0] == "delete-generic-password" {
					return nil, nil
				}
				return nil, nil
			}
			return nil, fmt.Errorf("security %s failed: %s", strings.Join(args, " "), stderr)
		}
		return nil, err
	}
	return bytesTrimSpace(output), nil
}

func bytesTrimSpace(in []byte) []byte {
	return []byte(strings.TrimSpace(string(in)))
}
