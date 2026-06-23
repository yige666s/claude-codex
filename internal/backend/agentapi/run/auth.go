package run

import (
	"context"
	"strconv"
	"strings"
	"time"

	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
)

func parseOperationRateLimits(value string) map[string]agentruntime.OperationLimit {
	limits := map[string]agentruntime.OperationLimit{}
	for _, item := range startupconfig.SplitCSV(value) {
		key, raw, ok := strings.Cut(item, "=")
		if !ok {
			key, raw, ok = strings.Cut(item, ":")
		}
		key = strings.TrimSpace(key)
		raw = strings.TrimSpace(raw)
		if !ok || key == "" || raw == "" {
			continue
		}
		limit, window, ok := parseOperationRateLimit(raw)
		if !ok {
			continue
		}
		limits[key] = agentruntime.OperationLimit{Limit: limit, Window: window}
	}
	return limits
}

func parseOperationRateLimit(value string) (int, time.Duration, bool) {
	limitPart, windowPart, ok := strings.Cut(strings.TrimSpace(value), "/")
	if !ok {
		return 0, 0, false
	}
	limit, err := strconv.Atoi(strings.TrimSpace(limitPart))
	if err != nil || limit <= 0 {
		return 0, 0, false
	}
	switch strings.ToLower(strings.TrimSpace(windowPart)) {
	case "s", "sec", "second":
		return limit, time.Second, true
	case "m", "min", "minute":
		return limit, time.Minute, true
	case "h", "hr", "hour":
		return limit, time.Hour, true
	case "d", "day":
		return limit, 24 * time.Hour, true
	default:
		duration, err := time.ParseDuration(strings.TrimSpace(windowPart))
		if err != nil || duration <= 0 {
			return 0, 0, false
		}
		return limit, duration, true
	}
}

type authConfig struct {
	mode                string
	userHeader          string
	authToken           string
	jwtSecret           string
	jwtIssuer           string
	jwtAudience         string
	jwtUserClaim        string
	sessionCookieName   string
	sessionCookieSecret string
	trustedUserHeader   string
	trustedSecretHeader string
	trustedSecret       string
}

func buildAuthenticator(cfg authConfig) agentruntime.Authenticator {
	mode := strings.ToLower(strings.TrimSpace(cfg.mode))
	jwt := agentruntime.JWTAuthenticator{
		Secret:    cfg.jwtSecret,
		UserClaim: cfg.jwtUserClaim,
		Issuer:    cfg.jwtIssuer,
		Audience:  cfg.jwtAudience,
	}
	switch mode {
	case "jwt":
		return jwt
	case "cookie", "session-cookie":
		return agentruntime.SessionCookieAuthenticator{
			CookieName: cfg.sessionCookieName,
			JWTAuthenticator: agentruntime.JWTAuthenticator{
				Secret:    startupconfig.FirstNonEmpty(cfg.sessionCookieSecret, cfg.jwtSecret),
				UserClaim: cfg.jwtUserClaim,
				Issuer:    cfg.jwtIssuer,
				Audience:  cfg.jwtAudience,
			},
		}
	case "trusted-header", "gateway":
		return agentruntime.TrustedHeaderAuthenticator{
			UserHeader:     cfg.trustedUserHeader,
			RequiredHeader: cfg.trustedSecretHeader,
			RequiredValue:  cfg.trustedSecret,
		}
	case "header":
		return agentruntime.HeaderAuthenticator{UserHeader: cfg.userHeader, BearerToken: cfg.authToken}
	case "none":
		return agentruntime.HeaderAuthenticator{UserHeader: cfg.userHeader}
	default:
		var chain agentruntime.CompositeAuthenticator
		if cfg.jwtSecret != "" {
			chain = append(chain, jwt)
		}
		if cfg.sessionCookieSecret != "" {
			chain = append(chain, agentruntime.SessionCookieAuthenticator{
				CookieName:       cfg.sessionCookieName,
				JWTAuthenticator: agentruntime.JWTAuthenticator{Secret: cfg.sessionCookieSecret, UserClaim: cfg.jwtUserClaim, Issuer: cfg.jwtIssuer, Audience: cfg.jwtAudience},
			})
		}
		if cfg.trustedSecretHeader != "" && cfg.trustedSecret != "" {
			chain = append(chain, agentruntime.TrustedHeaderAuthenticator{
				UserHeader:     cfg.trustedUserHeader,
				RequiredHeader: cfg.trustedSecretHeader,
				RequiredValue:  cfg.trustedSecret,
			})
		}
		chain = append(chain, agentruntime.HeaderAuthenticator{UserHeader: cfg.userHeader, BearerToken: cfg.authToken})
		return chain
	}
}

type authServiceConfig struct {
	jwtSecret                 string
	jwtIssuer                 string
	jwtAudience               string
	accessTTL                 time.Duration
	refreshTTL                time.Duration
	emailVerificationRequired bool
	emailVerificationTTL      time.Duration
	emailProvider             string
	emailFrom                 string
	emailPublicBaseURL        string
	resendAPIKey              string
	resendBaseURL             string
}

func buildAuthService(enabled bool, storeCfg storeConfig, authCfg authServiceConfig) *agentruntime.AuthService {
	if !enabled {
		return nil
	}
	if strings.TrimSpace(authCfg.jwtSecret) == "" {
		logFatal("enable-user-system requires -jwt-secret or AGENT_API_JWT_SECRET")
	}
	if !strings.EqualFold(strings.TrimSpace(storeCfg.backend), "sql") {
		logFatal("enable-user-system currently requires -store-backend sql")
	}
	db := openSQLDB(storeCfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(storeCfg.sqlDialect, storeCfg.sqlDriver))
	store := agentruntime.NewSQLUserStoreWithDialect(db, dialect)
	if err := store.Init(ctx); err != nil {
		logFatalf("init sql user store: %v", err)
	}
	if authCfg.emailVerificationRequired && strings.TrimSpace(authCfg.emailProvider) == "" {
		logFatal("email verification requires -email-provider or AGENT_API_EMAIL_PROVIDER")
	}
	return &agentruntime.AuthService{
		Store:                     store,
		JWTSecret:                 authCfg.jwtSecret,
		Issuer:                    authCfg.jwtIssuer,
		Audience:                  authCfg.jwtAudience,
		AccessTTL:                 authCfg.accessTTL,
		RefreshTTL:                authCfg.refreshTTL,
		EmailVerificationRequired: authCfg.emailVerificationRequired,
		EmailVerificationTTL:      authCfg.emailVerificationTTL,
		PublicBaseURL:             authCfg.emailPublicBaseURL,
		Mailer:                    buildMailer(authCfg),
	}
}

func buildMailer(authCfg authServiceConfig) agentruntime.Mailer {
	switch strings.ToLower(strings.TrimSpace(authCfg.emailProvider)) {
	case "":
		return nil
	case "resend":
		return agentruntime.ResendMailer{
			APIKey:  authCfg.resendAPIKey,
			From:    authCfg.emailFrom,
			BaseURL: authCfg.resendBaseURL,
		}
	default:
		logFatalf("unsupported email provider %q", authCfg.emailProvider)
		return nil
	}
}
