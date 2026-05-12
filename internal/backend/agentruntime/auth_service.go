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
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
	UserStatusBanned   = "banned"
)

type AuthService struct {
	Store      UserStore
	JWTSecret  string
	Issuer     string
	Audience   string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

type AuthSession struct {
	User         UserProfile `json:"user"`
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	CSRFToken    string      `json:"csrf_token,omitempty"`
	ExpiresAt    time.Time   `json:"expires_at"`
}

type UserProfile struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

func (s *AuthService) Register(ctx context.Context, email, password, displayName string, r *http.Request) (*AuthSession, error) {
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
	user := &UserAccount{
		ID:           uuid.NewString(),
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: string(passwordHash),
		Status:       UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.Store.CreateUser(ctx, user); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, fmt.Errorf("email is already registered")
		}
		return nil, err
	}
	return s.issueSession(ctx, user, r)
}

func (s *AuthService) Login(ctx context.Context, email, password string, r *http.Request) (*AuthSession, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("user system is not configured")
	}
	user, err := s.Store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("invalid email or password")
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
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      user.Status,
		CreatedAt:   user.CreatedAt,
		LastLoginAt: user.LastLoginAt,
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
