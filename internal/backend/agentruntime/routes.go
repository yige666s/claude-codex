package agentruntime

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	chicors "github.com/go-chi/cors"
)

type routeContextKey string

const userContextKey routeContextKey = "user"

func (s *Server) buildRouter() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(s.requestLifecycleMiddleware)
	router.Use(middleware.Recoverer)
	router.Use(s.requestTimeoutMiddleware)
	router.Use(s.rejectDisallowedOriginMiddleware)
	router.Use(s.corsMiddleware)
	router.Use(s.optionsMiddleware)
	router.Use(s.shutdownMiddleware)

	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", s.handleReadyz)
	router.Get("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		s.metrics.WritePrometheus(w)
	})
	router.Get("/", s.handleApp)
	router.Get("/app", s.handleApp)

	router.With(s.publicRiskMiddleware(RiskOperationAuthRegister)).Post("/v1/auth/register", s.handleAuthRegister)
	router.With(s.publicRiskMiddleware(RiskOperationAuthLogin)).Post("/v1/auth/login", s.handleAuthLogin)
	router.Get("/v1/auth/verify-email", s.handleAuthVerifyEmail)
	router.Post("/v1/auth/verify-email", s.handleAuthVerifyEmail)
	router.Post("/v1/auth/password-reset/request", s.handleAuthPasswordResetRequest)
	router.Post("/v1/auth/password-reset/confirm", s.handleAuthPasswordResetConfirm)
	router.With(s.publicRiskMiddleware(RiskOperationAuthRefresh)).Post("/v1/auth/refresh", s.handleAuthRefresh)

	router.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Use(s.csrfMiddleware)
		r.Use(s.globalRateLimitMiddleware)
		r.Use(s.riskMiddleware)
		s.mountCoreRoutes(r)
	})

	router.MethodNotAllowed(s.notFoundHandler())
	router.NotFound(s.notFoundHandler())
	var handler http.Handler = router
	if s.instrumentHTTP != nil {
		handler = s.instrumentHTTP(handler)
	}
	return handler
}

