package run

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestBuildAuthenticatorAutoAcceptsSessionCookieSignedWithJWTSecret(t *testing.T) {
	assertAuthenticatorAcceptsSessionCookieSignedWithJWTSecret(t, "auto")
}

func TestBuildAuthenticatorJWTAcceptsSessionCookieSignedWithJWTSecret(t *testing.T) {
	assertAuthenticatorAcceptsSessionCookieSignedWithJWTSecret(t, "jwt")
}

func assertAuthenticatorAcceptsSessionCookieSignedWithJWTSecret(t *testing.T, mode string) {
	t.Helper()
	const secret = "test-jwt-secret"
	const userID = "user-cookie"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Add(-time.Minute).Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	auth := buildAuthenticator(authConfig{
		mode:              mode,
		jwtSecret:         secret,
		sessionCookieName: "agentapi_session",
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/runs/run-1/events?stream=1", nil)
	req.AddCookie(&http.Cookie{Name: "agentapi_session", Value: signed})

	user, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("authenticate session cookie: %v", err)
	}
	if user.ID != userID {
		t.Fatalf("user ID = %q, want %q", user.ID, userID)
	}
}
