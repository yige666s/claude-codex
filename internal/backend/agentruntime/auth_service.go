package agentruntime

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	UserStatusActive              = "active"
	UserStatusPendingVerification = "pending_verification"
	UserStatusDisabled            = "disabled"
	UserStatusBanned              = "banned"
)

type AuthService struct {
	Store                     UserStore
	JWTSecret                 string
	Issuer                    string
	Audience                  string
	AccessTTL                 time.Duration
	RefreshTTL                time.Duration
	EmailVerificationRequired bool
	EmailVerificationTTL      time.Duration
	PasswordResetTTL          time.Duration
	PublicBaseURL             string
	Mailer                    Mailer
}

type AuthSession struct {
	User         UserProfile `json:"user"`
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	CSRFToken    string      `json:"csrf_token,omitempty"`
	ExpiresAt    time.Time   `json:"expires_at"`
}

type AuthRegistration struct {
	Session              *AuthSession `json:"session,omitempty"`
	VerificationRequired bool         `json:"verification_required,omitempty"`
	Email                string       `json:"email,omitempty"`
}

type UserProfile struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	Status          string     `json:"status"`
	EmailVerified   bool       `json:"email_verified"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
}

func (s *AuthService) Register(ctx context.Context, email, password, displayName string, r *http.Request) (*AuthRegistration, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("user system is not configured")
	}
	email = normalizeEmail(email)
	if !validEmail(email) {
		return nil, fmt.Errorf("valid email is required")
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	status := UserStatusActive
	var emailVerifiedAt *time.Time
	if s.EmailVerificationRequired {
		status = UserStatusPendingVerification
	} else {
		emailVerifiedAt = &now
	}
	user := &UserAccount{
		ID:              uuid.NewString(),
		Email:           email,
		DisplayName:     displayName,
		PasswordHash:    string(passwordHash),
		Status:          status,
		EmailVerifiedAt: emailVerifiedAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.Store.CreateUser(ctx, user); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, fmt.Errorf("email is already registered")
		}
		return nil, err
	}
	if s.EmailVerificationRequired {
		if err := s.sendVerificationEmail(ctx, user, r); err != nil {
			_ = s.Store.DeleteUser(ctx, user.ID)
			return nil, err
		}
		return &AuthRegistration{VerificationRequired: true, Email: user.Email}, nil
	}
	session, err := s.issueSession(ctx, user, r)
	if err != nil {
		return nil, err
	}
	return &AuthRegistration{Session: session}, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string, r *http.Request) (*AuthSession, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("user system is not configured")
	}
	user, err := s.Store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("invalid email or password")
	}
	if user.Status == UserStatusPendingVerification {
		return nil, fmt.Errorf("email is not verified")
	}
	if user.Status != UserStatusActive {
		return nil, fmt.Errorf("user is not active")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid email or password")
	}
	now := time.Now().UTC()
	_ = s.Store.UpdateLastLogin(ctx, user.ID, now)
	user.LastLoginAt = &now
	return s.issueSession(ctx, user, r)
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string, r *http.Request) (*AuthSession, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("user system is not configured")
	}
	hash := refreshTokenHash(refreshToken)
	rec, err := s.Store.GetRefreshToken(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token")
	}
	if rec.RevokedAt != nil || time.Now().UTC().After(rec.ExpiresAt) {
		return nil, fmt.Errorf("invalid refresh token")
	}
	user, err := s.Store.GetUserByID(ctx, rec.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token")
	}
	if user.Status == UserStatusPendingVerification {
		return nil, fmt.Errorf("email is not verified")
	}
	if user.Status != UserStatusActive {
		return nil, fmt.Errorf("user is not active")
	}
	if err := s.Store.RevokeRefreshToken(ctx, hash, time.Now().UTC()); err != nil {
		return nil, err
	}
	return s.issueSession(ctx, user, r)
}

func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	if s == nil || s.Store == nil {
		return fmt.Errorf("user system is not configured")
	}
	if strings.TrimSpace(refreshToken) == "" {
		return nil
	}
	return s.Store.RevokeRefreshToken(ctx, refreshTokenHash(refreshToken), time.Now().UTC())
}

func (s *AuthService) DeleteAccount(ctx context.Context, userID string) error {
	if s == nil || s.Store == nil {
		return fmt.Errorf("user system is not configured")
	}
	if err := s.Store.RevokeUserRefreshTokens(ctx, userID, time.Now().UTC()); err != nil {
		return err
	}
	return s.Store.DeleteUser(ctx, userID)
}

func (s *AuthService) PruneExpiredRefreshTokens(ctx context.Context, cutoff time.Time) (int, error) {
	if s == nil || s.Store == nil {
		return 0, nil
	}
	return s.Store.PruneExpiredRefreshTokens(ctx, cutoff)
}

func (s *AuthService) Me(ctx context.Context, userID string) (*UserProfile, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("user system is not configured")
	}
	user, err := s.Store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	profile := userProfile(user)
	return &profile, nil
}

func (s *AuthService) VerifyEmail(ctx context.Context, token string) (*UserProfile, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("user system is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("verification token is required")
	}
	user, err := s.Store.ConsumeEmailVerificationToken(ctx, tokenHash(token), time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("verification link is invalid or expired")
	}
	profile := userProfile(user)
	return &profile, nil
}

func (s *AuthService) RequestPasswordReset(ctx context.Context, email string, r *http.Request) error {
	if s == nil || s.Store == nil {
		return fmt.Errorf("user system is not configured")
	}
	email = normalizeEmail(email)
	if !validEmail(email) {
		return fmt.Errorf("valid email is required")
	}
	user, err := s.Store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil
	}
	if s.Mailer == nil {
		return fmt.Errorf("password reset mailer is not configured")
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	ttl := s.PasswordResetTTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	now := time.Now().UTC()
	rec := &PasswordResetTokenRecord{
		TokenHash: tokenHash(token),
		UserID:    user.ID,
		Email:     user.Email,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	if err := s.Store.CreatePasswordResetToken(ctx, rec); err != nil {
		return err
	}
	link := s.passwordResetURL(token, r)
	return s.Mailer.Send(ctx, EmailMessage{
		To:      user.Email,
		Subject: "Reset your AgentAPI password",
		HTML:    passwordResetEmailHTML(user.DisplayName, link),
		Text:    "Reset your AgentAPI password: " + link,
	})
}

func (s *AuthService) ResetPassword(ctx context.Context, token string, password string) (*UserProfile, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("user system is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("password reset token is required")
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user, err := s.Store.ConsumePasswordResetToken(ctx, tokenHash(token), string(passwordHash), time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("password reset link is invalid or expired")
	}
	_ = s.Store.RevokeUserRefreshTokens(ctx, user.ID, time.Now().UTC())
	profile := userProfile(user)
	return &profile, nil
}

func (s *AuthService) sendVerificationEmail(ctx context.Context, user *UserAccount, r *http.Request) error {
	if s.Mailer == nil {
		return fmt.Errorf("email verification mailer is not configured")
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	ttl := s.EmailVerificationTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := time.Now().UTC()
	rec := &EmailVerificationTokenRecord{
		TokenHash: tokenHash(token),
		UserID:    user.ID,
		Email:     user.Email,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	if err := s.Store.CreateEmailVerificationToken(ctx, rec); err != nil {
		return err
	}
	link := s.verificationURL(token, r)
	return s.Mailer.Send(ctx, EmailMessage{
		To:      user.Email,
		Subject: "Verify your AgentAPI email",
		HTML:    verificationEmailHTML(user.DisplayName, link),
		Text:    "Verify your AgentAPI account: " + link,
	})
}

func (s *AuthService) verificationURL(token string, r *http.Request) string {
	base := strings.TrimSpace(s.PublicBaseURL)
	if base == "" && r != nil {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
			base = scheme + "://" + forwarded
		} else if host := strings.TrimSpace(r.Host); host != "" {
			base = scheme + "://" + host
		}
	}
	if base == "" {
		base = "http://localhost:8081"
	}
	return strings.TrimRight(base, "/") + "/v1/auth/verify-email?token=" + url.QueryEscape(token)
}

func (s *AuthService) passwordResetURL(token string, r *http.Request) string {
	base := strings.TrimSpace(s.PublicBaseURL)
	if base == "" && r != nil {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
			base = scheme + "://" + forwarded
		} else if host := strings.TrimSpace(r.Host); host != "" {
			base = scheme + "://" + host
		}
	}
	if base == "" {
		base = "http://localhost:8081"
	}
	return strings.TrimRight(base, "/") + "/reset-password?token=" + url.QueryEscape(token)
}

func verificationEmailHTML(displayName, link string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "there"
	}
	escapedName := html.EscapeString(name)
	escapedLink := html.EscapeString(link)
	return `<!doctype html>
