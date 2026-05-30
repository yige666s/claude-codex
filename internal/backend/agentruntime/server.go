package agentruntime

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/httpjson"
	skillpkg "claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"

	"github.com/gorilla/websocket"
)

const defaultRateLimitWindow = time.Minute

type Server struct {
	runtime          *Runtime
	router           http.Handler
	auth             Authenticator
	authService      *AuthService
	limiter          RateLimitPolicy
	logger           *slog.Logger
	upgrader         websocket.Upgrader
	security         WebSecurityConfig
	llmStatus        func() LLMGovernanceStatus
	llmUsage         LLMUsageAdminStore
	llmConfig        *LLMGovernanceConfigManager
	metrics          *MetricsRegistry
	audit            AuditLogger
	risk             RiskStore
	riskScanner      RiskScanner
	evaluation       EvaluationStore
	instrumentHTTP   func(http.Handler) http.Handler
	operationLimiter *OperationRateLimiter
	adminToken       string
	skillRegistry    SkillRegistryAdminStore
	readyMu          sync.RWMutex
	readyChecks      map[string]readinessCheck
	shutdownOnce     sync.Once
	shutdownCh       chan struct{}
}

func NewServer(runtime *Runtime, auth Authenticator, limiter RateLimitPolicy, logger *log.Logger) *Server {
	return NewServerWithLogger(runtime, auth, limiter, newStructuredLogger(logger))
}

func NewServerWithLogger(runtime *Runtime, auth Authenticator, limiter RateLimitPolicy, logger *slog.Logger) *Server {
	if limiter == nil {
		limiter = NewRateLimiter(60, defaultRateLimitWindow)
	}
	server := &Server{
		runtime: runtime,
		auth:    auth,
		limiter: limiter,
		logger:  componentLogger(logger, "http_server"),
		metrics: NewMetricsRegistry(),
		upgrader: websocket.Upgrader{
			CheckOrigin:  sameHostOrigin,
			Subprotocols: []string{"agentapi.bearer"},
		},
		readyChecks: make(map[string]readinessCheck),
		shutdownCh:  make(chan struct{}),
	}
	server.router = server.buildRouter()
	return server
}

func (s *Server) SetHTTPInstrumentation(instrument func(http.Handler) http.Handler) {
	if s == nil {
		return
	}
	s.instrumentHTTP = instrument
	s.router = s.buildRouter()
}

func (s *Server) BeginShutdown() {
	if s == nil {
		return
	}
	s.shutdownOnce.Do(func() {
		close(s.shutdownCh)
	})
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.BeginShutdown()
	if s.runtime == nil {
		return nil
	}
	return s.runtime.Shutdown(ctx)
}

func (s *Server) shutdownDone() <-chan struct{} {
	if s == nil || s.shutdownCh == nil {
		ch := make(chan struct{})
		return ch
	}
	return s.shutdownCh
}

func (s *Server) isShuttingDown() bool {
	select {
	case <-s.shutdownDone():
		return true
	default:
		return false
	}
}

func (s *Server) SetWebSecurity(config WebSecurityConfig) error {
	if err := validateCookieSecurity(config); err != nil {
		return err
	}
	s.security = config
	s.upgrader.CheckOrigin = func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return sameHostOrigin(r)
		}
		return sameHostOrigin(r) || originAllowed(origin, config.CORSAllowedOrigins)
	}
	return nil
}

func (s *Server) SetAuthService(authService *AuthService) {
	s.authService = authService
}

func (s *Server) SetLLMStatusProvider(provider func() LLMGovernanceStatus) {
	s.llmStatus = provider
}

func (s *Server) SetLLMUsageStore(store LLMUsageAdminStore) {
	s.llmUsage = store
}

func (s *Server) SetEvaluationStore(store EvaluationStore) {
	if s == nil {
		return
	}
	s.evaluation = store
}

func (s *Server) SetLLMGovernanceConfigManager(manager *LLMGovernanceConfigManager) {
	s.llmConfig = manager
}

func (s *Server) SetAdminToken(token string) {
	if s == nil {
		return
	}
	s.adminToken = strings.TrimSpace(token)
}

func (s *Server) SetSkillRegistry(registry SkillRegistryAdminStore) {
	if s == nil {
		return
	}
	s.skillRegistry = registry
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.NotFound(w, r)
		return
	}
	if s.router == nil {
		s.router = s.buildRouter()
	}
	s.router.ServeHTTP(w, r)
}

func (s *Server) handleApp(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(appHTML))
}

func (s *Server) handleLLMStatus(w http.ResponseWriter, _ *http.Request) {
	if s.llmStatus == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "llm governance is not configured"})
		return
	}
	status := s.llmStatus()
	if s.llmConfig != nil {
		status.Config = s.llmConfig.StatusMap()
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email       string `json:"email" validate:"required,email"`
		Password    string `json:"password" validate:"notblank"`
		DisplayName string `json:"display_name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	if s.authService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user system is not configured"})
		return
	}
	registration, err := s.authService.Register(r.Context(), body.Email, body.Password, body.DisplayName, r)
	if err != nil {
		s.recordRiskEvent(r, RiskEvent{
			IPAddress:  clientIP(r),
			Operation:  RiskOperationAuthRegister,
			Reason:     "auth_register_failed",
			RiskLevel:  RiskLevelLow,
			ScoreDelta: 3,
			Metadata:   map[string]any{"email": body.Email, "error": err.Error()},
		})
		writeJSONError(w, err)
		return
	}
	if registration != nil && registration.VerificationRequired {
		s.logEvent("auth_register_verification_sent", map[string]any{"email": registration.Email, "request_id": requestIDFromContext(r.Context())})
		s.auditEvent(r, "auth_register_verification_sent", User{}, map[string]any{"email": registration.Email})
		writeJSON(w, http.StatusAccepted, registration)
		return
	}
	if registration == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registration did not create a session"})
		return
	}
	session := registration.Session
	if session == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registration did not create a session"})
		return
	}
	s.setAuthCookies(w, session)
	s.logEvent("auth_register", map[string]any{"user_id": session.User.ID, "request_id": requestIDFromContext(r.Context())})
	s.auditEvent(r, "auth_register", User{ID: session.User.ID}, map[string]any{"email": session.User.Email})
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleAuthVerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" && r.Method == http.MethodPost {
		var body struct {
			Token string `json:"token" validate:"notblank"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSONError(w, err)
			return
		}
		token = strings.TrimSpace(body.Token)
	}
	if s.authService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user system is not configured"})
		return
	}
	profile, err := s.authService.VerifyEmail(r.Context(), token)
	if err != nil {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<html><body><h1>Email verification failed</h1><p>The verification link is invalid or expired.</p></body></html>`))
			return
		}
		writeJSONError(w, err)
		return
	}
	s.logEvent("auth_email_verified", map[string]any{"user_id": profile.ID, "request_id": requestIDFromContext(r.Context())})
	s.auditEvent(r, "auth_email_verified", User{ID: profile.ID}, map[string]any{"email": profile.Email})
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><h1>Email verified</h1><p>You can close this page and sign in.</p><p><a href="/">Return to AgentAPI</a></p></body></html>`))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": profile, "verified": true})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email" validate:"required,email"`
		Password string `json:"password" validate:"notblank"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	if s.authService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user system is not configured"})
		return
	}
	session, err := s.authService.Login(r.Context(), body.Email, body.Password, r)
	if err != nil {
		s.recordRiskEvent(r, RiskEvent{
			IPAddress:  clientIP(r),
			Operation:  RiskOperationAuthLogin,
			Reason:     "auth_login_failed",
			RiskLevel:  RiskLevelMedium,
			ScoreDelta: 10,
			Metadata:   map[string]any{"email": body.Email},
		})
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	s.setAuthCookies(w, session)
	s.logEvent("auth_login", map[string]any{"user_id": session.User.ID, "request_id": requestIDFromContext(r.Context())})
	s.auditEvent(r, "auth_login", User{ID: session.User.ID}, map[string]any{"email": session.User.Email})
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleAuthPasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email" validate:"required,email"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	if s.authService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user system is not configured"})
		return
	}
	if err := s.authService.RequestPasswordReset(r.Context(), body.Email, r); err != nil {
		s.recordRiskEvent(r, RiskEvent{
			IPAddress:  clientIP(r),
			Operation:  RiskOperationAuthLogin,
			Reason:     "auth_password_reset_request_failed",
			RiskLevel:  RiskLevelLow,
			ScoreDelta: 3,
			Metadata:   map[string]any{"email": body.Email, "error": err.Error()},
		})
		writeJSONError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"sent": true})
}

