package x402

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

func ParsePaymentRequirement(header string) (*PaymentRequirement, error) {
	if strings.TrimSpace(header) == "" {
		return nil, errors.New("payment requirement header is empty")
	}
	payload, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		payload = []byte(header)
	}
	var requirement PaymentRequirement
	if err := json.Unmarshal(payload, &requirement); err != nil {
		return nil, err
	}
	return &requirement, nil
}

func ValidatePaymentRequirement(requirement *PaymentRequirement, cfg WalletConfig) error {
	if requirement == nil {
		return errors.New("payment requirement is nil")
	}
	if requirement.Network != "" && cfg.Network != "" && requirement.Network != cfg.Network {
		return errors.New("payment network mismatch")
	}
	return nil
}
