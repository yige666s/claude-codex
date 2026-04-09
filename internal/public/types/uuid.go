package types

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// UUID generates a random UUID v4.
func UUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to a simple counter-based ID if random fails
		return fmt.Sprintf("fallback-%d", generateFallbackID())
	}

	// Set version (4) and variant bits
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ShortID generates a short random ID (12 characters).
func ShortID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%d", generateFallbackID())
	}
	return hex.EncodeToString(b)
}

var fallbackCounter uint64

func generateFallbackID() uint64 {
	fallbackCounter++
	return fallbackCounter
}

// ValidateUUID checks if a string is a valid UUID.
func ValidateUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	// Simple validation - check format
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	return true
}