func (s *Server) mountCoreRoutes(r chi.Router) {
	s.mountAdminRoutes(r)

	r.Get("/v1/auth/me", s.withUser(s.handleAuthMe))
	r.Post("/v1/auth/logout", s.withUser(s.handleAuthLogout))
	r.Delete("/v1/account", s.withUser(s.handleAccountDelete))
	r.Get("/v1/data/export", s.withUser(s.handleDataExport))

	r.Get("/v1/personalization", s.withUser(s.handleGetPersonalization))
	r.Patch("/v1/personalization", s.withUser(s.handleUpdatePersonalization))
	r.Post("/v1/personalization/reset", s.withUser(s.handleResetPersonalization))
	r.Post("/v1/personalization/browser-memory", s.withUser(s.handleCreateBrowserMemory))

	r.Get("/v1/memory", s.withUser(s.handleListMemory))
	r.Delete("/v1/memory", s.withUser(s.handleDeleteAllMemory))
	r.Get("/v1/memory/settings", s.withUser(s.handleGetMemorySettings))
	r.Patch("/v1/memory/settings", s.withUser(s.handleUpdateMemorySettings))
	r.Get("/v1/memory/maintenance", s.withUser(s.handleListMemoryMaintenance))
	r.Post("/v1/memory/maintenance/run", s.withUser(s.handleRunMemoryMaintenance))
	r.Post("/v1/memory/maintenance/{actionID}/apply", s.withUserParam("actionID", s.handleApplyMemoryMaintenance))
	r.Post("/v1/memory/maintenance/{actionID}/dismiss", s.withUserParam("actionID", s.handleDismissMemoryMaintenance))
	r.Post("/v1/memory/score", s.withUser(s.handleScoreMemory))
	r.Post("/v1/memory/rebuild", s.withUser(s.handleRebuildMemory))
	r.Patch("/v1/memory/{itemID}", s.withUserParam("itemID", s.handleUpdateMemoryItem))
	r.Post("/v1/memory/{itemID}/feedback", s.withUserParam("itemID", s.handleMemoryFeedback))
	r.Post("/v1/memory/{itemID}/resolve", s.withUserParam("itemID", s.handleResolveMemory))
	r.Delete("/v1/memory/{itemID}", s.withUserParam("itemID", s.handleDeleteMemoryItem))

	r.Get("/v1/sessions", s.withUser(s.handleListSessions))
	r.Post("/v1/sessions", s.withUser(s.handleCreateSession))
	r.Get("/v1/sessions/summary", s.withUser(s.handleListSessionSummaries))
	r.Get("/v1/sessions/{sessionID}", s.withUserParam("sessionID", s.handleGetSession))
	r.Delete("/v1/sessions/{sessionID}", s.withUserParam("sessionID", s.handleDeleteSession))
	r.Post("/v1/sessions/{sessionID}/messages", s.withUserParam("sessionID", s.handleMessage))
	r.Post("/v1/sessions/{sessionID}/cancel", s.withUserParam("sessionID", s.handleCancel))
	r.Delete("/v1/sessions/{sessionID}/memory", s.withUserParam("sessionID", s.handleDeleteSessionMemory))
	r.Get("/v1/sessions/{sessionID}/ws", s.withUserParam("sessionID", s.handleWebSocket))
	r.Get("/v1/sessions/{sessionID}/live/ws", s.withUserParam("sessionID", s.handleLiveWebSocket))

	r.Get("/v1/jobs", s.withUser(s.handleListJobs))
	r.Post("/v1/jobs", s.withUser(s.handleCreateJob))
	r.Get("/v1/jobs/{jobID}", s.withUserParam("jobID", s.handleGetJob))
	r.Get("/v1/jobs/{jobID}/events", s.withUserParam("jobID", s.handleJobEvents))
	r.Post("/v1/jobs/{jobID}/cancel", s.withUserParam("jobID", s.handleCancelJob))

	r.Get("/v1/attachments", s.withUser(s.handleListAttachments))
	r.Post("/v1/attachments", s.withUser(s.handleCreateAttachment))
	r.Post("/v1/attachments/presign", s.withUser(s.handleCreateAttachmentPresign))
	r.Get("/v1/attachments/{attachmentID}", s.withUserParam("attachmentID", s.handleDownloadAttachment))
	r.Delete("/v1/attachments/{attachmentID}", s.withUserParam("attachmentID", s.handleDeleteAttachment))
	r.Post("/v1/attachments/{attachmentID}/confirm", s.withUserParam("attachmentID", s.handleConfirmAttachmentUpload))
	r.Post("/v1/attachments/{attachmentID}/memory/extract", s.withUserAssetParam(AssetKindAttachment, "attachmentID"))

	r.Get("/v1/artifacts", s.withUser(s.handleListArtifacts))
	r.Get("/v1/artifacts/{artifactID}", s.withUserParam("artifactID", s.handleDownloadArtifact))
	r.Delete("/v1/artifacts/{artifactID}", s.withUserParam("artifactID", s.handleDeleteArtifact))
	r.Get("/v1/artifacts/{artifactID}/preview", s.withUserParam("artifactID", s.handlePreviewArtifact))
	r.Post("/v1/artifacts/{artifactID}/memory/extract", s.withUserAssetParam(AssetKindArtifact, "artifactID"))

	r.Get("/v1/search/messages", s.withUser(s.handleSearchMessages))
	r.Get("/v1/skills", s.handleListSkills)
	r.Get("/v1/llm/status", s.handleLLMStatus)
}

