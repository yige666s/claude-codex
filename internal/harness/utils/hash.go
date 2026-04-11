package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func DJB2Hash(value string) int32 {
	var hash int32
	for _, r := range value {
		hash = ((hash << 5) - hash + int32(r))
	}
	return hash
}

func HashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func HashPair(a, b string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s", a, b)))
	return hex.EncodeToString(sum[:])
}
