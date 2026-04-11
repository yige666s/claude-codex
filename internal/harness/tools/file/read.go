package file

import (
	"context"
	"encoding/json"
	"os"
	"sync"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type ReadTool struct {
	rootDir string
}

type readInput struct {
	Path string `json:"path"`
}

type ReadListener func(path string, content string)

var (
	readListenersMu    sync.RWMutex
	readListeners      = map[int]ReadListener{}
	nextReadListenerID int
)

func NewReadTool(rootDir string) *ReadTool {
	return &ReadTool{rootDir: rootDir}
}

func RegisterReadListener(listener ReadListener) func() {
	if listener == nil {
		return func() {}
	}

	readListenersMu.Lock()
	id := nextReadListenerID
	nextReadListenerID++
	readListeners[id] = listener
	readListenersMu.Unlock()

	return func() {
		readListenersMu.Lock()
		delete(readListeners, id)
		readListenersMu.Unlock()
	}
}

func ResetReadListenersForTest() {
	readListenersMu.Lock()
	readListeners = map[int]ReadListener{}
	nextReadListenerID = 0
	readListenersMu.Unlock()
}

func notifyReadListeners(path string, content string) {
	readListenersMu.RLock()
	listeners := make([]ReadListener, 0, len(readListeners))
	for _, listener := range readListeners {
		listeners = append(listeners, listener)
	}
	readListenersMu.RUnlock()

	for _, listener := range listeners {
		listener(path, content)
	}
}

func (t *ReadTool) Name() string {
	return "file_read"
}

func (t *ReadTool) Description() string {
	return "Read a UTF-8 text file from the project root."
}

func (t *ReadTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}

func (t *ReadTool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *ReadTool) IsConcurrencySafe() bool {
	return true // reads are safe to run concurrently
}

func (t *ReadTool) Execute(_ context.Context, input json.RawMessage) (toolkit.Result, error) {
	var payload readInput
	if err := json.Unmarshal(input, &payload); err != nil {
		return toolkit.Result{}, err
	}

	path, err := toolkit.ResolvePath(t.rootDir, payload.Path)
	if err != nil {
		return toolkit.Result{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return toolkit.Result{}, err
	}

	notifyReadListeners(path, string(data))

	return toolkit.Result{Output: string(data)}, nil
}
