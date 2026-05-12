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

func ListActiveClients() map[string]*Client {
	clients := map[string]*Client{}
	activeClients.Range(func(key, value any) bool {
		name, ok := key.(string)
		if !ok {
			return true
		}
		client, ok := value.(*Client)
		if ok && client != nil {
			clients[name] = client
		}
		return true
	})
	return clients
}

func ClearActiveClients() {
	activeClients.Range(func(key, _ any) bool {
		activeClients.Delete(key)
		return true
	})
}
