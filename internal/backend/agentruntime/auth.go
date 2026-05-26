package agentruntime

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

func (a JWTAuthenticator) authenticateToken(token string) (User, error) {
	if strings.TrimSpace(a.Secret) == "" {
		return User{}, fmt.Errorf("JWT secret is required")
	}
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unsupported JWT alg")
		}
		return []byte(a.Secret), nil
	}, jwt.WithLeeway(a.leeway()), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return User{}, fmt.Errorf("invalid JWT")
	}
	if parsed == nil || !parsed.Valid {
		return User{}, fmt.Errorf("invalid JWT")
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

func (a JWTAuthenticator) validateClaims(claims jwt.MapClaims) error {
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

func (a JWTAuthenticator) leeway() time.Duration {
	if a.Leeway > 0 {
		return a.Leeway
	}
	return 30 * time.Second
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
	case []string:
		for _, item := range aud {
			if item == want {
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
