package mcp

import "sync"

var sdkHandlers sync.Map // map[string]SendMCPMessageCallback

func RegisterSDKControlHandler(serverName string, handler SendMCPMessageCallback) {
	if serverName == "" || handler == nil {
		return
	}
	sdkHandlers.Store(serverName, handler)
}

func UnregisterSDKControlHandler(serverName string) {
	sdkHandlers.Delete(serverName)
}

func getSDKControlHandler(serverName string) (SendMCPMessageCallback, bool) {
	value, ok := sdkHandlers.Load(serverName)
	if !ok {
		return nil, false
	}
	handler, ok := value.(SendMCPMessageCallback)
	return handler, ok
}