<html>
  <body style="font-family: system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; color: #1f2933; line-height: 1.5;">
    <h2>Verify your AgentAPI email</h2>
    <p>Hi ` + escapedName + `,</p>
    <p>Confirm this email address to finish creating your account.</p>
    <p><a href="` + escapedLink + `" style="display:inline-block;background:#0f766e;color:#fff;padding:10px 16px;border-radius:6px;text-decoration:none;">Verify email</a></p>
    <p>If the button does not work, paste this link into your browser:</p>
    <p><a href="` + escapedLink + `">` + escapedLink + `</a></p>
  </body>
</html>`
}

func passwordResetEmailHTML(displayName, link string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "there"
	}
	escapedName := html.EscapeString(name)
	escapedLink := html.EscapeString(link)
	return `<!doctype html>
<html>
  <body style="font-family: system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; color: #1f2933; line-height: 1.5;">
    <h2>Reset your AgentAPI password</h2>
    <p>Hi ` + escapedName + `,</p>
    <p>Use this link to choose a new password for your account. The link expires soon.</p>
    <p><a href="` + escapedLink + `" style="display:inline-block;background:#0f766e;color:#fff;padding:10px 16px;border-radius:6px;text-decoration:none;">Reset password</a></p>
    <p>If the button does not work, paste this link into your browser:</p>
    <p><a href="` + escapedLink + `">` + escapedLink + `</a></p>
  </body>