func (s *Server) mountAdminRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.adminMiddleware)
		r.Get("/v1/admin/users", s.withAdminUserStore(s.handleAdminListUsers))
		r.Get("/v1/admin/users/{userID}", s.withAdminUserStoreParam("userID", s.handleAdminGetUser))
		r.Patch("/v1/admin/users/{userID}", s.withUserAdminStoreParam("userID", s.handleAdminUpdateUser))
		r.Post("/v1/admin/users/{userID}/{action}", s.withUserAdminStoreTwoParams("userID", "action", s.handleAdminUserAction))

		r.Group(func(r chi.Router) {
			r.Use(s.runtimeRequiredMiddleware)
			r.Get("/v1/admin/ops/sessions", s.handleAdminOpsListSessions)
			r.Get("/v1/admin/ops/sessions/{sessionID}", s.withParam("sessionID", s.handleAdminOpsGetSession))
			r.Get("/v1/admin/ops/jobs", s.handleAdminOpsListJobs)
			r.Get("/v1/admin/ops/jobs/{jobID}", s.withParam("jobID", s.handleAdminOpsGetJob))
			r.Get("/v1/admin/ops/jobs/{jobID}/events", s.withParam("jobID", s.handleAdminOpsListJobEvents))
			r.Post("/v1/admin/ops/jobs/{jobID}/cancel", s.withUserParam("jobID", s.handleAdminOpsCancelJob))
			r.Get("/v1/admin/ops/assets", s.handleAdminOpsListAssets)
			r.Get("/v1/admin/ops/health", s.handleAdminOpsHealth)
			r.Get("/v1/admin/ops/llm-usage", s.handleAdminOpsLLMUsage)
			r.Get("/v1/admin/ops/llm-config", s.handleAdminOpsLLMConfig)
			r.Patch("/v1/admin/ops/llm-config", s.withUser(s.handleAdminOpsUpdateLLMConfig))
			r.Get("/v1/admin/ops/quota", s.handleAdminOpsQuota)
			r.Post("/v1/admin/ops/quota/reset", s.withUser(s.handleAdminOpsQuotaReset))
			r.Post("/v1/admin/ops/quota/refund", s.withUser(s.handleAdminOpsQuotaRefund))
			r.Get("/v1/admin/ops/audit", s.handleAdminOpsAudit)
			r.Get("/v1/admin/ops/risk", s.handleAdminOpsRisk)
			r.Get("/v1/admin/ops/risk/reviews", s.handleAdminOpsRiskReviews)
			r.Patch("/v1/admin/ops/risk/reviews/{reviewID}", s.withUserParam("reviewID", s.handleAdminOpsUpdateRiskReview))

			r.Group(func(r chi.Router) {
				r.Use(s.evaluationRequiredMiddleware)
				r.Post("/v1/admin/ops/eval/runs", s.withUser(s.handleAdminOpsCreateEvaluationRun))
				r.Get("/v1/admin/ops/eval/runs", s.handleAdminOpsListEvaluationRuns)
				r.Get("/v1/admin/ops/eval/runs/{runID}", s.withParam("runID", s.handleAdminOpsGetEvaluationRun))
				r.Get("/v1/admin/ops/eval/results", s.handleAdminOpsListEvaluationResults)
				r.Get("/v1/admin/ops/eval/reviews", s.handleAdminOpsListEvaluationReviews)
				r.Patch("/v1/admin/ops/eval/reviews/{reviewID}", s.withUserParam("reviewID", s.handleAdminOpsUpdateEvaluationReview))
				r.Get("/v1/admin/ops/eval/summary", s.handleAdminOpsEvaluationSummary)
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(s.skillRegistryRequiredMiddleware)
			r.Get("/v1/admin/skills", s.handleAdminListSkills)
			r.Post("/v1/admin/skills", s.withUser(s.handleAdminImportSkill))
			r.Patch("/v1/admin/skills/{name}", s.withUserParam("name", s.handleAdminUpdateSkill))
			r.Get("/v1/admin/skills/{name}/versions", s.withParam("name", s.handleAdminListSkillVersions))
			r.Get("/v1/admin/skills/{name}/releases", s.withParam("name", s.handleAdminListSkillReleases))
			r.Post("/v1/admin/skills/{name}/review", s.withParam("name", s.handleAdminReviewSkill))
			r.Post("/v1/admin/skills/{name}/submit-review", s.withUserParam("name", s.handleAdminSubmitSkillReview))
			r.Post("/v1/admin/skills/{name}/rollback", s.withUserParam("name", s.handleAdminRollbackSkill))
			r.Get("/v1/admin/skills/{name}/executions", s.withParam("name", s.handleAdminListSkillExecutions))
			r.Get("/v1/admin/skills/{name}/analytics", s.withParam("name", s.handleAdminSkillAnalytics))
			r.Get("/v1/admin/skills/{name}/versions/diff", s.withParam("name", s.handleAdminSkillVersionDiff))
			r.Post("/v1/admin/skills/{name}/{action}", s.withUserTwoParams("name", "action", s.handleAdminSetSkillStatus))
		})
	})
}

func (s *Server) requestLifecycleMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		reqID := requestIDFromContext(r.Context())
		if reqID == "" {
			reqID = r.Header.Get(middleware.RequestIDHeader)
		}
		logState := &requestLogState{}
		rec := &statusRecorder{ResponseWriter: w}
		rec.Header().Set("X-Request-ID", reqID)
		r = r.WithContext(withRequestID(r.Context(), reqID))
		r = r.WithContext(context.WithValue(r.Context(), requestLogContextKey, logState))
		defer func() {
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			duration := time.Since(started)
			if s.metrics != nil {
				s.metrics.RecordRequest(r.Method, r.URL.Path, status, duration)
			}
			route := r.URL.Path
			if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
				if pattern := routeContext.RoutePattern(); strings.TrimSpace(pattern) != "" {
					route = pattern
				}
			}
			logFields(s.logger, map[string]any{
				"event":       "request",
				"request_id":  reqID,
				"user_id":     logState.userID,
				"method":      r.Method,
				"route":       route,
				"status":      status,
				"bytes":       rec.bytes,
				"duration_ms": duration.Milliseconds(),
			})
		}()
		next.ServeHTTP(rec, r)
	})
}

func (s *Server) requestTimeoutMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timeout := s.security.RequestTimeout
		if timeout <= 0 || isUpgradeRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		middleware.Timeout(timeout)(next).ServeHTTP(w, r)
	})
}

func isUpgradeRequest(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") ||
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func (s *Server) rejectDisallowedOriginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.corsOriginAllowed(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin is not allowed"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chicors.Handler(s.corsOptions())(next).ServeHTTP(w, r)
	})
}

