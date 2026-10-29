package sandboxstaging

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"claude-codex/internal/backend/agentruntime/sandboxnode"
	"claude-codex/internal/backend/agentruntime/sandboxoci"
	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

const MaxExecuteBody = 96 << 10
const MaxCollectResponse = 16 << 20

type ExecuteRequest struct {
	StageControl
	ExecutionID string `json:"execution_id"`
	Script      []byte `json:"script"`
}
type ExecuteReply struct {
	Result sandboxoci.Result `json:"result"`
}
type CollectReply struct {
	Files []sandboxtransfer.OutputFile `json:"files"`
}

// NewDevelopmentExecutionHandler exists only for explicit offline/dev wiring.
// cmd/sandboxnode does NOT register it; no production execute route exists.
func NewDevelopmentExecutionHandler(key []byte, worker *DevelopmentWorker, shared ...*sandboxnode.TaskSigner) (http.Handler, error) {
	if worker == nil || worker.Recovery == nil || !worker.Recovery.alive() || worker.ready() != nil {
		return nil, errors.New("development execution requires a recovered worker")
	}
	signer, err := sandboxnode.NewTaskSigner(key, nil)
	if err != nil {
		return nil, err
	}
	if len(shared) > 0 {
		if len(shared) != 1 || shared[0] == nil {
			return nil, errors.New("invalid shared task signer")
		}
		signer = shared[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/dev/execute", func(w http.ResponseWriter, r *http.Request) {
		var req ExecuteRequest
		if err := strictStageJSON(r.Body, &req); err != nil {
			http.Error(w, "invalid command request", http.StatusBadRequest)
			return
		}
		result, err := worker.Execute(r.Context(), req.StageControl, req.ExecutionID, req.Script)
		if err != nil {
			http.Error(w, "command unavailable", http.StatusConflict)
			return
		}
		reply, marshalErr := json.Marshal(ExecuteReply{Result: result})
		if marshalErr != nil || len(reply) > MaxCollectResponse {
			http.Error(w, "command response exceeded bound", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(reply)
	})
	mux.HandleFunc("POST /v1/dev/collect", func(w http.ResponseWriter, r *http.Request) {
		var control StageControl
		if err := strictStageJSON(r.Body, &control); err != nil {
			http.Error(w, "invalid collect request", http.StatusBadRequest)
			return
		}
		files, err := worker.Collect(r.Context(), control)
		if err != nil {
			http.Error(w, "collection unavailable", http.StatusConflict)
			return
		}
		reply, err := json.Marshal(CollectReply{Files: files})
		if err != nil || len(reply) > MaxCollectResponse {
			http.Error(w, "collection exceeded bound", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(reply)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost || (r.URL.Path != "/v1/dev/execute" && r.URL.Path != "/v1/dev/collect") || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "invalid content type", http.StatusUnsupportedMediaType)
			return
		}
		max := int64(16 << 10)
		if r.URL.Path == "/v1/dev/execute" {
			max = MaxExecuteBody
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, max+1))
		r.Body.Close()
		if err != nil || int64(len(data)) > max {
			http.Error(w, "command body exceeds bound", http.StatusRequestEntityTooLarge)
			return
		}
		if signer.Verify(r, data) != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		mux.ServeHTTP(w, r)
	}), nil
}