func (s *Server) handleAuthPasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token" validate:"notblank"`
		Password string `json:"password" validate:"notblank"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	if s.authService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user system is not configured"})
		return
	}
	profile, err := s.authService.ResetPassword(r.Context(), body.Token, body.Password)
	if err != nil {
		s.recordRiskEvent(r, RiskEvent{
			IPAddress:  clientIP(r),
			Operation:  RiskOperationAuthLogin,
			Reason:     "auth_password_reset_failed",
			RiskLevel:  RiskLevelMedium,
			ScoreDelta: 8,
		})
		writeJSONError(w, err)
		return
	}
	s.logEvent("auth_password_reset", map[string]any{"user_id": profile.ID, "request_id": requestIDFromContext(r.Context())})
	s.auditEvent(r, "auth_password_reset", User{ID: profile.ID}, map[string]any{"email": profile.Email})
	writeJSON(w, http.StatusOK, map[string]any{"user": profile, "reset": true})
}

func (s *Server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token" validate:"notblank"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	if s.authService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user system is not configured"})
		return
	}
	session, err := s.authService.Refresh(r.Context(), body.RefreshToken, r)
	if err != nil {
		s.recordRiskEvent(r, RiskEvent{
			IPAddress:  clientIP(r),
			Operation:  RiskOperationAuthRefresh,
			Reason:     "auth_refresh_failed",
			RiskLevel:  RiskLevelLow,
			ScoreDelta: 4,
		})
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	s.setAuthCookies(w, session)
	s.auditEvent(r, "auth_refresh", User{ID: session.User.ID}, nil)
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request, user User) {
	if s.authService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user system is not configured"})
		return
	}
	profile, err := s.authService.Me(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": profile})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request, user User) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	if s.authService != nil {
		_ = s.authService.Logout(r.Context(), body.RefreshToken)
	}
	s.clearAuthCookies(w)
	s.auditEvent(r, "auth_logout", user, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleDataExport(w http.ResponseWriter, r *http.Request, user User) {
	var profile *UserProfile
	if s.authService != nil {
		if loaded, err := s.authService.Me(r.Context(), user.ID); err == nil {
			profile = loaded
		}
	}
	if profile == nil {
		profile = &UserProfile{ID: user.ID}
	}
	export, err := s.runtime.ExportUserData(r.Context(), profile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.recordGovernanceEvent("data_export")
	s.auditEvent(r, "data_export", user, nil)
	writeJSON(w, http.StatusOK, export)
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(s.adminToken) == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin API is not configured"})
		return false
	}
	token := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("admin_token"))
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.adminToken)) != 1 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin token is required"})
		return false
	}
	return true
}

func (s *Server) handleAdminListSkills(w http.ResponseWriter, r *http.Request) {
	records, err := s.skillRegistry.ListSkills(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": records})
}

func (s *Server) adminOpsUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
		return "", false
	}
	return userID, true
}

func (s *Server) handleAdminOpsListSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.adminOpsUserID(w, r)
	if !ok {
		return
	}
	sessions, err := s.runtime.ListSessions(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := parseBoundedInt(r.URL.Query().Get("limit"), 50, 1, 200)
	out := make([]*state.Session, 0, minInt(len(sessions), limit))
	for _, session := range sessions {
		if session == nil || (query != "" && !adminSessionMatchesQuery(session, query)) {
			continue
		}
		out = append(out, session)
		if len(out) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) handleAdminOpsGetSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	userID, ok := s.adminOpsUserID(w, r)
	if !ok {
		return
	}
	session, err := s.runtime.GetSession(r.Context(), userID, sessionID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": session})
}

func (s *Server) handleAdminOpsListJobs(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.adminOpsUserID(w, r)
	if !ok {
		return
	}
	jobs, err := s.runtime.ListJobs(r.Context(), userID, r.URL.Query().Get("session_id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	limit := parseBoundedInt(r.URL.Query().Get("limit"), 50, 1, 200)
	out := make([]*Job, 0, minInt(len(jobs), limit))
	for _, job := range jobs {
		if job == nil {
			continue
		}
		if status != "" && status != "all" && strings.ToLower(job.Status) != status {
			continue
		}
		if query != "" && !adminJobMatchesQuery(job, query) {
			continue
		}
		out = append(out, job)
		if len(out) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (s *Server) handleAdminOpsGetJob(w http.ResponseWriter, r *http.Request, jobID string) {
	userID, ok := s.adminOpsUserID(w, r)
	if !ok {
		return
	}
	job, err := s.runtime.GetJob(r.Context(), userID, jobID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleAdminOpsListJobEvents(w http.ResponseWriter, r *http.Request, jobID string) {
	userID, ok := s.adminOpsUserID(w, r)
	if !ok {
		return
	}
	limit := parseBoundedInt(r.URL.Query().Get("limit"), 500, 1, 2000)
	events, err := s.runtime.ListJobEvents(r.Context(), userID, jobID, jobEventCursor(r), limit)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleAdminOpsCancelJob(w http.ResponseWriter, r *http.Request, user User, jobID string) {
	userID, ok := s.adminOpsUserID(w, r)
	if !ok {
		return
	}
	if err := s.runtime.CancelJob(r.Context(), userID, jobID); err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "admin_job_cancel", user, map[string]any{"target_user_id": userID, "job_id": jobID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleAdminOpsListAssets(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.adminOpsUserID(w, r)
	if !ok {
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	jobID := strings.TrimSpace(r.URL.Query().Get("job_id"))
	limit := parseBoundedInt(r.URL.Query().Get("limit"), 100, 1, 300)
	var items []*Artifact
	if kind == "" || kind == "all" || kind == AssetKindAttachment {
		attachments, err := s.runtime.ListAttachments(r.Context(), userID, sessionID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		items = append(items, attachments...)
	}
	if kind == "" || kind == "all" || kind == AssetKindArtifact {
		artifacts, err := s.runtime.ListArtifacts(r.Context(), userID, sessionID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		items = append(items, artifacts...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	out := make([]*Artifact, 0, minInt(len(items), limit))
	for _, item := range items {
		if item == nil {
			continue
		}
		if jobID != "" && item.JobID != jobID {
			continue
		}
		if query != "" && !adminAssetMatchesQuery(item, query) {
			continue
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": out})
}

func (s *Server) handleAdminOpsHealth(w http.ResponseWriter, r *http.Request) {
	readinessStatus, readinessChecks := s.readinessSnapshot(r.Context(), 2*time.Second)
	var llmStatus LLMGovernanceStatus
	if s.llmStatus != nil {
		llmStatus = s.llmStatus()
	}
	if s.llmConfig != nil {
		llmStatus.Config = s.llmConfig.StatusMap()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"readiness": map[string]any{
			"status": readinessStatus,
			"checks": readinessChecks,
		},
		"llm":  llmStatus,
		"live": s.metrics.LiveHealthSnapshot(),
	})
}

func (s *Server) handleAdminOpsLLMConfig(w http.ResponseWriter, _ *http.Request) {
	if s.llmConfig == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "llm governance config is not configured"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": s.llmConfig.StatusMap()})
}

func (s *Server) handleAdminOpsUpdateLLMConfig(w http.ResponseWriter, r *http.Request, actor User) {
	if s.llmConfig == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "llm governance config is not configured"})
		return
	}
	var patch LLMGovernanceConfigPatch
	if err := readJSON(r, &patch); err != nil {
		writeJSONError(w, err)
		return
	}
	if _, err := s.llmConfig.Update(r.Context(), patch); err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "admin_llm_config_update", actor, map[string]any{
		"config": s.llmConfig.StatusMap(),
	})
	writeJSON(w, http.StatusOK, map[string]any{"config": s.llmConfig.StatusMap()})
}

func (s *Server) handleAdminOpsLLMUsage(w http.ResponseWriter, r *http.Request) {
	if s.llmUsage == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "llm usage store is not configured"})
		return
	}
	filter := LLMUsageAdminFilter{
		UserID: strings.TrimSpace(r.URL.Query().Get("user_id")),
		Limit:  parseBoundedInt(r.URL.Query().Get("limit"), 200, 1, 1000),
	}
	days := parseBoundedInt(r.URL.Query().Get("days"), 1, 1, 90)
	filter.Since = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	if since := strings.TrimSpace(r.URL.Query().Get("since")); since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since"})
			return
		}
		filter.Since = parsed.UTC()
	}
	summary, err := s.llmUsage.SummarizeLLMUsage(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"usage": summary})
}

func (s *Server) handleAdminOpsQuota(w http.ResponseWriter, r *http.Request) {
	if s.llmUsage == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "llm usage store is not configured"})
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
		return
	}
	since, ok := adminQuotaSince(w, r)
	if !ok {
		return
	}
	limit := parseBoundedInt(r.URL.Query().Get("limit"), 100, 1, 1000)
	summary, err := s.llmUsage.SummarizeLLMQuota(r.Context(), userID, since, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quota": summary})
}

func (s *Server) handleAdminOpsQuotaReset(w http.ResponseWriter, r *http.Request, actor User) {
	if s.llmUsage == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "llm usage store is not configured"})
		return
	}
	var body adminOpsQuotaResetRequest
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	userID := strings.TrimSpace(body.UserID)
	since := startOfUTCDay(time.Now())
	current, err := s.llmUsage.SummarizeLLMQuota(r.Context(), userID, since, 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	effective := current.EffectiveUsage
	adjustment := LLMQuotaAdjustment{
		UserID:                userID,
		ActorUserID:           actor.ID,
		Reason:                firstNonEmptyString(body.Reason, "manual daily quota reset"),
		RequestDelta:          -effective.Requests,
		InputTokenDelta:       -effective.InputTokens,
		OutputTokenDelta:      -effective.OutputTokens,
		TotalTokenDelta:       -effective.TotalTokens,
		EstimatedCostDeltaUSD: -effective.EstimatedCostUSD,
		CreatedAt:             time.Now().UTC(),
	}
	if err := s.llmUsage.RecordLLMQuotaAdjustment(r.Context(), adjustment); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "admin_llm_quota_reset", actor, map[string]any{
		"target_user_id": userID,
		"request_delta":  adjustment.RequestDelta,
		"token_delta":    adjustment.TotalTokenDelta,
		"cost_delta_usd": adjustment.EstimatedCostDeltaUSD,
		"reason":         adjustment.Reason,
	})
	summary, err := s.llmUsage.SummarizeLLMQuota(r.Context(), userID, since, 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quota": summary})
}

func (s *Server) handleAdminOpsQuotaRefund(w http.ResponseWriter, r *http.Request, actor User) {
	if s.llmUsage == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "llm usage store is not configured"})
		return
	}
	var body adminOpsQuotaRefundRequest
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	userID := strings.TrimSpace(body.UserID)
	adjustment := LLMQuotaAdjustment{
		UserID:                userID,
		ActorUserID:           actor.ID,
		Reason:                firstNonEmptyString(body.Reason, "manual quota refund"),
		RequestDelta:          -body.RequestRefund,
		TotalTokenDelta:       -body.TokenRefund,
		EstimatedCostDeltaUSD: -body.CostRefundUSD,
		CreatedAt:             time.Now().UTC(),
	}
	if err := s.llmUsage.RecordLLMQuotaAdjustment(r.Context(), adjustment); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "admin_llm_quota_refund", actor, map[string]any{
		"target_user_id": userID,
		"request_delta":  adjustment.RequestDelta,
		"token_delta":    adjustment.TotalTokenDelta,
		"cost_delta_usd": adjustment.EstimatedCostDeltaUSD,
		"reason":         adjustment.Reason,
	})
	since := startOfUTCDay(time.Now())
	summary, err := s.llmUsage.SummarizeLLMQuota(r.Context(), userID, since, 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quota": summary})
}

func adminQuotaSince(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	days := parseBoundedInt(r.URL.Query().Get("days"), 1, 1, 90)
	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	if strings.TrimSpace(r.URL.Query().Get("daily")) == "true" || strings.TrimSpace(r.URL.Query().Get("days")) == "" {
		since = startOfUTCDay(time.Now())
	}
	if value := strings.TrimSpace(r.URL.Query().Get("since")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since"})
			return time.Time{}, false
		}
		since = parsed.UTC()
	}
	return since, true
}

func (s *Server) handleAdminOpsAudit(w http.ResponseWriter, r *http.Request) {
	store, ok := s.audit.(AuditLogStore)
	if !ok || store == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "audit log store is not configured"})
		return
	}
	filter := AuditLogFilter{
		UserID:    strings.TrimSpace(r.URL.Query().Get("user_id")),
		Event:     strings.TrimSpace(r.URL.Query().Get("event")),
		RiskLevel: strings.TrimSpace(r.URL.Query().Get("risk")),
		Query:     strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:     parseBoundedInt(r.URL.Query().Get("limit"), 200, 1, 1000),
	}
	days := parseBoundedInt(r.URL.Query().Get("days"), 1, 1, 90)
	filter.Since = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	if since := strings.TrimSpace(r.URL.Query().Get("since")); since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since"})
			return
		}
		filter.Since = parsed.UTC()
	}
	summary, err := store.ListAuditRecords(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": summary})
}

func (s *Server) handleAdminOpsRisk(w http.ResponseWriter, r *http.Request) {
	if s.risk == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "risk store is not configured"})
		return
	}
	filter := RiskEventFilter{
		UserID:    strings.TrimSpace(r.URL.Query().Get("user_id")),
		SessionID: strings.TrimSpace(r.URL.Query().Get("session_id")),
		IPAddress: strings.TrimSpace(r.URL.Query().Get("ip_address")),
		Operation: strings.TrimSpace(r.URL.Query().Get("operation")),
		RiskLevel: strings.TrimSpace(r.URL.Query().Get("risk")),
		Query:     strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:     parseBoundedInt(r.URL.Query().Get("limit"), 200, 1, 1000),
	}
	days := parseBoundedInt(r.URL.Query().Get("days"), 1, 1, 90)
	filter.Since = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	if since := strings.TrimSpace(r.URL.Query().Get("since")); since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since"})
			return
		}
		filter.Since = parsed.UTC()
	}
	summary, err := s.risk.ListRiskEvents(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"risk": summary})
}

func (s *Server) handleAdminOpsRiskReviews(w http.ResponseWriter, r *http.Request) {
	store, ok := s.risk.(RiskReviewStore)
	if !ok || store == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "risk review store is not configured"})
		return
	}
	filter := RiskReviewFilter{
		UserID:    strings.TrimSpace(r.URL.Query().Get("user_id")),
		Status:    strings.TrimSpace(r.URL.Query().Get("status")),
		RiskLevel: strings.TrimSpace(r.URL.Query().Get("risk")),
		Operation: strings.TrimSpace(r.URL.Query().Get("operation")),
		Query:     strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:     parseBoundedInt(r.URL.Query().Get("limit"), 200, 1, 1000),
	}
	days := parseBoundedInt(r.URL.Query().Get("days"), 7, 1, 90)
	filter.Since = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	if since := strings.TrimSpace(r.URL.Query().Get("since")); since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since"})
			return
		}
		filter.Since = parsed.UTC()
	}
	summary, err := store.ListRiskReviews(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": summary})
}

func (s *Server) handleAdminOpsUpdateRiskReview(w http.ResponseWriter, r *http.Request, user User, reviewID string) {
	store, ok := s.risk.(RiskReviewStore)
	if !ok || store == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "risk review store is not configured"})
		return
	}
	var body struct {
		Status     string `json:"status"`
		AssignedTo string `json:"assigned_to"`
		Resolution string `json:"resolution"`
		Note       string `json:"note"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	item, err := store.UpdateRiskReview(r.Context(), reviewID, RiskReviewUpdate{
		Status:     body.Status,
		AssignedTo: body.AssignedTo,
		Resolution: body.Resolution,
		Note:       body.Note,
		ActorID:    user.ID,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "risk review not found"})
		return
	}
	s.auditEvent(r, "risk_review_update", user, map[string]any{
		"risk_review_id": item.ID,
		"risk_event_id":  item.RiskEventID,
		"status":         item.Status,
		"target_user_id": item.UserID,
		"asset_id":       item.AssetID,
		"job_id":         item.JobID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"review": item})
}

