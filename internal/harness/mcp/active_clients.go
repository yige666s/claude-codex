package mcp

import "sync"

var activeClients sync.Map // map[string]*Client

func RegisterActiveClient(name string, client *Client) {
	if client == nil || name == "" {
		return
	}
	activeClients.Store(name, client)
}

func GetActiveClient(name string) (*Client, bool) {
	value, ok := activeClients.Load(name)
	if !ok {
		return nil, false
	}
	client, ok := value.(*Client)
	return client, ok
}

func ClearActiveClients() {
	activeClients.Range(func(key, _ any) bool {
		activeClients.Delete(key)
		return true
	})
}
