package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type ReadTool struct {
	rootDir string
}

type readInput struct {
	FilePath string `json:"file_path"`
	Path     string `json:"path,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Pages    string `json:"pages,omitempty"`
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
	return ReadToolName
}

func (t *ReadTool) Description() string {
	return "Read a UTF-8 text file from the local filesystem."
}

func (t *ReadTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string","description":"The absolute path to the file to read"},"offset":{"type":"number","description":"The line number to start reading from; line numbers are 1-indexed"},"limit":{"type":"number","description":"The number of lines to read"},"pages":{"type":"string","description":"PDF page range to read, such as 1-5"}},"required":["file_path"]}`)
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

	path, err := toolkit.ResolvePath(t.rootDir, payload.filePath())
	if err != nil {
		return toolkit.Result{}, err
	}
	if info, err := os.Stat(path); err != nil {
		return toolkit.Result{}, err
	} else if info.IsDir() {
		return toolkit.Result{}, fmt.Errorf("%s is a directory", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return toolkit.Result{}, err
	}

	content := string(data)
	notifyReadListeners(path, content)

	return toolkit.Result{Output: addLineNumbers(readLineRange(content, payload.Offset, payload.Limit), firstLine(payload.Offset))}, nil
}

func (in readInput) filePath() string {
	if in.FilePath != "" {
		return in.FilePath
	}
	return in.Path
}

func readLineRange(content string, offset, limit int) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	start := firstLine(offset) - 1
	if start >= len(lines) {
		return ""
	}
	lines = lines[start:]
	if limit > 0 && limit < len(lines) {
		lines = lines[:limit]
	}
	return strings.Join(lines, "\n")
}

func firstLine(offset int) int {
	if offset < 1 {
		return 1
	}
	return offset
}

func addLineNumbers(content string, startLine int) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lineNumber := fmt.Sprintf("%6d", startLine+i)
		if len(lineNumber) >= 6 {
			lines[i] = lineNumber + "\u2192" + line
		} else {
			lines[i] = lineNumber + "\u2192" + line
		}
	}
	return strings.Join(lines, "\n")
}