func adminSessionMatchesQuery(session *state.Session, query string) bool {
	if session == nil || query == "" {
		return true
	}
	haystacks := []string{session.ID, session.Description, session.WorkingDir}
	for _, message := range session.Messages {
		haystacks = append(haystacks, message.Role, message.Content)
	}
	return containsLowerAny(query, haystacks...)
}

func adminJobMatchesQuery(job *Job, query string) bool {
	if job == nil || query == "" {
		return true
	}
	return containsLowerAny(query, job.ID, job.SessionID, job.Type, job.Status, job.Content, job.Error, strings.Join(job.AttachmentIDs, " "))
}

func adminAssetMatchesQuery(asset *Artifact, query string) bool {
	if asset == nil || query == "" {
		return true
	}
	return containsLowerAny(query, asset.ID, asset.Kind, asset.SessionID, asset.JobID, asset.Filename, asset.ContentType, asset.ObjectKey)
}

func containsLowerAny(query string, values ...string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func (s *Server) adminUserStore() (AdminUserStore, bool) {
	if s == nil || s.authService == nil || s.authService.Store == nil {
		return nil, false
	}
	store, ok := s.authService.Store.(AdminUserStore)
	return store, ok
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request, store AdminUserStore) {
	filter := adminUserFilterFromRequest(r)
	users, err := store.ListUsers(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleAdminGetUser(w http.ResponseWriter, r *http.Request, store AdminUserStore, userID string) {
	record, err := store.GetAdminUser(r.Context(), userID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "user not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": record})
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request, actor User, store AdminUserStore, userID string) {
	var body struct {
		Status *string `json:"status" validate:"required"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	status := normalizeOptionalUserStatus(*body.Status)
	if status == "" || !validUserStatus(status) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user status"})
		return
	}
	s.updateAdminUserStatus(w, r, actor, store, userID, status, "user_status_update")
}

func (s *Server) handleAdminUserAction(w http.ResponseWriter, r *http.Request, actor User, store AdminUserStore, userID string, action string) {
	status := ""
	event := ""
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "disable":
		status = UserStatusDisabled
		event = "user_disable"
	case "ban":
		status = UserStatusBanned
		event = "user_ban"
	case "reactivate":
		status = UserStatusActive
		event = "user_reactivate"
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	s.updateAdminUserStatus(w, r, actor, store, userID, status, event)
}

func (s *Server) updateAdminUserStatus(w http.ResponseWriter, r *http.Request, actor User, store AdminUserStore, userID string, status string, event string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id is required"})
		return
	}
	if userID == actor.ID && status != UserStatusActive {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot disable or ban the current admin session user"})
		return
	}
	record, err := store.UpdateUserStatus(r.Context(), userID, status, time.Now().UTC())
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			code = http.StatusNotFound
		}
		writeJSON(w, code, map[string]string{"error": "user not found"})
		return
	}
	if status != UserStatusActive && s.authService != nil && s.authService.Store != nil {
		_ = s.authService.Store.RevokeUserRefreshTokens(r.Context(), userID, time.Now().UTC())
		if refreshed, err := store.GetAdminUser(r.Context(), userID); err == nil {
			record = refreshed
		}
	}
	if event == "" {
		event = "user_status_update"
	}
	s.recordGovernanceEvent(event)
	s.auditEvent(r, event, actor, map[string]any{"target_user_id": record.ID, "status": record.Status})
	writeJSON(w, http.StatusOK, map[string]any{"user": record})
}

func adminUserFilterFromRequest(r *http.Request) AdminUserFilter {
	query := r.URL.Query()
	limit := 100
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	offset := 0
	if value := strings.TrimSpace(query.Get("offset")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			offset = parsed
		}
	}
	return AdminUserFilter{
		Query:  query.Get("q"),
		Status: query.Get("status"),
		Limit:  limit,
		Offset: offset,
	}
}

func (s *Server) handleAdminImportSkill(w http.ResponseWriter, r *http.Request, user User) {
	var body struct {
		Skill     SkillRegistryRecord `json:"skill"`
		Changelog string              `json:"changelog"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	record := body.Skill
	if record.Name == "" {
		record = SkillRegistryRecord{
			Name:        strings.TrimSpace(skillMetadataValueString(body.Skill.Metadata["name"])),
			DisplayName: body.Skill.DisplayName,
			Description: body.Skill.Description,
			Category:    body.Skill.Category,
			Icon:        body.Skill.Icon,
			Status:      body.Skill.Status,
			Version:     body.Skill.Version,
			Source:      body.Skill.Source,
			SkillRoot:   body.Skill.SkillRoot,
			Metadata:    body.Skill.Metadata,
			ContentHash: body.Skill.ContentHash,
		}
	}
	if record.Status == SkillStatusPublished {
		review := ReviewSkillForPublication(record)
		if !review.Passed {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "skill review failed", "review": review})
			return
		}
	}
	updated, err := s.skillRegistry.ImportSkill(r.Context(), record, body.Changelog)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	if updated.Status == SkillStatusPublished {
		if err := s.skillRegistry.RecordSkillRelease(r.Context(), updated, body.Changelog, user.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	s.refreshSkillCatalog(r.Context())
	s.recordGovernanceEvent("skill_import")
	s.auditEvent(r, "skill_import", user, map[string]any{"skill_name": updated.Name, "status": updated.Status})
	writeJSON(w, http.StatusCreated, map[string]any{"skill": updated})
}

func (s *Server) handleAdminUpdateSkill(w http.ResponseWriter, r *http.Request, user User, name string) {
	var body struct {
		DisplayName *string        `json:"display_name"`
		Description *string        `json:"description"`
		Category    *string        `json:"category"`
		Icon        *string        `json:"icon"`
		Version     *string        `json:"version"`
		Status      *string        `json:"status"`
		Changelog   *string        `json:"changelog"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	record, err := s.skillRegistry.GetSkill(r.Context(), name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "skill not found"})
		return
	}
	previousStatus := record.Status
	if body.DisplayName != nil {
		record.DisplayName = strings.TrimSpace(*body.DisplayName)
	}
	if body.Description != nil {
		record.Description = strings.TrimSpace(*body.Description)
	}
	if body.Category != nil {
		record.Category = strings.TrimSpace(*body.Category)
	}
	if body.Icon != nil {
		record.Icon = strings.TrimSpace(*body.Icon)
	}
	if body.Version != nil {
		record.Version = strings.TrimSpace(*body.Version)
	}
	if body.Status != nil {
		status := normalizeSkillStatus(*body.Status)
		if !validSkillStatus(*body.Status) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid skill status"})
			return
		}
		record.Status = status
	}
	if body.Metadata != nil {
		if record.Metadata == nil {
			record.Metadata = map[string]any{}
		}
		for key, value := range body.Metadata {
			key = strings.TrimSpace(key)
			if key != "" {
				record.Metadata[key] = value
			}
		}
	}
	policyUpdated := skillMetadataIncludesPolicy(body.Metadata)
	if record.Status == SkillStatusPublished {
		reviewRecord := record
		if previousStatus == SkillStatusDisabled || previousStatus == SkillStatusArchived {
			reviewRecord.Status = previousStatus
		}
		review := ReviewSkillForPublication(reviewRecord)
		if !review.Passed {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "skill review failed", "review": review})
			return
		}
	}
	updated, err := s.skillRegistry.UpdateSkill(r.Context(), record)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if body.Changelog != nil || body.Version != nil || (body.Status != nil && normalizeSkillStatus(*body.Status) == SkillStatusPublished) {
		if err := s.skillRegistry.RecordSkillVersion(r.Context(), updated, stringValue(body.Changelog)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if body.Status != nil {
		nextStatus := normalizeSkillStatus(*body.Status)
		if nextStatus == SkillStatusPublished || nextStatus == SkillStatusArchived {
			if err := s.skillRegistry.RecordSkillRelease(r.Context(), updated, stringValue(body.Changelog), user.ID); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
	}
	s.refreshSkillCatalog(r.Context())
	s.recordGovernanceEvent("skill_update")
	s.auditEvent(r, "skill_update", user, map[string]any{"skill_name": updated.Name, "status": updated.Status})
	if policyUpdated {
		s.recordGovernanceEvent("skill_policy_update")
		s.auditEvent(r, "skill_policy_update", user, map[string]any{"skill_name": updated.Name, "status": updated.Status})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill": updated})
}

func skillMetadataIncludesPolicy(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	for _, key := range []string{"policy", "permissions", "runtime_policy", "runtimePolicy"} {
		if _, ok := metadata[key]; ok {
			return true
		}
	}
	for _, key := range []string{"agentapi", "runtime", "openclaw"} {
		value, ok := metadata[key].(map[string]any)
		if !ok {
			continue
		}
		for _, policyKey := range []string{"policy", "permissions", "runtime_policy", "runtimePolicy"} {
			if _, ok := value[policyKey]; ok {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleAdminSetSkillStatus(w http.ResponseWriter, r *http.Request, user User, name string, action string) {
	var body struct {
		Changelog string `json:"changelog"`
	}
	if err := readOptionalJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	status := ""
	event := ""
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "publish":
		status = SkillStatusPublished
		event = "skill_publish"
	case "unpublish":
		status = SkillStatusUnpublished
		event = "skill_unpublish"
	case "submit-review", "review-pending", "review_pending":
		status = SkillStatusReviewPending
		event = "skill_review_pending"
	case "disable":
		status = SkillStatusDisabled
		event = "skill_disable"
	case "archive":
		status = SkillStatusArchived
		event = "skill_archive"
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if status == SkillStatusPublished {
		record, err := s.skillRegistry.GetSkill(r.Context(), name)
		if err != nil {
			statusCode := http.StatusInternalServerError
			if errors.Is(err, sql.ErrNoRows) {
				statusCode = http.StatusNotFound
			}
			writeJSON(w, statusCode, map[string]string{"error": "skill not found"})
			return
		}
		review := ReviewSkillForPublication(record)
		if !review.Passed {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "skill review failed", "review": review})
			return
		}
	}
	updated, err := s.skillRegistry.SetSkillStatus(r.Context(), name, status)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			statusCode = http.StatusNotFound
		}
		writeJSON(w, statusCode, map[string]string{"error": "skill not found"})
		return
	}
	if status == SkillStatusPublished || strings.TrimSpace(body.Changelog) != "" {
		if err := s.skillRegistry.RecordSkillVersion(r.Context(), updated, body.Changelog); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if status == SkillStatusPublished || status == SkillStatusArchived {
		if err := s.skillRegistry.RecordSkillRelease(r.Context(), updated, body.Changelog, user.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	s.refreshSkillCatalog(r.Context())
	s.recordGovernanceEvent(event)
	s.auditEvent(r, event, user, map[string]any{"skill_name": updated.Name, "status": updated.Status})
	writeJSON(w, http.StatusOK, map[string]any{"skill": updated})
}

func (s *Server) handleAdminListSkillVersions(w http.ResponseWriter, r *http.Request, name string) {
	versions, err := s.skillRegistry.ListSkillVersions(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Server) handleAdminReviewSkill(w http.ResponseWriter, r *http.Request, name string) {
	record, err := s.skillRegistry.GetSkill(r.Context(), name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "skill not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"review": ReviewSkillForPublication(record)})
}

func (s *Server) handleAdminSubmitSkillReview(w http.ResponseWriter, r *http.Request, user User, name string) {
	var body struct {
		Changelog string `json:"changelog"`
	}
	if err := readOptionalJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	record, err := s.skillRegistry.GetSkill(r.Context(), name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "skill not found"})
		return
	}
	review := ReviewSkillForPublication(record)
	if !review.Passed {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "skill review failed", "review": review})
		return
	}
	updated, err := s.skillRegistry.SetSkillStatus(r.Context(), name, SkillStatusReviewPending)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.skillRegistry.RecordSkillVersion(r.Context(), updated, firstNonEmptyString(body.Changelog, "Submitted for review")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.refreshSkillCatalog(r.Context())
	s.recordGovernanceEvent("skill_review_pending")
	s.auditEvent(r, "skill_review_pending", user, map[string]any{"skill_name": updated.Name, "status": updated.Status})
	writeJSON(w, http.StatusOK, map[string]any{"skill": updated, "review": review})
}

func (s *Server) handleAdminListSkillReleases(w http.ResponseWriter, r *http.Request, name string) {
	releases, err := s.skillRegistry.ListSkillReleases(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": releases})
}

func (s *Server) handleAdminRollbackSkill(w http.ResponseWriter, r *http.Request, user User, name string) {
	var body struct {
		Version     string `json:"version"`
		ContentHash string `json:"content_hash"`
		Status      string `json:"status"`
		Changelog   string `json:"changelog"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	current, err := s.skillRegistry.GetSkill(r.Context(), name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "skill not found"})
		return
	}
	versions, err := s.skillRegistry.ListSkillVersions(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	version, ok := findSkillVersion(versions, body.Version, body.ContentHash)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "skill version not found"})
		return
	}
	rolled := skillRegistryRecordFromVersion(current, version)
	if body.Status != "" {
		if !validSkillStatus(body.Status) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid skill status"})
			return
		}
		rolled.Status = normalizeSkillStatus(body.Status)
	}
	if rolled.Status == SkillStatusPublished {
		review := ReviewSkillForPublication(rolled)
		if !review.Passed {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "skill review failed", "review": review})
			return
		}
	}
	updated, err := s.skillRegistry.UpdateSkill(r.Context(), rolled)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	changelog := firstNonEmptyString(body.Changelog, "Rolled back to version "+version.Version)
	if err := s.skillRegistry.RecordSkillVersion(r.Context(), updated, changelog); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if updated.Status == SkillStatusPublished {
		if err := s.skillRegistry.RecordSkillRelease(r.Context(), updated, changelog, user.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	s.refreshSkillCatalog(r.Context())
	s.recordGovernanceEvent("skill_rollback")
	s.auditEvent(r, "skill_rollback", user, map[string]any{"skill_name": updated.Name, "version": updated.Version, "content_hash": updated.ContentHash})
	writeJSON(w, http.StatusOK, map[string]any{"skill": updated, "rolled_back_from": current})
}

func (s *Server) handleAdminSkillVersionDiff(w http.ResponseWriter, r *http.Request, name string) {
	current, err := s.skillRegistry.GetSkill(r.Context(), name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "skill not found"})
		return
	}
	versions, err := s.skillRegistry.ListSkillVersions(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	from, ok := resolveSkillDiffRecord(current, versions, r.URL.Query().Get("from_version"), r.URL.Query().Get("from_hash"), r.URL.Query().Get("from"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "from skill version not found"})
		return
	}
	to, ok := resolveSkillDiffRecord(current, versions, r.URL.Query().Get("to_version"), r.URL.Query().Get("to_hash"), r.URL.Query().Get("to"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "to skill version not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"diff": diffSkillRegistryRecords(from, to), "from": from, "to": to})
}

func (s *Server) handleAdminListSkillExecutions(w http.ResponseWriter, r *http.Request, name string) {
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime is not configured"})
		return
	}
	records, err := s.runtime.ListSkillExecutions(r.Context(), skillExecutionFilterFromRequest(r, name))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"executions": records})
}

