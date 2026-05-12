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
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	skillpkg "claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"

	"github.com/gorilla/websocket"
)

const defaultRateLimitWindow = time.Minute

type Server struct {
	runtime          *Runtime
	auth             Authenticator
	authService      *AuthService
	limiter          RateLimitPolicy
	logger           *log.Logger
	upgrader         websocket.Upgrader
	security         WebSecurityConfig
	llmStatus        func() LLMGovernanceStatus
	llmUsage         LLMUsageAdminStore
	metrics          *MetricsRegistry
	audit            AuditLogger
	risk             RiskStore
	riskScanner      RiskScanner
	operationLimiter *OperationRateLimiter
	adminToken       string
	skillRegistry    SkillRegistryAdminStore
	readyMu          sync.RWMutex
	readyChecks      map[string]readinessCheck
	shutdownOnce     sync.Once
	shutdownCh       chan struct{}
}

func NewServer(runtime *Runtime, auth Authenticator, limiter RateLimitPolicy, logger *log.Logger) *Server {
	if limiter == nil {
		limiter = NewRateLimiter(60, defaultRateLimitWindow)
	}
	return &Server{
		runtime: runtime,
		auth:    auth,
		limiter: limiter,
		logger:  logger,
		metrics: NewMetricsRegistry(),
		upgrader: websocket.Upgrader{
			CheckOrigin: sameHostOrigin,
		},
		readyChecks: make(map[string]readinessCheck),
		shutdownCh:  make(chan struct{}),
	}
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
	started := time.Now()
	reqID := requestID(r)
	rec := &statusRecorder{ResponseWriter: w}
	rec.Header().Set("X-Request-ID", reqID)
	r = r.WithContext(withRequestID(r.Context(), reqID))
	defer func() {
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(started)
		if s.metrics != nil {
			s.metrics.RecordRequest(r.Method, r.URL.Path, status, duration)
		}
		logJSON(s.logger, map[string]any{
			"event":       "request",
			"request_id":  reqID,
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      status,
			"bytes":       rec.bytes,
			"duration_ms": duration.Milliseconds(),
		})
	}()

	if !s.applyCORS(rec, r) {
		writeJSON(rec, http.StatusForbidden, map[string]string{"error": "origin is not allowed"})
		return
	}
	if r.Method == http.MethodOptions {
		rec.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/healthz" {
		writeJSON(rec, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if r.URL.Path == "/readyz" {
		s.handleReadyz(rec, r)
		return
	}
	if s.isShuttingDown() {
		writeJSON(rec, http.StatusServiceUnavailable, map[string]string{"error": "server is shutting down"})
		return
	}
	if r.URL.Path == "/metrics" {
		s.metrics.WritePrometheus(rec)
		return
	}
	if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/app") {
		s.handleApp(rec, r)
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) >= 2 && parts[0] == "v1" && parts[1] == "auth" {
		if operation := publicAuthRiskOperation(r.Method, path); operation != "" && !s.allowPublicOperation(rec, r, operation) {
			return
		}
		if s.handlePublicAuth(rec, r, path) {
			return
		}
	}

	user, ok := s.authenticate(rec, r)
	if !ok {
		return
	}
	if !s.requireCSRF(rec, r) {
		return
	}
	if !s.limiter.Allow(user.ID) {
		if s.metrics != nil {
			s.metrics.IncRateLimited()
		}
		s.recordRiskEvent(r, RiskEvent{
			UserID:     user.ID,
			IPAddress:  clientIP(r),
			Operation:  "global_rate_limit",
			Reason:     "global_rate_limit",
			RiskLevel:  RiskLevelMedium,
			ScoreDelta: 8,
		})
		s.auditEvent(r, "risk_rate_limited", user, map[string]any{"operation": "global_rate_limit"})
		writeJSON(rec, http.StatusTooManyRequests, map[string]string{"error": "rate limited"})
		return
	}
	if operation := classifyRiskOperation(r.Method, path, parts); operation != "" && !s.allowUserOperation(rec, r, user, operation) {
		return
	}

	switch {
	case r.Method == http.MethodGet && path == "v1/auth/me":
		s.handleAuthMe(rec, r, user)
	case r.Method == http.MethodPost && path == "v1/auth/logout":
		s.handleAuthLogout(rec, r, user)
	case r.Method == http.MethodDelete && path == "v1/account":
		s.handleAccountDelete(rec, r, user)
	case r.Method == http.MethodGet && path == "v1/data/export":
		s.handleDataExport(rec, r, user)
	case path == "v1/admin/users" || (len(parts) >= 4 && parts[0] == "v1" && parts[1] == "admin" && parts[2] == "users"):
		s.handleAdminUsers(rec, r, user, parts)
	case path == "v1/admin/ops" || (len(parts) >= 4 && parts[0] == "v1" && parts[1] == "admin" && parts[2] == "ops"):
		s.handleAdminOps(rec, r, user, parts)
	case path == "v1/admin/skills" || (len(parts) >= 4 && parts[0] == "v1" && parts[1] == "admin" && parts[2] == "skills"):
		s.handleAdminSkills(rec, r, user, parts)
	case r.Method == http.MethodGet && path == "v1/memory/settings":
		s.handleGetMemorySettings(rec, r, user)
	case r.Method == http.MethodPatch && path == "v1/memory/settings":
		s.handleUpdateMemorySettings(rec, r, user)
	case r.Method == http.MethodGet && path == "v1/memory":
		s.handleListMemory(rec, r, user)
	case r.Method == http.MethodGet && path == "v1/memory/maintenance":
		s.handleListMemoryMaintenance(rec, r, user)
	case r.Method == http.MethodPost && path == "v1/memory/maintenance/run":
		s.handleRunMemoryMaintenance(rec, r, user)
	case r.Method == http.MethodPost && len(parts) == 5 && parts[0] == "v1" && parts[1] == "memory" && parts[2] == "maintenance" && parts[4] == "apply":
		s.handleApplyMemoryMaintenance(rec, r, user, parts[3])
	case r.Method == http.MethodPost && len(parts) == 5 && parts[0] == "v1" && parts[1] == "memory" && parts[2] == "maintenance" && parts[4] == "dismiss":
		s.handleDismissMemoryMaintenance(rec, r, user, parts[3])
	case r.Method == http.MethodPost && path == "v1/memory/score":
		s.handleScoreMemory(rec, r, user)
	case r.Method == http.MethodPost && path == "v1/memory/rebuild":
		s.handleRebuildMemory(rec, r, user)
	case r.Method == http.MethodDelete && path == "v1/memory":
		s.handleDeleteAllMemory(rec, r, user)
	case r.Method == http.MethodPatch && len(parts) == 3 && parts[0] == "v1" && parts[1] == "memory":
		s.handleUpdateMemoryItem(rec, r, user, parts[2])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "v1" && parts[1] == "memory" && parts[3] == "feedback":
		s.handleMemoryFeedback(rec, r, user, parts[2])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "v1" && parts[1] == "memory" && parts[3] == "resolve":
		s.handleResolveMemory(rec, r, user, parts[2])
	case r.Method == http.MethodDelete && len(parts) == 3 && parts[0] == "v1" && parts[1] == "memory":
		s.handleDeleteMemoryItem(rec, r, user, parts[2])
	case r.Method == http.MethodPost && path == "v1/attachments":
		s.handleCreateAttachment(rec, r, user)
	case r.Method == http.MethodGet && path == "v1/attachments":
		s.handleListAttachments(rec, r, user)
	case r.Method == http.MethodGet && path == "v1/artifacts":
		s.handleListArtifacts(rec, r, user)
	case r.Method == http.MethodGet && path == "v1/search/messages":
		s.handleSearchMessages(rec, r, user)
	case r.Method == http.MethodPost && path == "v1/jobs":
		s.handleCreateJob(rec, r, user)
	case r.Method == http.MethodGet && path == "v1/jobs":
		s.handleListJobs(rec, r, user)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "v1" && parts[1] == "jobs":
		s.handleGetJob(rec, r, user, parts[2])
	case r.Method == http.MethodGet && len(parts) == 4 && parts[0] == "v1" && parts[1] == "jobs" && parts[3] == "events":
		s.handleJobEvents(rec, r, user, parts[2])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "v1" && parts[1] == "jobs" && parts[3] == "cancel":
		s.handleCancelJob(rec, r, user, parts[2])
	case r.Method == http.MethodPost && path == "v1/sessions":
		s.handleCreateSession(rec, r, user)
	case r.Method == http.MethodGet && path == "v1/sessions":
		s.handleListSessions(rec, r, user)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "v1" && parts[1] == "sessions":
		s.handleGetSession(rec, r, user, parts[2])
	case r.Method == http.MethodDelete && len(parts) == 3 && parts[0] == "v1" && parts[1] == "sessions":
		s.handleDeleteSession(rec, r, user, parts[2])
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "v1" && parts[1] == "attachments":
		s.handleDownloadAttachment(rec, r, user, parts[2])
	case r.Method == http.MethodPost && len(parts) == 5 && parts[0] == "v1" && parts[1] == "attachments" && parts[3] == "memory" && parts[4] == "extract":
		s.handleExtractAssetMemory(rec, r, user, AssetKindAttachment, parts[2])
	case r.Method == http.MethodDelete && len(parts) == 3 && parts[0] == "v1" && parts[1] == "attachments":
		s.handleDeleteAttachment(rec, r, user, parts[2])
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "v1" && parts[1] == "artifacts":
		s.handleDownloadArtifact(rec, r, user, parts[2])
	case r.Method == http.MethodPost && len(parts) == 5 && parts[0] == "v1" && parts[1] == "artifacts" && parts[3] == "memory" && parts[4] == "extract":
		s.handleExtractAssetMemory(rec, r, user, AssetKindArtifact, parts[2])
	case r.Method == http.MethodDelete && len(parts) == 3 && parts[0] == "v1" && parts[1] == "artifacts":
		s.handleDeleteArtifact(rec, r, user, parts[2])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "v1" && parts[1] == "sessions" && parts[3] == "messages":
		s.handleMessage(rec, r, user, parts[2])
	case r.Method == http.MethodDelete && len(parts) == 4 && parts[0] == "v1" && parts[1] == "sessions" && parts[3] == "memory":
		s.handleDeleteSessionMemory(rec, r, user, parts[2])
	case r.Method == http.MethodGet && len(parts) == 4 && parts[0] == "v1" && parts[1] == "sessions" && parts[3] == "ws":
		s.handleWebSocket(rec, r, user, parts[2])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "v1" && parts[1] == "sessions" && parts[3] == "cancel":
		s.handleCancel(rec, r, user, parts[2])
	case r.Method == http.MethodGet && path == "v1/skills":
		s.handleListSkills(rec, r)
	case r.Method == http.MethodGet && path == "v1/llm/status":
		s.handleLLMStatus(rec, r)
	default:
		writeJSON(rec, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (s *Server) handlePublicAuth(w http.ResponseWriter, r *http.Request, path string) bool {
	switch {
	case r.Method == http.MethodPost && path == "v1/auth/register":
		s.handleAuthRegister(w, r)
		return true
	case r.Method == http.MethodPost && path == "v1/auth/login":
		s.handleAuthLogin(w, r)
		return true
	case r.Method == http.MethodPost && path == "v1/auth/refresh":
		s.handleAuthRefresh(w, r)
		return true
	default:
		return false
	}
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
	writeJSON(w, http.StatusOK, s.llmStatus())
}

func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.authService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user system is not configured"})
		return
	}
	session, err := s.authService.Register(r.Context(), body.Email, body.Password, body.DisplayName, r)
	if err != nil {
		s.recordRiskEvent(r, RiskEvent{
			IPAddress:  clientIP(r),
			Operation:  RiskOperationAuthRegister,
			Reason:     "auth_register_failed",
			RiskLevel:  RiskLevelLow,
			ScoreDelta: 3,
			Metadata:   map[string]any{"email": body.Email, "error": err.Error()},
		})
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.setAuthCookies(w, session)
	s.logEvent("auth_register", map[string]any{"user_id": session.User.ID, "request_id": requestIDFromContext(r.Context())})
	s.auditEvent(r, "auth_register", User{ID: session.User.ID}, map[string]any{"email": session.User.Email})
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

func (s *Server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

func (s *Server) handleAdminSkills(w http.ResponseWriter, r *http.Request, user User, parts []string) {
	if !s.requireAdmin(w, r) {
		return
	}
	if s.skillRegistry == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "skill registry is not configured"})
		return
	}
	switch {
	case r.Method == http.MethodGet && len(parts) == 3:
		s.handleAdminListSkills(w, r)
	case r.Method == http.MethodPatch && len(parts) == 4:
		s.handleAdminUpdateSkill(w, r, user, parts[3])
	case r.Method == http.MethodGet && len(parts) == 5 && parts[4] == "versions":
		s.handleAdminListSkillVersions(w, r, parts[3])
	case r.Method == http.MethodPost && len(parts) == 5 && parts[4] == "review":
		s.handleAdminReviewSkill(w, r, parts[3])
	case r.Method == http.MethodGet && len(parts) == 5 && parts[4] == "executions":
		s.handleAdminListSkillExecutions(w, r, parts[3])
	case r.Method == http.MethodGet && len(parts) == 5 && parts[4] == "analytics":
		s.handleAdminSkillAnalytics(w, r, parts[3])
	case r.Method == http.MethodPost && len(parts) == 5:
		s.handleAdminSetSkillStatus(w, r, user, parts[3], parts[4])
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
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

func (s *Server) handleAdminOps(w http.ResponseWriter, r *http.Request, user User, parts []string) {
	if !s.requireAdmin(w, r) {
		return
	}
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime is not configured"})
		return
	}
	switch {
	case r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "sessions":
		s.handleAdminOpsListSessions(w, r)
	case r.Method == http.MethodGet && len(parts) == 5 && parts[3] == "sessions":
		s.handleAdminOpsGetSession(w, r, parts[4])
	case r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "jobs":
		s.handleAdminOpsListJobs(w, r)
	case r.Method == http.MethodGet && len(parts) == 5 && parts[3] == "jobs":
		s.handleAdminOpsGetJob(w, r, parts[4])
	case r.Method == http.MethodGet && len(parts) == 6 && parts[3] == "jobs" && parts[5] == "events":
		s.handleAdminOpsListJobEvents(w, r, parts[4])
	case r.Method == http.MethodPost && len(parts) == 6 && parts[3] == "jobs" && parts[5] == "cancel":
		s.handleAdminOpsCancelJob(w, r, user, parts[4])
	case r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "assets":
		s.handleAdminOpsListAssets(w, r)
	case r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "health":
		s.handleAdminOpsHealth(w, r)
	case r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "llm-usage":
		s.handleAdminOpsLLMUsage(w, r)
	case r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "quota":
		s.handleAdminOpsQuota(w, r)
	case r.Method == http.MethodPost && len(parts) == 5 && parts[3] == "quota" && parts[4] == "reset":
		s.handleAdminOpsQuotaReset(w, r, user)
	case r.Method == http.MethodPost && len(parts) == 5 && parts[3] == "quota" && parts[4] == "refund":
		s.handleAdminOpsQuotaRefund(w, r, user)
	case r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "audit":
		s.handleAdminOpsAudit(w, r)
	case r.Method == http.MethodGet && len(parts) == 5 && parts[3] == "risk" && parts[4] == "reviews":
		s.handleAdminOpsRiskReviews(w, r)
	case r.Method == http.MethodPatch && len(parts) == 6 && parts[3] == "risk" && parts[4] == "reviews":
		s.handleAdminOpsUpdateRiskReview(w, r, user, parts[5])
	case r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "risk":
		s.handleAdminOpsRisk(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	writeJSON(w, http.StatusOK, map[string]any{
		"readiness": map[string]any{
			"status": readinessStatus,
			"checks": readinessChecks,
		},
		"llm": llmStatus,
	})
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
	var body struct {
		UserID string `json:"user_id"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	userID := strings.TrimSpace(body.UserID)
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
		return
	}
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
	var body struct {
		UserID        string  `json:"user_id"`
		RequestRefund int     `json:"request_refund"`
		TokenRefund   int     `json:"token_refund"`
		CostRefundUSD float64 `json:"cost_refund_usd"`
		Reason        string  `json:"reason"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	userID := strings.TrimSpace(body.UserID)
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
		return
	}
	if body.RequestRefund < 0 || body.TokenRefund < 0 || body.CostRefundUSD < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refund values must be positive"})
		return
	}
	if body.RequestRefund == 0 && body.TokenRefund == 0 && body.CostRefundUSD == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one refund value is required"})
		return
	}
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request, user User, parts []string) {
	if !s.requireAdmin(w, r) {
		return
	}
	store, ok := s.adminUserStore()
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "admin user store is not configured"})
		return
	}
	switch {
	case r.Method == http.MethodGet && len(parts) == 3:
		s.handleAdminListUsers(w, r, store)
	case r.Method == http.MethodGet && len(parts) == 4:
		s.handleAdminGetUser(w, r, store, parts[3])
	case r.Method == http.MethodPatch && len(parts) == 4:
		s.handleAdminUpdateUser(w, r, user, store, parts[3])
	case r.Method == http.MethodPost && len(parts) == 5:
		s.handleAdminUserAction(w, r, user, store, parts[3], parts[4])
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
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
		Status *string `json:"status"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Status == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status is required"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	case "disable":
		status = SkillStatusDisabled
		event = "skill_disable"
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
	case SkillStatusDraft, SkillStatusPublished, SkillStatusUnpublished, SkillStatusDisabled, SkillStatusArchived:
		return true
	default:
		return false
	}
}

func (s *Server) handleCreateAttachment(w http.ResponseWriter, r *http.Request, user User) {
	maxBytes := s.runtime.MaxAssetBytes()
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	items, err := s.runtime.ExtractMemoryFromAsset(r.Context(), user.ID, kind, assetID, body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	_, _ = w.Write(data)
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request, user User) {
	var body struct {
		SessionID      string              `json:"session_id"`
		Content        string              `json:"content"`
		Type           string              `json:"type"`
		AttachmentIDs  []string            `json:"attachment_ids"`
		AttachmentURLs []ChatAttachmentURL `json:"attachment_urls"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
			req := ChatRequest{UserID: user.ID, SessionID: sessionID, Content: msg.Content, AttachmentIDs: msg.AttachmentIDs, AttachmentURLs: msg.AttachmentURLs}
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	session, err := s.runtime.CreateSession(r.Context(), user.ID, body.WorkingDir)
	if err != nil {
		s.logf("create_session user=%s error=%v", user.ID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "session_create", user, map[string]any{"session_id": session.ID})
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request, user User) {
	sessions, err := s.runtime.ListSessions(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request, user User, sessionID string) {
	session, err := s.runtime.GetSession(r.Context(), user.ID, sessionID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusOK, session)
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		Type string `json:"type"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	updated, err := s.runtime.ApplyMemoryFeedback(r.Context(), user.ID, itemID, body.Type)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		Action string `json:"action"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	updated, err := s.runtime.ResolveMemoryConflict(r.Context(), user.ID, itemID, body.Action)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	actions, err := s.runtime.PlanMemoryMaintenance(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "memory_run_maintenance", user, map[string]any{"count": len(actions)})
	s.recordGovernanceEvent("memory_run_maintenance")
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

func (s *Server) handleApplyMemoryMaintenance(w http.ResponseWriter, r *http.Request, user User, actionID string) {
	action, err := s.runtime.ApplyMemoryMaintenance(r.Context(), user.ID, actionID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "memory_apply_maintenance", user, map[string]any{"action_id": action.ID, "type": action.Type})
	s.recordGovernanceEvent("memory_apply_maintenance_" + strings.TrimSpace(action.Type))
	writeJSON(w, http.StatusOK, action)
}

func (s *Server) handleDismissMemoryMaintenance(w http.ResponseWriter, r *http.Request, user User, actionID string) {
	action, err := s.runtime.DismissMemoryMaintenance(r.Context(), user.ID, actionID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	var body struct {
		Content        string              `json:"content"`
		AttachmentIDs  []string            `json:"attachment_ids"`
		AttachmentURLs []ChatAttachmentURL `json:"attachment_urls"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	req := ChatRequest{UserID: user.ID, SessionID: sessionID, Content: body.Content, AttachmentIDs: body.AttachmentIDs, AttachmentURLs: body.AttachmentURLs}
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
		s.logger.Printf(format, args...)
	}
}

func (s *Server) logEvent(event string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["event"] = event
	logJSON(s.logger, fields)
}

func (s *Server) recordGovernanceEvent(event string) {
	if s == nil || s.metrics == nil {
		return
	}
	event = strings.ToLower(strings.TrimSpace(event))
	event = strings.NewReplacer(" ", "_", "-", "_", "/", "_").Replace(event)
	s.metrics.IncGovernanceEvent(event)
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
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	return nil
}

func readOptionalJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(normalizeAPIResponse(w, status, value))
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

func strconvQuote(value string) string {
	value = strings.ReplaceAll(filepath.Base(value), `"`, "")
	return `"` + value + `"`
}
