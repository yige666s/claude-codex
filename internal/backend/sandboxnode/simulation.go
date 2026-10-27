package sandboxnode

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// NewSimulationHandler exposes a no-op lifecycle contract only. It cannot run
// a command and must be enabled explicitly for WSL integration tests.
func NewSimulationHandler(key []byte, journal *FileJournal, revision string) (http.Handler, error) {
	if journal == nil || revision == "" {
		return nil, errors.New("simulation journal and policy revision required")
	}
	signer, err := NewTaskSigner(key, nil)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sim/prepare", func(w http.ResponseWriter, r *http.Request) {
		var spec PrepareSpec
		if err := decodeSimJSON(r, &spec); err != nil {
			http.Error(w, "invalid prepare request", http.StatusBadRequest)
			return
		}
		if spec.PolicyRevision != revision {
			http.Error(w, "policy revision mismatch", http.StatusConflict)
			return
		}
		lease, err := journal.Prepare(spec)
		if err != nil {
			http.Error(w, "prepare rejected", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, lease)
	})
	mux.HandleFunc("POST /v1/sim/exec", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			LeaseID string `json:"lease_id"`
			UserID  string `json:"user_id"`
		}
		if err := decodeSimJSON(r, &body); err != nil {
			http.Error(w, "invalid simulation request", http.StatusBadRequest)
			return
		}
		if _, err := journal.AppendEventForUser(body.LeaseID, body.UserID, "status", "simulated execution complete"); err != nil {
			http.Error(w, "lease unavailable", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"execution_id": body.LeaseID, "status": "simulated", "code_executed": false})
	})
	mux.HandleFunc("POST /v1/sim/collect", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			LeaseID string `json:"lease_id"`
			UserID  string `json:"user_id"`
		}
		if err := decodeSimJSON(r, &body); err != nil {
			http.Error(w, "invalid simulation request", http.StatusBadRequest)
			return
		}
		if _, err := journal.Snapshot(body.LeaseID, body.UserID); err != nil {
			http.Error(w, "lease unavailable", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"artifacts": []string{}, "simulation_only": true})
	})
	mux.HandleFunc("GET /v1/sim/events", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		cursor, err := strconv.ParseInt(q.Get("cursor"), 10, 64)
		if err != nil {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
		items, next, err := journal.Events(q.Get("lease_id"), q.Get("user_id"), cursor, 64)
		if err != nil {
			http.Error(w, "events unavailable", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": items, "next_cursor": next})
	})
	for _, op := range []string{"cancel", "dispose"} {
		operation := op
		mux.HandleFunc("POST /v1/sim/"+operation, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				LeaseID string `json:"lease_id"`
				UserID  string `json:"user_id"`
			}
			if err := decodeSimJSON(r, &body); err != nil {
				http.Error(w, "invalid lease request", http.StatusBadRequest)
				return
			}
			var lease LeaseSnapshot
			var opErr error
			if operation == "cancel" {
				lease, opErr = journal.Cancel(body.LeaseID, body.UserID)
			} else {
				lease, opErr = journal.Dispose(body.LeaseID, body.UserID)
			}
			if opErr != nil {
				http.Error(w, "lease not found", http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, lease)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/sim/") {
			http.NotFound(w, r)
			return
		}
		data, err := limitedBody(r)
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := signer.Verify(r, data); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	}), nil
}
func decodeSimJSON(r *http.Request, out any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("extra or invalid JSON")
	}
	return nil
}
