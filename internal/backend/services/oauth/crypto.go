package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// base64URLEncode encodes a buffer to base64 URL-safe format
func base64URLEncode(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	// Replace characters for URL-safe encoding
	encoded = replaceChars(encoded, '+', '-')
	encoded = replaceChars(encoded, '/', '_')
	// Remove padding
	for len(encoded) > 0 && encoded[len(encoded)-1] == '=' {
		encoded = encoded[:len(encoded)-1]
	}
	return encoded
}

// replaceChars replaces all occurrences of old with new in string
func replaceChars(s string, old, new byte) string {
	result := []byte(s)
	for i := range result {
		if result[i] == old {
			result[i] = new
		}
	}
	return string(result)
}

// GenerateCodeVerifier generates a random code verifier for PKCE
func GenerateCodeVerifier() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64URLEncode(bytes), nil
}

// GenerateCodeChallenge generates a code challenge from a verifier
func GenerateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64URLEncode(hash[:])
}

// GenerateState generates a random state parameter for CSRF protection
func GenerateState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64URLEncode(bytes), nil
}

// GeneratePKCEParams generates all PKCE parameters at once
func GeneratePKCEParams() (*PKCEParams, error) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		return nil, err
	}

	state, err := GenerateState()
	if err != nil {
		return nil, err
	}

	challenge := GenerateCodeChallenge(verifier)

	return &PKCEParams{
		CodeVerifier:  verifier,
		CodeChallenge: challenge,
		State:         state,
	}, nil
}
