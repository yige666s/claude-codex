package agentruntime

import (
	"errors"
	"net/http"

	"claude-codex/internal/backend/sandboxpolicy"
)

type sandboxPolicyRevisionRequest struct {
	Revision       int64 `json:"revision"`
	ExpectedActive int64 `json:"expected_active"`
}

func (s *Server) sandboxPolicyAvailable(w http.ResponseWriter) bool {
	if s.sandboxPolicy == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "sandbox policy SQL store is not configured"})
		return false
	}
	return true
}

func (s *Server) handleAdminSandboxPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.sandboxPolicyAvailable(w) {
		return
	}
	rev, err := s.sandboxPolicy.Active(r.Context())
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": rev})
}

func (s *Server) handleAdminSandboxPolicyRevisions(w http.ResponseWriter, r *http.Request) {
	if !s.sandboxPolicyAvailable(w) {
		return
	}
	revisions, err := s.sandboxPolicy.List(r.Context(), 20)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": revisions})
}

func (s *Server) handleAdminSandboxPolicyValidate(w http.ResponseWriter, r *http.Request) {
	var doc sandboxpolicy.Document
	if err := readJSON(r, &doc); err != nil {
		writeJSONError(w, err)
		return
	}
	if err := doc.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "native_enabled": false})
}

func (s *Server) handleAdminSandboxPolicyPreview(w http.ResponseWriter, r *http.Request) {
	if !s.sandboxPolicyAvailable(w) {
		return
	}
	var body struct {
		Origin string `json:"origin"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	rev, err := s.sandboxPolicy.Active(r.Context())
	if err != nil {
		writeJSONError(w, err)
		return
	}
	if body.Origin != "unknown" && body.Origin != "external" && body.Origin != "generated" && body.Origin != "repo_release" {
		body.Origin = "unknown"
	}
	profile, ok := rev.Document.ProfileFor(body.Origin)
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev.ID, "origin": body.Origin, "profile": profile, "routed": ok, "simulation_only": true})
}

func (s *Server) handleAdminSandboxPolicyDraft(w http.ResponseWriter, r *http.Request, actor User) {
	if !s.sandboxPolicyAvailable(w) {
		return
	}
	var doc sandboxpolicy.Document
	if err := readJSON(r, &doc); err != nil {
		writeJSONError(w, err)
		return
	}
	rev, err := s.sandboxPolicy.Draft(r.Context(), doc, actor.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "admin_sandbox_policy_draft", actor, map[string]any{"revision": rev.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"draft": rev})
}

func (s *Server) handleAdminSandboxPolicyPublish(w http.ResponseWriter, r *http.Request, actor User) {
	s.changeSandboxPolicy(w, r, actor, false)
}
func (s *Server) handleAdminSandboxPolicyRollback(w http.ResponseWriter, r *http.Request, actor User) {
	s.changeSandboxPolicy(w, r, actor, true)
}
func (s *Server) changeSandboxPolicy(w http.ResponseWriter, r *http.Request, actor User, rollback bool) {
	if !s.sandboxPolicyAvailable(w) {
		return
	}
	var body sandboxPolicyRevisionRequest
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	if body.Revision <= 0 || body.ExpectedActive <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "revision and expected_active are required"})
		return
	}
	var rev sandboxpolicy.Revision
	var err error
	if rollback {
		rev, err = s.sandboxPolicy.Rollback(r.Context(), body.Revision, body.ExpectedActive, actor.ID)
	} else {
		rev, err = s.sandboxPolicy.Publish(r.Context(), body.Revision, body.ExpectedActive, actor.ID)
	}
	if err != nil {
		if errors.Is(err, sandboxpolicy.ErrRevisionConflict) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "sandbox policy revision conflict"})
			return
		}
		writeJSONError(w, err)
		return
	}
	operation := "publish"
	if rollback {
		operation = "rollback"
	}
	s.auditEvent(r, "admin_sandbox_policy_"+operation, actor, map[string]any{"revision": rev.ID, "previous_revision": body.ExpectedActive})
	writeJSON(w, http.StatusOK, map[string]any{"active": rev})
}