func (s *Server) handleAdminSkillAnalytics(w http.ResponseWriter, r *http.Request, name string) {
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime is not configured"})
		return
	}
	summary, err := s.runtime.SummarizeSkillExecutions(r.Context(), skillExecutionFilterFromRequest(r, name))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": summary})
}

func skillExecutionFilterFromRequest(r *http.Request, name string) SkillExecutionFilter {
	query := r.URL.Query()
	limit := 100
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	return SkillExecutionFilter{
		SkillName: strings.TrimSpace(name),
		Status:    query.Get("status"),
		UserID:    query.Get("user_id"),
		SessionID: query.Get("session_id"),
		JobID:     query.Get("job_id"),
		Limit:     limit,
	}
}

func (s *Server) refreshSkillCatalog(ctx context.Context) {
	if s == nil || s.runtime == nil || s.skillRegistry == nil {
		return
	}
	records, err := s.skillRegistry.ListSkills(ctx)
	if err != nil {
		s.logEvent("skill_registry_refresh_error", map[string]any{"error": err.Error()})
		return
	}
	if registry, ok := s.runtime.skills.(*RegistrySkillCatalog); ok {
		registry.SetRecords(records)
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func validSkillStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case SkillStatusDraft, SkillStatusReviewPending, "review-pending", "review", SkillStatusPublished, SkillStatusUnpublished, SkillStatusDisabled, SkillStatusArchived:
		return true
	default:
		return false
	}
}

func findSkillVersion(versions []SkillVersionRecord, version, contentHash string) (SkillVersionRecord, bool) {
	version = strings.TrimSpace(version)
	contentHash = strings.TrimSpace(contentHash)
	for _, record := range versions {
		if version != "" && record.Version != version {
			continue
		}
		if contentHash != "" && record.ContentHash != contentHash {
			continue
		}
		if version == "" && contentHash == "" {
			continue
		}
		return record, true
	}
	return SkillVersionRecord{}, false
}

func skillRegistryRecordFromVersion(current SkillRegistryRecord, version SkillVersionRecord) SkillRegistryRecord {
	record := current
	record.Version = strings.TrimSpace(version.Version)
	record.ContentHash = strings.TrimSpace(version.ContentHash)
	if value := skillMetadataValueString(version.Metadata["display_name"]); value != "" {
		record.DisplayName = value
	}
	if value := skillMetadataValueString(version.Metadata["description"]); value != "" {
		record.Description = value
	}
	if value := skillMetadataValueString(version.Metadata["category"]); value != "" {
		record.Category = value
	}
	if value := skillMetadataValueString(version.Metadata["icon"]); value != "" {
		record.Icon = value
	}
	if value := skillMetadataValueString(version.Metadata["status"]); value != "" {
		record.Status = normalizeSkillStatus(value)
	}
	if value := skillMetadataValueString(version.Metadata["source"]); value != "" {
		record.Source = value
	}
	if value := skillMetadataValueString(version.Metadata["skill_root"]); value != "" {
		record.SkillRoot = value
	}
	if metadata, ok := version.Metadata["metadata"].(map[string]any); ok {
		record.Metadata = copySkillMetadata(metadata)
	}
	return normalizeSkillRegistryRecord(record)
}

