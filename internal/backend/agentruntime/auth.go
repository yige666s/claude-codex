package agentruntime

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type CompositeAuthenticator []Authenticator

func (a CompositeAuthenticator) Authenticate(r *http.Request) (User, error) {
	var lastErr error
	for _, auth := range a {
		if auth == nil {
			continue
		}
		user, err := auth.Authenticate(r)
		if err == nil {
			return user, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return User{}, lastErr
	}
	return User{}, fmt.Errorf("authenticator is not configured")
}

type HeaderAuthenticator struct {
	UserHeader  string
	BearerToken string
}

func (a HeaderAuthenticator) Authenticate(r *http.Request) (User, error) {
	if strings.TrimSpace(a.BearerToken) != "" {
		got := bearerToken(r)
		if got == "" {
			got = queryToken(r)
		}
		if got != a.BearerToken {
			return User{}, fmt.Errorf("unauthorized")
		}
	}
	header := a.UserHeader
	if header == "" {
		header = "X-User-ID"
	}
	userID := strings.TrimSpace(r.Header.Get(header))
	if userID == "" {
		userID = strings.TrimSpace(r.URL.Query().Get("user_id"))
	}
	if userID == "" {
		return User{}, fmt.Errorf("%s header is required", header)
	}
	return User{ID: userID}, nil
}

type TrustedHeaderAuthenticator struct {
	UserHeader     string
	RequiredHeader string
	RequiredValue  string
}

func (a TrustedHeaderAuthenticator) Authenticate(r *http.Request) (User, error) {
	if a.RequiredHeader != "" && r.Header.Get(a.RequiredHeader) != a.RequiredValue {
		return User{}, fmt.Errorf("trusted user gateway header is invalid")
	}
	header := a.UserHeader
	if header == "" {
		header = "X-User-ID"
	}
	userID := strings.TrimSpace(r.Header.Get(header))
	if userID == "" {
		return User{}, fmt.Errorf("%s header is required", header)
	}
	return User{ID: userID}, nil
}

type JWTAuthenticator struct {
	Secret    string
	UserClaim string
	Issuer    string
	Audience  string
	Leeway    time.Duration
}

func (a JWTAuthenticator) Authenticate(r *http.Request) (User, error) {
	token := bearerToken(r)
	if token == "" {
		token = queryToken(r)
	}
	if token == "" {
		return User{}, fmt.Errorf("bearer token is required")
	}
	return a.authenticateToken(token)
}

type SessionCookieAuthenticator struct {
	CookieName string
	JWTAuthenticator
}

func (a SessionCookieAuthenticator) Authenticate(r *http.Request) (User, error) {
	name := a.CookieName
	if name == "" {
		name = "agentapi_session"
	}
	cookie, err := r.Cookie(name)
	if err != nil {
		return User{}, fmt.Errorf("session cookie is required")
	}
	return a.authenticateToken(cookie.Value)
}

type jwtClaims map[string]any

func (a JWTAuthenticator) authenticateToken(token string) (User, error) {
	if strings.TrimSpace(a.Secret) == "" {
		return User{}, fmt.Errorf("JWT secret is required")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return User{}, fmt.Errorf("invalid JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return User{}, fmt.Errorf("invalid JWT header")
	}
	var header map[string]any
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return User{}, fmt.Errorf("invalid JWT header")
	}
	if alg, _ := header["alg"].(string); alg != "HS256" {
		return User{}, fmt.Errorf("unsupported JWT alg")
	}
	mac := hmac.New(sha256.New, []byte(a.Secret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(got, want) {
		return User{}, fmt.Errorf("invalid JWT signature")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return User{}, fmt.Errorf("invalid JWT payload")
	}
	var claims jwtClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return User{}, fmt.Errorf("invalid JWT payload")
	}
	if err := a.validateClaims(claims); err != nil {
		return User{}, err
	}
	claim := a.UserClaim
	if claim == "" {
		claim = "sub"
	}
	userID, _ := claims[claim].(string)
	if strings.TrimSpace(userID) == "" {
		return User{}, fmt.Errorf("JWT user claim %q is required", claim)
	}
	return User{ID: userID}, nil
}

func (a JWTAuthenticator) validateClaims(claims jwtClaims) error {
	now := time.Now().UTC()
	leeway := a.Leeway
	if leeway <= 0 {
		leeway = 30 * time.Second
	}
	if exp, ok := numericClaim(claims, "exp"); ok && now.After(time.Unix(exp, 0).Add(leeway)) {
		return fmt.Errorf("JWT is expired")
	}
	if nbf, ok := numericClaim(claims, "nbf"); ok && now.Add(leeway).Before(time.Unix(nbf, 0)) {
		return fmt.Errorf("JWT is not valid yet")
	}
	if a.Issuer != "" {
		iss, _ := claims["iss"].(string)
		if iss != a.Issuer {
			return fmt.Errorf("JWT issuer is invalid")
		}
	}
	if a.Audience != "" && !audienceMatches(claims["aud"], a.Audience) {
		return fmt.Errorf("JWT audience is invalid")
	}
	return nil
}

func numericClaim(claims jwtClaims, key string) (int64, bool) {
	switch value := claims[key].(type) {
	case float64:
		return int64(value), true
	case json.Number:
		n, err := value.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

func audienceMatches(value any, want string) bool {
	switch aud := value.(type) {
	case string:
		return aud == want
	case []any:
		for _, item := range aud {
			if s, _ := item.(string); s == want {
				return true
			}
		}
	}
	return false
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[len("bearer "):])
	}
	if token := webSocketProtocolBearerToken(r); token != "" {
		return token
	}
	return ""
}

func queryToken(r *http.Request) string {
	return ""
}

func webSocketProtocolBearerToken(r *http.Request) string {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return ""
	}
	protocols := strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",")
	for i, protocol := range protocols {
		if strings.TrimSpace(protocol) != "agentapi.bearer" || i+1 >= len(protocols) {
			continue
		}
		token, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(protocols[i+1]))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(token))
	}
	return ""
}