</html>`
}

func (s *AuthService) issueSession(ctx context.Context, user *UserAccount, r *http.Request) (*AuthSession, error) {
	accessTTL := s.AccessTTL
	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	refreshTTL := s.RefreshTTL
	if refreshTTL <= 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
	expiresAt := time.Now().UTC().Add(accessTTL)
	access, err := signAccessToken(s.JWTSecret, s.Issuer, s.Audience, user, expiresAt)
	if err != nil {
		return nil, err
	}
	refresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	rec := &RefreshTokenRecord{
		TokenHash: refreshTokenHash(refresh),
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(refreshTTL),
	}
	if r != nil {
		rec.UserAgent = r.UserAgent()
		rec.IPAddress = requestIP(r)
	}
	if err := s.Store.CreateRefreshToken(ctx, rec); err != nil {
		return nil, err
	}
	return &AuthSession{
		User:         userProfile(user),
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    expiresAt,
	}, nil
}

func userProfile(user *UserAccount) UserProfile {
	return UserProfile{
		ID:              user.ID,
		Email:           user.Email,
		DisplayName:     user.DisplayName,
		Status:          user.Status,
		EmailVerified:   user.EmailVerifiedAt != nil || user.Status == UserStatusActive,
		EmailVerifiedAt: user.EmailVerifiedAt,
		CreatedAt:       user.CreatedAt,
		LastLoginAt:     user.LastLoginAt,
	}
}

func signAccessToken(secret, issuer, audience string, user *UserAccount, expiresAt time.Time) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf("JWT secret is required")
	}
	now := time.Now().UTC()
	claims := map[string]any{
		"sub":          user.ID,
		"email":        user.Email,
		"display_name": user.DisplayName,
		"typ":          "access",
		"iat":          now.Unix(),
		"exp":          expiresAt.Unix(),
	}
	if issuer != "" {
		claims["iss"] = issuer
	}
	if audience != "" {
		claims["aud"] = audience
	}
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func randomToken() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}

func refreshTokenHash(token string) string {
	return tokenHash(token)
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func validEmail(email string) bool {
	return strings.Contains(email, "@") && strings.Contains(email, ".") && !strings.ContainsAny(email, " \t\r\n")
}

func requestIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if comma := strings.Index(forwarded, ","); comma >= 0 {
			return strings.TrimSpace(forwarded[:comma])
		}
		return forwarded
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
