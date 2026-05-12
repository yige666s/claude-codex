package agentruntime

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultCSRFHeader = "X-CSRF-Token"

type WebSecurityConfig struct {
	CORSAllowedOrigins   []string
	CORSAllowedHeaders   []string
	CORSAllowedMethods   []string
	CORSAllowCredentials bool
	SessionCookieName    string
	CSRFTokenCookieName  string
	CSRFHeaderName       string
	CookieDomain         string
	CookiePath           string
	CookieSecure         bool
	CookieHTTPOnly       bool
	CookieSameSite       http.SameSite
	EnableCSRF           bool
}

func (c WebSecurityConfig) csrfHeaderName() string {
	if strings.TrimSpace(c.CSRFHeaderName) == "" {
		return defaultCSRFHeader
	}
	return c.CSRFHeaderName
}

func (c WebSecurityConfig) csrfCookieName() string {
	if strings.TrimSpace(c.CSRFTokenCookieName) == "" {
		return "agentapi_csrf"
	}
	return c.CSRFTokenCookieName
}

func (c WebSecurityConfig) sessionCookieName() string {
	if strings.TrimSpace(c.SessionCookieName) == "" {
		return "agentapi_session"
	}
	return c.SessionCookieName
}

func (c WebSecurityConfig) cookiePath() string {
	if strings.TrimSpace(c.CookiePath) == "" {
		return "/"
	}
	return c.CookiePath
}

func (c WebSecurityConfig) sameSite() http.SameSite {
	if c.CookieSameSite == 0 {
		return http.SameSiteLaxMode
	}
	return c.CookieSameSite
}

func (c WebSecurityConfig) cookieHTTPOnly() bool {
	if !c.CookieHTTPOnly {
		return true
	}
	return c.CookieHTTPOnly
}

func newCSRFToken() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}

func ParseSameSite(value string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	case "default":
		return http.SameSiteDefaultMode
	default:
		return http.SameSiteLaxMode
	}
}

func (s *Server) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if sameHostOrigin(r) {
		return true
	}
	if !originAllowed(origin, s.security.CORSAllowedOrigins) {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Add("Vary", "Origin")
	if s.security.CORSAllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	methods := s.security.CORSAllowedMethods
	if len(methods) == 0 {
		methods = []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"}
	}
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(methods, ", "))
	headers := s.security.CORSAllowedHeaders
	if len(headers) == 0 {
		headers = []string{"Authorization", "Content-Type", "X-User-ID", "X-Admin-Token", s.security.csrfHeaderName()}
	}
	w.Header().Set("Access-Control-Allow-Headers", strings.Join(headers, ", "))
	w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
	return true
}

func originAllowed(origin string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	for _, item := range allowed {
		item = strings.TrimSpace(item)
		if item == "*" || strings.EqualFold(origin, item) {
			return true
		}
	}
	return false
}

func (s *Server) requireCSRF(w http.ResponseWriter, r *http.Request) bool {
	if !s.security.EnableCSRF || !usesSessionCookie(r, s.security.sessionCookieName()) || csrfSafeMethod(r.Method) {
		return true
	}
	cookie, err := r.Cookie(s.security.csrfCookieName())
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "csrf token cookie is required"})
		return false
	}
	header := strings.TrimSpace(r.Header.Get(s.security.csrfHeaderName()))
	if header == "" || header != cookie.Value {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "csrf token is invalid"})
		return false
	}
	return true
}

func csrfSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func usesSessionCookie(r *http.Request, name string) bool {
	_, err := r.Cookie(name)
	return err == nil && bearerToken(r) == ""
}

func (s *Server) setAuthCookies(w http.ResponseWriter, session *AuthSession) {
	if session == nil || strings.TrimSpace(session.AccessToken) == "" {
		return
	}
	security := s.security
	http.SetCookie(w, &http.Cookie{
		Name:     security.sessionCookieName(),
		Value:    session.AccessToken,
		Path:     security.cookiePath(),
		Domain:   strings.TrimSpace(security.CookieDomain),
		Expires:  session.ExpiresAt,
		MaxAge:   int(time.Until(session.ExpiresAt).Seconds()),
		Secure:   security.CookieSecure,
		HttpOnly: security.cookieHTTPOnly(),
		SameSite: security.sameSite(),
	})
	if !security.EnableCSRF {
		return
	}
	token, err := newCSRFToken()
	if err != nil {
		return
	}
	session.CSRFToken = token
	http.SetCookie(w, &http.Cookie{
		Name:     security.csrfCookieName(),
		Value:    token,
		Path:     security.cookiePath(),
		Domain:   strings.TrimSpace(security.CookieDomain),
		MaxAge:   int((24 * time.Hour).Seconds()),
		Secure:   security.CookieSecure,
		HttpOnly: false,
		SameSite: security.sameSite(),
	})
	w.Header().Set(security.csrfHeaderName(), token)
}

func (s *Server) clearAuthCookies(w http.ResponseWriter) {
	security := s.security
	for _, name := range []string{security.sessionCookieName(), security.csrfCookieName()} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     security.cookiePath(),
			Domain:   strings.TrimSpace(security.CookieDomain),
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
			Secure:   security.CookieSecure,
			HttpOnly: name == security.sessionCookieName(),
			SameSite: security.sameSite(),
		})
	}
}

func validateCookieSecurity(config WebSecurityConfig) error {
	if config.CookieSameSite == http.SameSiteNoneMode && !config.CookieSecure {
		return fmt.Errorf("SameSite=None cookies require Secure=true")
	}
	return nil
}
