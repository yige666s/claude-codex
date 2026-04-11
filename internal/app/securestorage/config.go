package securestorage

import (
	"fmt"
	"runtime"
	"strings"

	"claude-codex/internal/app/config"
)

func NewStoreFromConfig(cfg config.Config) (Store, error) {
	switch strings.TrimSpace(strings.ToLower(cfg.SecretStore)) {
	case "", "auto":
		return NewDefaultStore()
	case "plaintext":
		path, err := DefaultCredentialsPath()
		if err != nil {
			return nil, err
		}
		return NewPlaintextStore(path), nil
	case "keychain":
		if runtime.GOOS != "darwin" {
			return nil, ErrUnsupportedPlatform
		}
		return NewKeychainStore(DefaultServiceName(), DefaultAccountName()), nil
	default:
		return nil, fmt.Errorf("unsupported secret_store %q", cfg.SecretStore)
	}
}

func StartPrefetchForConfig(cfg config.Config) {
	store, err := NewStoreFromConfig(cfg)
	if err != nil {
		return
	}
	StartKeychainPrefetch(store)
}