func resolveSkillDiffRecord(current SkillRegistryRecord, versions []SkillVersionRecord, version, hash, alias string) (SkillRegistryRecord, bool) {
	alias = strings.ToLower(strings.TrimSpace(alias))
	if alias == "current" || (alias == "" && strings.TrimSpace(version) == "" && strings.TrimSpace(hash) == "") {
		return normalizeSkillRegistryRecord(current), true
	}
	if alias != "" && alias != "current" {
		version = alias
	}
	found, ok := findSkillVersion(versions, version, hash)
	if !ok {
		return SkillRegistryRecord{}, false
	}
	return skillRegistryRecordFromVersion(current, found), true
}

func diffSkillRegistryRecords(from, to SkillRegistryRecord) []map[string]any {
	fields := []struct {
		name string
		a    any
		b    any
	}{
		{"display_name", from.DisplayName, to.DisplayName},
		{"description", from.Description, to.Description},
		{"category", from.Category, to.Category},
		{"icon", from.Icon, to.Icon},
		{"status", from.Status, to.Status},
		{"version", from.Version, to.Version},
		{"source", from.Source, to.Source},
		{"skill_root", from.SkillRoot, to.SkillRoot},
		{"content_hash", from.ContentHash, to.ContentHash},
		{"metadata", stableJSONMap(from.Metadata), stableJSONMap(to.Metadata)},
	}
	out := make([]map[string]any, 0)
	for _, field := range fields {
		if fmt.Sprint(field.a) == fmt.Sprint(field.b) {
			continue
		}
		out = append(out, map[string]any{
			"field": field.name,
			"from":  field.a,
			"to":    field.b,
		})
	}
	return out
}

func stableJSONMap(value map[string]any) string {
	if len(value) == 0 {
		return "{}"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func (s *Server) handleCreateAttachment(w http.ResponseWriter, r *http.Request, user User) {
	maxBytes := s.runtime.MaxAssetBytes()
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeJSONError(w, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file is required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		writeJSONError(w, err)
		return
	}
	if int64(len(data)) > maxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("file exceeds max size of %d bytes", maxBytes)})
		return
	}
	filename := header.Filename
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(filename))
	}
	attachment, err := s.runtime.CreateAttachment(r.Context(), user.ID, r.FormValue("session_id"), filename, contentType, data)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "attachment_create", user, map[string]any{
		"session_id": attachment.SessionID,
		"asset_id":   attachment.ID,
		"filename":   attachment.Filename,
		"size_bytes": attachment.SizeBytes,
	})
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) handleCreateAttachmentPresign(w http.ResponseWriter, r *http.Request, user User) {
	var req struct {
		SessionID   string `json:"session_id"`
		Filename    string `json:"filename" validate:"notblank"`
		ContentType string `json:"content_type"`
		SizeBytes   int64  `json:"size_bytes" validate:"gt=0"`
		TTLSeconds  int64  `json:"ttl_seconds,omitempty" validate:"gte=0"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSONError(w, err)
		return
	}
	ttl := time.Duration(req.TTLSeconds) * time.Second
	upload, err := s.runtime.CreatePresignedAttachmentUpload(r.Context(), user.ID, req.SessionID, req.Filename, req.ContentType, req.SizeBytes, ttl)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "attachment_presign", user, map[string]any{
		"session_id":    req.SessionID,
		"attachment_id": upload.AttachmentID,
		"filename":      req.Filename,
		"size_bytes":    req.SizeBytes,
	})
	writeJSON(w, http.StatusCreated, upload)
}

func (s *Server) handleConfirmAttachmentUpload(w http.ResponseWriter, r *http.Request, user User, attachmentID string) {
	var req struct {
		SessionID   string `json:"session_id"`
		Filename    string `json:"filename" validate:"notblank"`
		ContentType string `json:"content_type"`
		SizeBytes   int64  `json:"size_bytes" validate:"gt=0"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSONError(w, err)
		return
	}
	attachment, err := s.runtime.ConfirmAttachmentUpload(r.Context(), user.ID, req.SessionID, attachmentID, req.Filename, req.ContentType, req.SizeBytes)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "attachment_confirm", user, map[string]any{
		"session_id":    attachment.SessionID,
		"attachment_id": attachment.ID,
		"filename":      attachment.Filename,
		"size_bytes":    attachment.SizeBytes,
	})
	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request, user User) {
	items, err := s.runtime.ListAttachments(r.Context(), user.ID, r.URL.Query().Get("session_id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attachments": items})
}

func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request, user User) {
	items, err := s.runtime.ListArtifacts(r.Context(), user.ID, r.URL.Query().Get("session_id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": items})
}

func (s *Server) handleDownloadAttachment(w http.ResponseWriter, r *http.Request, user User, attachmentID string) {
	attachment, data, err := s.runtime.GetAttachment(r.Context(), user.ID, attachmentID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "attachment not found"})
		return
	}
	writeAssetDownload(w, attachment, data)
}