func (s *Server) corsOptions() chicors.Options {
	methods := s.security.CORSAllowedMethods
	if len(methods) == 0 {
		methods = []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"}
	}
	headers := s.security.CORSAllowedHeaders
	if len(headers) == 0 {
		headers = []string{"Authorization", "Content-Type", "X-User-ID", "X-Admin-Token", s.security.csrfHeaderName()}
	}
	return chicors.Options{
		AllowedMethods:     methods,
		AllowedHeaders:     headers,
		ExposedHeaders:     []string{"X-Request-ID"},
		AllowOriginFunc:    func(r *http.Request, _ string) bool { return s.corsOriginAllowed(r) },
		AllowCredentials:   s.security.CORSAllowCredentials,
		MaxAge:             300,
		OptionsPassthrough: true,
	}
}

func (s *Server) corsOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	return origin == "" || sameHostOrigin(r) || originAllowed(origin, s.security.CORSAllowedOrigins)
}

func (s *Server) optionsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) shutdownMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		if s.isShuttingDown() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server is shutting down"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) publicRiskMiddleware(operation string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if operation != "" && !s.allowPublicOperation(w, r, operation) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.authenticate(w, r)
		if !ok {
			return
		}
		setRequestLogUserID(r.Context(), user.ID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, user)))
	})
}

func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		csrfProtection(s.security)(next).ServeHTTP(w, r)
	})
}

func (s *Server) globalRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user identity is required"})
			return
		}
		if s.limiter.Allow(user.ID) {
			next.ServeHTTP(w, r)
			return
		}
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
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limited"})
	})
}

func (s *Server) riskMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")
		operation := classifyRiskOperation(r.Method, path, parts)
		if operation == "" {
			next.ServeHTTP(w, r)
			return
		}
		user, ok := userFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user identity is required"})
			return
		}
		if !s.allowUserOperation(w, r, user, operation) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) adminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAdmin(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) runtimeRequiredMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.runtime == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime is not configured"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) skillRegistryRequiredMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.skillRegistry == nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "skill registry is not configured"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) evaluationRequiredMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.evaluation == nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "evaluation store is not configured"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) notFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.authenticate(w, r)
		if !ok {
			return
		}
		setRequestLogUserID(r.Context(), user.ID)
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, user))
		s.csrfMiddleware(s.globalRateLimitMiddleware(s.riskMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		})))).ServeHTTP(w, r)
	}
}

func userFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey).(User)
	return user, ok && strings.TrimSpace(user.ID) != ""
}

func (s *Server) withUser(handler func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user identity is required"})
			return
		}
		handler(w, r, user)
	}
}

func (s *Server) withUserParam(param string, handler func(http.ResponseWriter, *http.Request, User, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user identity is required"})
			return
		}
		handler(w, r, user, chi.URLParam(r, param))
	}
}

func (s *Server) withUserTwoParams(firstParam, secondParam string, handler func(http.ResponseWriter, *http.Request, User, string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user identity is required"})
			return
		}
		handler(w, r, user, chi.URLParam(r, firstParam), chi.URLParam(r, secondParam))
	}
}

func (s *Server) withParam(param string, handler func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, chi.URLParam(r, param))
	}
}

func (s *Server) withAdminUserStore(handler func(http.ResponseWriter, *http.Request, AdminUserStore)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := s.adminUserStoreResponse(w)
		if !ok {
			return
		}
		handler(w, r, store)
	}
}

func (s *Server) withAdminUserStoreParam(param string, handler func(http.ResponseWriter, *http.Request, AdminUserStore, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := s.adminUserStoreResponse(w)
		if !ok {
			return
		}
		handler(w, r, store, chi.URLParam(r, param))
	}
}

func (s *Server) withUserAdminStoreParam(param string, handler func(http.ResponseWriter, *http.Request, User, AdminUserStore, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user identity is required"})
			return
		}
		store, ok := s.adminUserStoreResponse(w)
		if !ok {
			return
		}
		handler(w, r, user, store, chi.URLParam(r, param))
	}
}

func (s *Server) withUserAdminStoreTwoParams(firstParam, secondParam string, handler func(http.ResponseWriter, *http.Request, User, AdminUserStore, string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user identity is required"})
			return
		}
		store, ok := s.adminUserStoreResponse(w)
		if !ok {
			return
		}
		handler(w, r, user, store, chi.URLParam(r, firstParam), chi.URLParam(r, secondParam))
	}
}

func (s *Server) adminUserStoreResponse(w http.ResponseWriter) (AdminUserStore, bool) {
	store, ok := s.adminUserStore()
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "admin user store is not configured"})
		return nil, false
	}
	return store, true
}

func (s *Server) withUserAssetParam(kind, param string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user identity is required"})
			return
		}
		s.handleExtractAssetMemory(w, r, user, kind, chi.URLParam(r, param))
	}
}
