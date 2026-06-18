package agentruntime

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

func normalizeLoopWebhookSecrets(values map[string]string) map[string]string {
	out := make(map[string]string)
	for source, secret := range values {
		source = strings.ToLower(strings.TrimSpace(source))
		secret = strings.TrimSpace(secret)
		if source == "" || secret == "" {
			continue
		}
		out[source] = secret
	}
	return out
}

func (s *Server) loopWebhookSecret(source string) (string, bool) {
	if s == nil {
		return "", false
	}
	source = strings.ToLower(strings.TrimSpace(source))
	secret := strings.TrimSpace(s.loopWebhookSecrets[source])
	if secret == "" {
		return "", false
	}
	return secret, true
}

func validLoopWebhookSignature(secret string, body []byte, header http.Header) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false
	}
	signature := firstNonEmptyString(
		header.Get("X-Agentapi-Webhook-Signature"),
		header.Get("X-Loop-Webhook-Signature"),
		header.Get("X-Hub-Signature-256"),
	)
	signature = strings.TrimSpace(strings.TrimPrefix(signature, "sha256="))
	if signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(signature)), []byte(expected))
}
