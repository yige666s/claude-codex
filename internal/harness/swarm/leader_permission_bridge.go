package swarm

import (
	"context"
	"sync"
)

// LeaderPermissionHandler resolves in-process teammate permission requests
// through the leader runtime when that bridge is available.
type LeaderPermissionHandler func(ctx context.Context, req *SwarmPermissionRequest) (*PermissionResolution, error)

var (
	leaderPermissionHandlerMu sync.RWMutex
	leaderPermissionHandler   LeaderPermissionHandler
)

// RegisterLeaderPermissionHandler makes the leader-side permission bridge
// available to non-UI swarm code.
func RegisterLeaderPermissionHandler(handler LeaderPermissionHandler) {
	leaderPermissionHandlerMu.Lock()
	defer leaderPermissionHandlerMu.Unlock()
	leaderPermissionHandler = handler
}

// GetLeaderPermissionHandler returns the registered leader permission bridge.
func GetLeaderPermissionHandler() LeaderPermissionHandler {
	leaderPermissionHandlerMu.RLock()
	defer leaderPermissionHandlerMu.RUnlock()
	return leaderPermissionHandler
}

// UnregisterLeaderPermissionHandler clears the leader permission bridge.
func UnregisterLeaderPermissionHandler() {
	RegisterLeaderPermissionHandler(nil)
}