func (s *Server) handleDownloadArtifact(w http.ResponseWriter, r *http.Request, user User, artifactID string) {
	artifact, data, err := s.runtime.GetArtifact(r.Context(), user.ID, artifactID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	writeAssetDownload(w, artifact, data)
}

func (s *Server) handlePreviewArtifact(w http.ResponseWriter, r *http.Request, user User, artifactID string) {
	artifact, data, err := s.runtime.GetArtifact(r.Context(), user.ID, artifactID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	if !isDOCXAsset(artifact) {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "preview is not available for this artifact type"})
		return
	}
	preview, err := renderDOCXPreviewHTML(artifact, data)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename="+strconvQuote(strings.TrimSuffix(artifact.Filename, filepath.Ext(artifact.Filename))+".html"))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(preview)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request, user User, attachmentID string) {
	if err := s.runtime.DeleteAttachment(r.Context(), user.ID, attachmentID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "attachment_delete", user, map[string]any{"attachment_id": attachmentID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request, user User, artifactID string) {
	if err := s.runtime.DeleteArtifact(r.Context(), user.ID, artifactID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "artifact_delete", user, map[string]any{"artifact_id": artifactID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleExtractAssetMemory(w http.ResponseWriter, r *http.Request, user User, kind, assetID string) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "asset ID is required"})
		return
	}
	var body MemoryAssetExtractionOptions
	if r.Body != nil {
		if err := readOptionalJSON(r, &body); err != nil {
			writeJSONError(w, err)
			return
		}
	}
	items, err := s.runtime.ExtractMemoryFromAsset(r.Context(), user.ID, kind, assetID, body)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "memory_extract_asset", user, map[string]any{
		"asset_id":   assetID,
		"asset_kind": normalizeAssetKind(kind),
		"count":      len(items),
	})
	s.recordGovernanceEvent("memory_extract_asset")
	s.recordPIIRedactions(items)
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func writeAssetDownload(w http.ResponseWriter, asset *Artifact, data []byte) {
	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename="+strconvQuote(asset.Filename))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	_, _ = w.Write(data)
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request, user User) {
	var body createJobRequest
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	s.scanAndRecordRisk(r, RiskScanTarget{
		Kind:      "job_prompt",
		UserID:    user.ID,
		SessionID: body.SessionID,
		Content:   body.Content,
	})
	job, err := s.runtime.CreateJob(r.Context(), ChatRequest{UserID: user.ID, SessionID: body.SessionID, Content: body.Content, AttachmentIDs: body.AttachmentIDs, AttachmentURLs: body.AttachmentURLs}, body.Type)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	if err := s.runtime.StartJob(r.Context(), job); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "job_create", user, map[string]any{"session_id": job.SessionID, "job_id": job.ID, "job_type": job.Type})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request, user User) {
	jobs, err := s.runtime.ListJobs(r.Context(), user.ID, r.URL.Query().Get("session_id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s *Server) handleSearchMessages(w http.ResponseWriter, r *http.Request, user User) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := parseBoundedInt(r.URL.Query().Get("limit"), 20, 1, 100)
	offset := parseBoundedInt(r.URL.Query().Get("offset"), 0, 0, 10000)
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": []MessageSearchResult{}, "limit": limit, "offset": offset})
		return
	}
	items, err := s.runtime.SearchMessages(r.Context(), user.ID, query, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request, user User, jobID string) {
	job, err := s.runtime.GetJob(r.Context(), user.ID, jobID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request, user User, jobID string) {
	if r.URL.Query().Get("stream") == "1" || strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		s.streamJobEvents(w, r, user, jobID)
		return
	}
	limit := 500
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 2000 {
			limit = parsed
		}
	}
	events, err := s.runtime.ListJobEvents(r.Context(), user.ID, jobID, jobEventCursor(r), limit)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) streamJobEvents(w http.ResponseWriter, r *http.Request, user User, jobID string) {
	if _, err := s.runtime.GetJob(r.Context(), user.ID, jobID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	updates, unsubscribe := s.runtime.subscribeJobEvents(jobID)
	defer unsubscribe()
	sink, err := newSSEEventSink(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	afterID := jobEventCursor(r)
	seen := make(map[string]struct{})
	sendRecord := func(record *JobEvent) error {
		if record == nil {
			return nil
		}
		if _, ok := seen[record.ID]; ok {
			return nil
		}
		seen[record.ID] = struct{}{}
		afterID = record.ID
		return sink.SendJobEvent(r.Context(), record)
	}
	for {
		events, err := s.runtime.ListJobEvents(r.Context(), user.ID, jobID, afterID, 500)
		if err != nil {
			_ = sink.Send(r.Context(), Event{Type: "error", JobID: jobID, Error: err.Error()})
			return
		}
		for _, record := range events {
			if err := sendRecord(record); err != nil {
				return
			}
		}
		if len(events) < 500 {
			break
		}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.shutdownDone():
			_ = sink.Send(r.Context(), Event{Type: "error", JobID: jobID, Error: "server is shutting down"})
			return
		case record, ok := <-updates:
			if !ok {
				_ = sink.Send(r.Context(), Event{Type: "error", JobID: jobID, Error: "job event stream dropped because the client is too slow"})
				return
			}
			if err := sendRecord(record); err != nil {
				return
			}
		case <-ticker.C:
			job, err := s.runtime.GetJob(r.Context(), user.ID, jobID)
			if err != nil {
				_ = sink.Send(r.Context(), Event{Type: "error", JobID: jobID, Error: err.Error()})
				return
			}
			if isTerminalJobStatus(job.Status) {
				events, err := s.runtime.ListJobEvents(r.Context(), user.ID, jobID, afterID, 500)
				if err != nil {
					_ = sink.Send(r.Context(), Event{Type: "error", JobID: jobID, Error: err.Error()})
					return
				}
				for _, record := range events {
					if err := sendRecord(record); err != nil {
						return
					}
				}
				if len(events) == 0 {
					return
				}
			}
		}
	}
}

func jobEventCursor(r *http.Request) string {
	if value := strings.TrimSpace(r.URL.Query().Get("after_id")); value != "" {
		return value
	}
	return strings.TrimSpace(r.Header.Get("Last-Event-ID"))
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request, user User, jobID string) {
	if err := s.runtime.CancelJob(r.Context(), user.ID, jobID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "job_cancel", user, map[string]any{"job_id": jobID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleAccountDelete(w http.ResponseWriter, r *http.Request, user User) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := readOptionalJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	if err := s.runtime.DeleteUserData(r.Context(), user.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if s.authService != nil {
		if body.RefreshToken != "" {
			_ = s.authService.Logout(r.Context(), body.RefreshToken)
		}
		if err := s.authService.DeleteAccount(r.Context(), user.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	s.clearAuthCookies(w)
	s.logEvent("account_delete", map[string]any{"user_id": user.ID, "request_id": requestIDFromContext(r.Context())})
	s.recordGovernanceEvent("account_delete")
	s.auditEvent(r, "account_delete", user, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request, user User, sessionID string) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logf("ws_upgrade_error user=%s session=%s error=%v", user.ID, sessionID, err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	sink := &websocketEventSink{conn: conn}
	var chatMu sync.Mutex
	var running bool

	for {
		var msg struct {
			Type           string              `json:"type"`
			Content        string              `json:"content,omitempty"`
			AttachmentIDs  []string            `json:"attachment_ids,omitempty"`
			AttachmentURLs []ChatAttachmentURL `json:"attachment_urls,omitempty"`
			ThinkingMode   bool                `json:"thinking_mode,omitempty"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			cancel()
			return
		}
		switch msg.Type {
		case "chat":
			chatMu.Lock()
			if running {
				chatMu.Unlock()
				_ = sink.Send(ctx, Event{Type: "error", SessionID: sessionID, Error: "chat turn already running"})
				continue
			}
			running = true
			chatMu.Unlock()
			req := ChatRequest{UserID: user.ID, SessionID: sessionID, Content: msg.Content, AttachmentIDs: msg.AttachmentIDs, AttachmentURLs: msg.AttachmentURLs, ThinkingMode: msg.ThinkingMode}
			decision := s.runtime.RouteChat(req)
			if decision.RunAsJob {
				if _, err := s.startRoutedJob(r, ctx, user, req, decision, sink); err != nil {
					_ = sink.Send(ctx, Event{Type: "error", SessionID: sessionID, Error: err.Error()})
				}
				chatMu.Lock()
				running = false
				chatMu.Unlock()
				continue
			}
			go func(req ChatRequest) {
				defer func() {
					chatMu.Lock()
					running = false
					chatMu.Unlock()
				}()
				_ = s.runtime.Chat(ctx, req, sink)
			}(req)
		case "cancel":
			if !s.runtime.Cancel(user.ID, sessionID) {
				_ = sink.Send(ctx, Event{Type: "error", SessionID: sessionID, Error: ErrSessionNotRunning.Error()})
			}
		default:
			_ = sink.Send(ctx, Event{Type: "error", SessionID: sessionID, Error: "unknown websocket message type"})
		}
	}
}

func (s *Server) handleLiveWebSocket(w http.ResponseWriter, r *http.Request, user User, sessionID string) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logf("live_ws_upgrade_error user=%s session=%s error=%v", user.ID, sessionID, err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stats := newLiveSessionStats()
	if s.metrics != nil {
		s.metrics.IncLiveActive()
		defer s.metrics.DecLiveActive()
	}
	sink := &observedLiveEventSink{sink: &websocketEventSink{conn: conn}, stats: stats}
	stream := &observedLiveClientStream{stream: &websocketLiveClientStream{conn: conn}, stats: stats}
	err = s.runtime.Live(ctx, LiveRequest{
		UserID:       user.ID,
		SessionID:    sessionID,
		ResumeHandle: strings.TrimSpace(r.URL.Query().Get("resume_handle")),
	}, stream, sink)
	if s.metrics != nil {
		s.metrics.RecordLiveSession(stats.metrics(err))
	}
	if err != nil {
		s.logf("live_ws_error user=%s session=%s error=%v", user.ID, sessionID, err)
	}
}

type websocketLiveClientStream struct {
	conn *websocket.Conn
}

func (s *websocketLiveClientStream) ReceiveLiveClientEvent(ctx context.Context) (LiveClientEvent, error) {
	type result struct {
		event LiveClientEvent
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		var event LiveClientEvent
		err := s.conn.ReadJSON(&event)
		ch <- result{event: event, err: err}
	}()
	select {
	case <-ctx.Done():
		return LiveClientEvent{}, ctx.Err()
	case result := <-ch:
		return result.event, result.err
	}
}

type liveSessionStats struct {
	startedAt         time.Time
	firstTranscriptAt time.Time
	firstAudioAt      time.Time
	audioChunks       int64
	audioBytes        int64
	clientDevice      string
	errorCode         string
	receivedDone      bool
}

func newLiveSessionStats() *liveSessionStats {
	return &liveSessionStats{startedAt: time.Now().UTC()}
}

func (s *liveSessionStats) metrics(err error) LiveMetricsRecord {
	if s == nil {
		return LiveMetricsRecord{}
	}
	now := time.Now().UTC()
	code := s.errorCode
	if code == "" && err != nil && !isExpectedWebSocketClose(err) {
		code = liveErrorCode(err)
	}
	return LiveMetricsRecord{
		DurationMS:        now.Sub(s.startedAt).Milliseconds(),
		FirstTranscriptMS: durationSinceMillis(s.startedAt, s.firstTranscriptAt),
		FirstAudioMS:      durationSinceMillis(s.startedAt, s.firstAudioAt),
		AudioChunks:       s.audioChunks,
		AudioBytes:        s.audioBytes,
		ErrorCode:         code,
		Disconnected:      err != nil && !isExpectedWebSocketClose(err) && !s.receivedDone,
		Success:           code == "" && (err == nil || isExpectedWebSocketClose(err)),
	}
}

type observedLiveClientStream struct {
	stream LiveClientStream
	stats  *liveSessionStats
}

func (s *observedLiveClientStream) ReceiveLiveClientEvent(ctx context.Context) (LiveClientEvent, error) {
	event, err := s.stream.ReceiveLiveClientEvent(ctx)
	if err != nil || s.stats == nil {
		return event, err
	}
	switch strings.ToLower(strings.TrimSpace(event.Type)) {
	case "audio":
		s.stats.audioChunks++
		s.stats.audioBytes += int64(base64DecodedSize(event.Data))
	case "client_trace":
		s.stats.clientDevice = event.Content
	case "close":
		s.stats.receivedDone = true
	}
	return event, nil
}

type observedLiveEventSink struct {
	sink  EventSink
	stats *liveSessionStats
}

func (s *observedLiveEventSink) Send(ctx context.Context, event Event) error {
	if s.stats != nil {
		now := time.Now().UTC()
		switch event.Type {
		case "live_transcript":
			if s.stats.firstTranscriptAt.IsZero() {
				s.stats.firstTranscriptAt = now
			}
		case "live_audio":
			if s.stats.firstAudioAt.IsZero() {
				s.stats.firstAudioAt = now
			}
		case "error":
			s.stats.errorCode = eventDataString(event.Data, "code")
			if s.stats.errorCode == "" {
				s.stats.errorCode = liveErrorCode(errors.New(event.Error))
			}
		case "done":
			s.stats.receivedDone = true
		}
	}
	return s.sink.Send(ctx, event)
}

func durationSinceMillis(start, finish time.Time) int64 {
	if start.IsZero() || finish.IsZero() || finish.Before(start) {
		return 0
	}
	return finish.Sub(start).Milliseconds()
}

func base64DecodedSize(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	padding := 0
	if strings.HasSuffix(value, "==") {
		padding = 2
	} else if strings.HasSuffix(value, "=") {
		padding = 1
	}
	size := len(value)*3/4 - padding
	if size < 0 {
		return 0
	}
	return size
}

func eventDataString(data json.RawMessage, key string) string {
	if len(data) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (User, bool) {
	if s.auth == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authenticator is not configured"})
		return User{}, false
	}
	user, err := s.auth.Authenticate(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return User{}, false
	}
	if strings.TrimSpace(user.ID) == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user identity is required"})
		return User{}, false
	}
	return user, true
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request, user User) {
	var body struct {
		WorkingDir string `json:"working_dir"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	session, err := s.runtime.CreateSession(r.Context(), user.ID, body.WorkingDir)
	if err != nil {
		s.logf("create_session user=%s error=%v", user.ID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "session_create", user, map[string]any{"session_id": session.ID})
	writeJSON(w, http.StatusCreated, publicSessionView(session))
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request, user User) {
	limit := parseBoundedInt(r.URL.Query().Get("limit"), 50, 1, 500)
	offset := parseBoundedInt(r.URL.Query().Get("offset"), 0, 0, 1000000)
	sessions, err := s.runtime.ListSessionsPage(r.Context(), user.ID, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if truthyQuery(r.URL.Query().Get("summary")) {
		writeJSON(w, http.StatusOK, publicSessionSummaryViews(sessions))
		return
	}
	writeJSON(w, http.StatusOK, publicSessionViews(sessions))
}

func (s *Server) handleListSessionSummaries(w http.ResponseWriter, r *http.Request, user User) {
	limit := parseBoundedInt(r.URL.Query().Get("limit"), 50, 1, 500)
	offset := parseBoundedInt(r.URL.Query().Get("offset"), 0, 0, 1000000)
	sessions, err := s.runtime.ListSessionsPage(r.Context(), user.ID, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, publicSessionSummaryViews(sessions))
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request, user User, sessionID string) {
	session, err := s.runtime.GetSession(r.Context(), user.ID, sessionID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusOK, publicSessionView(session))
}

func publicSessionViews(sessions []*state.Session) []*state.Session {
	out := make([]*state.Session, 0, len(sessions))
	for _, session := range sessions {
		if public := publicSessionView(session); public != nil {
			out = append(out, public)
		}
	}
	return out
}

func publicSessionSummaryViews(sessions []*state.Session) []*state.Session {
	out := make([]*state.Session, 0, len(sessions))
	for _, session := range sessions {
		if public := publicSessionSummaryView(session); public != nil {
			out = append(out, public)
		}
	}
	return out
}

func publicSessionSummaryView(session *state.Session) *state.Session {
	if session == nil {
		return nil
	}
	clone := *session
	clone.WorkingDir = ""
	clone.Metadata = nil
	clone.Messages = []state.Message{}
	return &clone
}

func publicSessionView(session *state.Session) *state.Session {
	if session == nil {
		return nil
	}
	clone := *session
	clone.WorkingDir = ""
	clone.Metadata = nil
	clone.Messages = make([]state.Message, 0, len(session.Messages))
	for _, message := range session.Messages {
		if message.Hidden || message.Role == "tool" || strings.TrimSpace(firstNonEmptyString(message.Content, message.ToolOutput)) == "" {
			continue
		}
		publicMessage := state.Message{
			Role:      message.Role,
			Content:   message.Content,
			CreatedAt: message.CreatedAt,
		}
		clone.Messages = append(clone.Messages, publicMessage)
	}
	return &clone
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request, user User, sessionID string) {
	if err := s.runtime.DeleteSession(r.Context(), user.ID, sessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "session_delete", user, map[string]any{"session_id": sessionID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleDeleteSessionMemory(w http.ResponseWriter, r *http.Request, user User, sessionID string) {
	if err := s.runtime.DeleteSessionMemory(r.Context(), user.ID, sessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "memory_delete_session", user, map[string]any{"session_id": sessionID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "memory_deleted"})
}

func (s *Server) handleListMemory(w http.ResponseWriter, r *http.Request, user User) {
	limit := 100
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = parsed
		if limit > 200 {
			limit = 200
		}
	}
	visibility := strings.TrimSpace(r.URL.Query().Get("visibility"))
	if visibility == "all" {
		visibility = ""
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = MemoryStatusActive
	} else if status == "all" {
		status = ""
	}
	level := strings.TrimSpace(r.URL.Query().Get("level"))
	if level == "all" {
		level = ""
	}
	items, err := s.runtime.ListMemoryItems(r.Context(), user.ID, MemoryItemFilter{
		SessionID:  strings.TrimSpace(r.URL.Query().Get("session_id")),
		Namespace:  strings.TrimSpace(r.URL.Query().Get("namespace")),
		Kind:       strings.TrimSpace(r.URL.Query().Get("kind")),
		Level:      level,
		Category:   strings.TrimSpace(r.URL.Query().Get("category")),
		Visibility: visibility,
		Status:     status,
		Query:      strings.TrimSpace(r.URL.Query().Get("q")),
		SourceKind: strings.TrimSpace(r.URL.Query().Get("source_kind")),
		SourceID:   strings.TrimSpace(r.URL.Query().Get("source_id")),
		Limit:      limit,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetMemorySettings(w http.ResponseWriter, r *http.Request, user User) {
	settings, err := s.runtime.GetMemorySettings(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateMemorySettings(w http.ResponseWriter, r *http.Request, user User) {
	settings, err := s.runtime.GetMemorySettings(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var body struct {
		Enabled        *bool `json:"enabled"`
		CaptureEnabled *bool `json:"capture_enabled"`
		ContextEnabled *bool `json:"context_enabled"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	if body.Enabled != nil {
		if *body.Enabled {
			settings.CaptureEnabled = true
			settings.ContextEnabled = true
		} else {
			settings.CaptureEnabled = false
			settings.ContextEnabled = false
		}
	}
	if body.CaptureEnabled != nil {
		settings.CaptureEnabled = *body.CaptureEnabled
	}
	if body.ContextEnabled != nil {
		settings.ContextEnabled = *body.ContextEnabled
	}
	settings = normalizeMemorySettings(settings)
	updated, err := s.runtime.UpdateMemorySettings(r.Context(), user.ID, settings)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "memory_update_settings", user, map[string]any{
		"enabled":         updated.Enabled,
		"capture_enabled": updated.CaptureEnabled,
		"context_enabled": updated.ContextEnabled,
	})
	s.recordGovernanceEvent("memory_update_settings")
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleGetPersonalization(w http.ResponseWriter, r *http.Request, user User) {
	settings, err := s.runtime.GetPersonalizationSettings(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdatePersonalization(w http.ResponseWriter, r *http.Request, user User) {
	settings, err := s.runtime.GetPersonalizationSettings(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var body struct {
		Profile            map[string]*string `json:"profile"`
		Style              map[string]*string `json:"style"`
		Traits             map[string]*string `json:"traits"`
		CustomInstructions *string            `json:"custom_instructions"`
		FeatureFlags       map[string]*bool   `json:"feature_flags"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	previous := settings
	applyPersonalizationProfilePatch(&settings.Profile, body.Profile)
	applyPersonalizationStylePatch(&settings.Style, body.Style)
	applyPersonalizationTraitsPatch(&settings.Traits, body.Traits)
	if body.CustomInstructions != nil {
		settings.CustomInstructions = *body.CustomInstructions
	}
	applyPersonalizationFeatureFlagPatch(&settings.FeatureFlags, body.FeatureFlags)
	updated, err := s.runtime.UpdatePersonalizationSettings(r.Context(), user.ID, settings)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.recordPersonalizationMetrics(updated, !personalizationSettingsEqual(previous, updated))
	s.auditEvent(r, "personalization_update_settings", user, map[string]any{
		"style_preset":       updated.Style.Preset,
		"tone":               updated.Style.Tone,
		"quick_answers":      updated.FeatureFlags.QuickAnswers,
		"use_saved_memory":   updated.FeatureFlags.UseSavedMemory,
		"use_chat_history":   updated.FeatureFlags.UseChatHistory,
		"use_browser_memory": updated.FeatureFlags.UseBrowserMemory,
	})
	s.recordGovernanceEvent("personalization_update_settings")
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleResetPersonalization(w http.ResponseWriter, r *http.Request, user User) {
	previous, previousErr := s.runtime.GetPersonalizationSettings(r.Context(), user.ID)
	settings, err := s.runtime.DeletePersonalizationSettings(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	changed := true
	if previousErr == nil {
		changed = !personalizationSettingsEqual(previous, settings)
	}
	s.recordPersonalizationMetrics(settings, changed)
	s.auditEvent(r, "personalization_reset_settings", user, nil)
	s.recordGovernanceEvent("personalization_reset_settings")
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleCreateBrowserMemory(w http.ResponseWriter, r *http.Request, user User) {
	var body BrowserMemoryRequest
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	item, err := s.runtime.SaveBrowserMemory(r.Context(), user.ID, body)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "personalization_browser_memory_create", user, map[string]any{
		"memory_id": item.ID,
		"url":       item.Metadata["browser_url"],
	})
	if s.metrics != nil {
		s.metrics.IncPersonalizationBrowserMemory()
	}
	s.recordGovernanceEvent("personalization_browser_memory_create")
	writeJSON(w, http.StatusCreated, item)
}

func applyPersonalizationProfilePatch(profile *PersonalizationProfile, patch map[string]*string) {
	if profile == nil || patch == nil {
		return
	}
	for key, value := range patch {
		if value == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "nickname":
			profile.Nickname = *value
		case "occupation":
			profile.Occupation = *value
		case "about":
			profile.About = *value
		}
	}
}

func applyPersonalizationStylePatch(style *PersonalizationStyle, patch map[string]*string) {
	if style == nil || patch == nil {
		return
	}
	for key, value := range patch {
		if value == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "preset":
			style.Preset = *value
		case "tone":
			style.Tone = *value
		}
	}
}

func applyPersonalizationTraitsPatch(traits *PersonalizationTraits, patch map[string]*string) {
	if traits == nil || patch == nil {
		return
	}
	for key, value := range patch {
		if value == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "warmth":
			traits.Warmth = *value
		case "enthusiasm":
			traits.Enthusiasm = *value
		case "headings_and_lists":
			traits.HeadingsAndLists = *value
		case "emoji":
			traits.Emoji = *value
		}
	}
}

func applyPersonalizationFeatureFlagPatch(flags *PersonalizationFeatureFlags, patch map[string]*bool) {
	if flags == nil || patch == nil {
		return
	}
	for key, value := range patch {
		if value == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "quick_answers":
			flags.QuickAnswers = *value
		case "use_saved_memory":
			flags.UseSavedMemory = *value
		case "use_chat_history":
			flags.UseChatHistory = *value
		case "use_browser_memory":
			flags.UseBrowserMemory = *value
		}
	}
}

func (s *Server) handleUpdateMemoryItem(w http.ResponseWriter, r *http.Request, user User, itemID string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "memory item ID is required"})
		return
	}
	existing, err := s.runtime.GetMemoryItem(r.Context(), user.ID, itemID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "memory item not found"})
		return
	}
	var body struct {
		Content    *string  `json:"content"`
		Namespace  *string  `json:"namespace"`
		Category   *string  `json:"category"`
		Tags       []string `json:"tags"`
		Visibility *string  `json:"visibility"`
		Status     *string  `json:"status"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	before := existing.Content
	if body.Content != nil {
		content, metadata := sanitizeMemoryContent(*body.Content)
		existing.Content = content
		if existing.Metadata == nil {
			existing.Metadata = map[string]any{}
		}
		for key, value := range metadata {
			existing.Metadata[key] = value
		}
		existing.RawHash = memoryRawHash(existing.Category, existing.Content)
	}
	if body.Category != nil {
		existing.Category = normalizeMemoryCategory(*body.Category)
		existing.RawHash = memoryRawHash(existing.Category, existing.Content)
	}
	if body.Namespace != nil && strings.TrimSpace(*body.Namespace) != "" {
		existing.Namespace = normalizeMemoryNamespace(*body.Namespace)
	}
	if body.Tags != nil {
		existing.Tags = normalizeMemoryTags(body.Tags)
	}
	if body.Visibility != nil && strings.TrimSpace(*body.Visibility) != "" {
		existing.Visibility = normalizeMemoryVisibility(*body.Visibility)
	}
	if body.Status != nil && strings.TrimSpace(*body.Status) != "" {
		existing.Status = strings.TrimSpace(*body.Status)
	}
	existing.Source = MemorySourceUserEdit
	existing.Confidence = 1
	existing.Weight = computeMemoryWeight(existing.Category, 1, existing.Confidence, time.Now().UTC(), existing.AccessCount)
	updated, err := s.runtime.UpdateMemoryItem(r.Context(), user.ID, existing)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "memory_update_item", user, map[string]any{
		"memory_id": itemID,
		"before":    truncateForMemory(before),
		"after":     truncateForMemory(updated.Content),
	})
	s.recordGovernanceEvent("memory_update_item")
	s.recordPIIRedactions([]MemoryItem{updated})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleMemoryFeedback(w http.ResponseWriter, r *http.Request, user User, itemID string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "memory item ID is required"})
		return
	}
	var body struct {
		Type string `json:"type" validate:"oneof=important incorrect not_relevant"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	updated, err := s.runtime.ApplyMemoryFeedback(r.Context(), user.ID, itemID, body.Type)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "memory_feedback", user, map[string]any{"memory_id": itemID, "type": body.Type})
	s.recordGovernanceEvent("memory_feedback_" + strings.TrimSpace(body.Type))
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleResolveMemory(w http.ResponseWriter, r *http.Request, user User, itemID string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "memory item ID is required"})
		return
	}
	var body struct {
		Action string `json:"action" validate:"oneof=accept reject keep_both"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	updated, err := s.runtime.ResolveMemoryConflict(r.Context(), user.ID, itemID, body.Action)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "memory_resolve_conflict", user, map[string]any{"memory_id": itemID, "action": body.Action})
	s.recordGovernanceEvent("memory_resolve_conflict")
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleRebuildMemory(w http.ResponseWriter, r *http.Request, user User) {
	items, err := s.runtime.RebuildMemoryAbstractions(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "memory_rebuild_abstractions", user, map[string]any{"count": len(items)})
	s.recordGovernanceEvent("memory_rebuild_abstractions")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleScoreMemory(w http.ResponseWriter, r *http.Request, user User) {
	items, err := s.runtime.ScoreMemoryQuality(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "memory_score_quality", user, map[string]any{"count": len(items)})
	s.recordGovernanceEvent("memory_score_quality")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleListMemoryMaintenance(w http.ResponseWriter, r *http.Request, user User) {
	actions, err := s.runtime.PlanMemoryMaintenance(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

func (s *Server) handleRunMemoryMaintenance(w http.ResponseWriter, r *http.Request, user User) {
	if _, err := s.runtime.ScoreMemoryQuality(r.Context(), user.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	report, err := s.runtime.RunMemoryMaintenance(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "memory_run_maintenance", user, map[string]any{"planned": len(report.Planned), "applied": len(report.Applied), "pending": len(report.Actions)})
	s.recordGovernanceEvent("memory_run_maintenance")
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleApplyMemoryMaintenance(w http.ResponseWriter, r *http.Request, user User, actionID string) {
	action, err := s.runtime.ApplyMemoryMaintenance(r.Context(), user.ID, actionID)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "memory_apply_maintenance", user, map[string]any{"action_id": action.ID, "type": action.Type})
	s.recordGovernanceEvent("memory_apply_maintenance_" + strings.TrimSpace(action.Type))
	writeJSON(w, http.StatusOK, action)
}

func (s *Server) handleDismissMemoryMaintenance(w http.ResponseWriter, r *http.Request, user User, actionID string) {
	action, err := s.runtime.DismissMemoryMaintenance(r.Context(), user.ID, actionID)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "memory_dismiss_maintenance", user, map[string]any{"action_id": action.ID, "type": action.Type})
	s.recordGovernanceEvent("memory_dismiss_maintenance")
	writeJSON(w, http.StatusOK, action)
}

func (s *Server) handleDeleteMemoryItem(w http.ResponseWriter, r *http.Request, user User, itemID string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "memory item ID is required"})
		return
	}
	if err := s.runtime.DeleteMemoryItem(r.Context(), user.ID, itemID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "memory_delete_item", user, map[string]any{"memory_id": itemID})
	s.recordGovernanceEvent("memory_delete_item")
	writeJSON(w, http.StatusOK, map[string]string{"status": "memory_deleted"})
}

func (s *Server) handleDeleteAllMemory(w http.ResponseWriter, r *http.Request, user User) {
	if err := s.runtime.DeleteUserMemory(r.Context(), user.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "memory_delete_user", user, nil)
	s.recordGovernanceEvent("memory_delete_user")
	writeJSON(w, http.StatusOK, map[string]string{"status": "memory_deleted"})
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request, user User, sessionID string) {
	var body chatMessageRequest
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	s.scanAndRecordRisk(r, RiskScanTarget{
		Kind:      "prompt",
		UserID:    user.ID,
		SessionID: sessionID,
		Content:   body.Content,
	})

	sink, err := newSSEEventSink(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logEvent("chat_start", map[string]any{"user_id": user.ID, "session_id": sessionID, "chars": len(body.Content), "request_id": requestIDFromContext(r.Context())})
	req := ChatRequest{UserID: user.ID, SessionID: sessionID, Content: body.Content, AttachmentIDs: body.AttachmentIDs, AttachmentURLs: body.AttachmentURLs, ThinkingMode: body.ThinkingMode}
	decision := s.runtime.RouteChat(req)
	if decision.RunAsJob {
		if _, err := s.startRoutedJob(r, r.Context(), user, req, decision, sink); err != nil && !errors.Is(err, context.Canceled) {
			s.logEvent("job_route_error", map[string]any{"user_id": user.ID, "session_id": sessionID, "error": err.Error(), "request_id": requestIDFromContext(r.Context())})
		}
		return
	}
	err = s.runtime.Chat(r.Context(), req, sink)
	if err != nil && !errors.Is(err, context.Canceled) {
		s.logEvent("chat_error", map[string]any{"user_id": user.ID, "session_id": sessionID, "error": err.Error(), "request_id": requestIDFromContext(r.Context())})
	}
}

func (s *Server) handleCancel(w http.ResponseWriter, _ *http.Request, user User, sessionID string) {
	if !s.runtime.Cancel(user.ID, sessionID) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": ErrSessionNotRunning.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) startRoutedJob(r *http.Request, ctx context.Context, user User, req ChatRequest, decision JobRoutingDecision, sink EventSink) (*Job, error) {
	req.UserID = user.ID
	job, err := s.runtime.CreateJob(ctx, req, firstNonEmptyString(decision.JobType, "chat"))
	if err != nil {
		_ = sink.Send(ctx, Event{Type: "error", SessionID: req.SessionID, Error: err.Error()})
		return nil, err
	}
	if err := s.runtime.StartJob(ctx, job); err != nil {
		_ = sink.Send(ctx, Event{Type: "error", SessionID: req.SessionID, JobID: job.ID, Error: err.Error()})
		return nil, err
	}
	s.logEvent("chat_routed_to_job", map[string]any{"user_id": user.ID, "session_id": req.SessionID, "job_id": job.ID, "job_type": job.Type, "reason": decision.Reason, "request_id": requestIDFromContext(ctx)})
	if r != nil {
		s.auditEvent(r, "job_create", user, map[string]any{"session_id": req.SessionID, "job_id": job.ID, "job_type": job.Type, "route_reason": decision.Reason})
	}
	return job, sink.Send(ctx, Event{Type: "job", SessionID: req.SessionID, JobID: job.ID, Job: job, JobReason: decision.Reason})
}

func (s *Server) handleListSkills(w http.ResponseWriter, _ *http.Request) {
	type skillView struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name,omitempty"`
		Description string `json:"description,omitempty"`
		Category    string `json:"category,omitempty"`
		Icon        string `json:"icon,omitempty"`
		Version     string `json:"version,omitempty"`
		Usage       string `json:"usage,omitempty"`
		RunAsJob    bool   `json:"run_as_job,omitempty"`
	}
	skills := s.runtime.ListSkills()
	out := make([]skillView, 0, len(skills))
	for _, skill := range skills {
		view := skillView{
			Name:        skill.Name,
			DisplayName: skill.DisplayName,
			Description: skill.Description,
			Version:     skill.Version,
			Usage:       skill.ArgumentHint,
			RunAsJob:    skill.RunAsJob || skill.ExecutionContext == skillpkg.ContextFork,
		}
		if registry, ok := s.runtime.skills.(*RegistrySkillCatalog); ok {
			if record, ok := registry.SkillRecord(skill.Name); ok {
				view.Category = record.Category
				view.Icon = record.Icon
				if view.Version == "" {
					view.Version = record.Version
				}
			}
		}
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

func (s *Server) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Info(fmt.Sprintf(format, args...))
	}
}

func (s *Server) logEvent(event string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["event"] = event
	if _, ok := fields["request_id"]; !ok {
		fields["request_id"] = ""
	}
	if _, ok := fields["user_id"]; !ok {
		fields["user_id"] = ""
	}
	logFields(s.logger, fields)
}

func (s *Server) recordGovernanceEvent(event string) {
	if s == nil || s.metrics == nil {
		return
	}
	event = strings.ToLower(strings.TrimSpace(event))
	event = strings.NewReplacer(" ", "_", "-", "_", "/", "_").Replace(event)
	s.metrics.IncGovernanceEvent(event)
}

func (s *Server) recordPersonalizationMetrics(settings PersonalizationSettings, changed bool) {
	if s == nil || s.metrics == nil {
		return
	}
	s.metrics.RecordPersonalizationUpdate(
		personalizationMetricsEnabled(settings),
		changed,
		personalizationFieldCoverage(settings),
	)
}

func (s *Server) recordPIIRedactions(items []MemoryItem) {
	if s == nil || s.metrics == nil {
		return
	}
	count := 0
	for _, item := range items {
		if item.Metadata == nil {
			continue
		}
		switch value := item.Metadata["pii_redacted"].(type) {
		case []string:
			count += len(value)
		case []any:
			count += len(value)
		case string:
			if strings.TrimSpace(value) != "" {
				count++
			}
		}
	}
	s.metrics.AddPIIRedactions(count)
}

type sseEventSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
	encMu   sync.Mutex
}

func newSSEEventSink(w http.ResponseWriter) (*sseEventSink, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming is not supported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	return &sseEventSink{w: w, flusher: flusher}, nil
}

func (s *sseEventSink) Send(ctx context.Context, event Event) error {
	return s.send(ctx, "", event)
}

func (s *sseEventSink) SendJobEvent(ctx context.Context, record *JobEvent) error {
	if record == nil {
		return nil
	}
	event := record.Event
	if event.JobID == "" {
		event.JobID = record.JobID
	}
	if event.SessionID == "" {
		event.SessionID = record.SessionID
	}
	return s.send(ctx, record.ID, event)
}

func (s *sseEventSink) send(ctx context.Context, id string, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	s.encMu.Lock()
	defer s.encMu.Unlock()
	if strings.TrimSpace(id) != "" {
		if _, err := fmt.Fprintf(s.w, "id: %s\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\n", event.Type); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

type websocketEventSink struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *websocketEventSink) Send(ctx context.Context, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteJSON(event)
}

func sameHostOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	origin = strings.TrimPrefix(origin, "http://")
	origin = strings.TrimPrefix(origin, "https://")
	if slash := strings.Index(origin, "/"); slash >= 0 {
		origin = origin[:slash]
	}
	return strings.EqualFold(origin, r.Host)
}

func readJSON(r *http.Request, v any) error {
	return httpjson.Decode(r, v)
}

func readOptionalJSON(r *http.Request, v any) error {
	return httpjson.DecodeOptional(r, v)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	httpjson.WriteWithOptions(w, status, value, httpjson.WriteOptions{Normalize: normalizeAPIResponse})
}

func parseBoundedInt(value string, fallback, minValue, maxValue int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func truthyQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func strconvQuote(value string) string {
	value = strings.ReplaceAll(filepath.Base(value), `"`, "")
	return `"` + value + `"`
}
