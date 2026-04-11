package x402

import (
	"crypto/rand"
	"encoding/hex"
)

func GeneratePrivateKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func IsEnabled(cfg WalletConfig) bool {
	return cfg.PrivateKey != ""
}
