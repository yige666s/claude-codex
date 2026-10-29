package sandboxstaging

import (
	"claude-codex/internal/backend/agentruntime/sandboxnode"
	"errors"
	"net/http"
)

// NewRecoveredDevelopmentHandler is an explicitly opt-in test assembly. It
// cannot be used by cmd/sandboxnode without a separate deliberate code change.
// The staging requests hold the process fence through authentication, input
// publication and journal commit. Execute/collect hold it in the worker.
func NewRecoveredDevelopmentHandler(key []byte, worker *DevelopmentWorker, shared ...*sandboxnode.TaskSigner) (http.Handler, error) {
	if worker == nil || worker.Recovery == nil || !worker.Recovery.alive() || worker.ready() != nil {
		return nil, errors.New("recovered development worker required")
	}
	var signer *sandboxnode.TaskSigner
	if len(shared) > 0 {
		if len(shared) != 1 || shared[0] == nil {
			return nil, errors.New("invalid shared task signer")
		}
		signer = shared[0]
	} else {
		var err error
		signer, err = sandboxnode.NewTaskSigner(key, nil)
		if err != nil {
			return nil, err
		}
	}
	stage, err := NewStagingHandler(key, worker.Service, signer)
	if err != nil {
		return nil, err
	}
	execution, err := NewDevelopmentExecutionHandler(key, worker, signer)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	for _, route := range []string{"/v1/tasks/prepare", "/v1/tasks/cancel", "/v1/tasks/dispose"} {
		mux.Handle(route, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			release, err := worker.Recovery.hold()
			if err != nil {
				http.Error(w, "Node admission closed", http.StatusServiceUnavailable)
				return
			}
			defer release()
			stage.ServeHTTP(w, r)
		}))
	}
	mux.Handle("/v1/dev/execute", execution)
	mux.Handle("/v1/dev/collect", execution)
	return mux, nil
}
