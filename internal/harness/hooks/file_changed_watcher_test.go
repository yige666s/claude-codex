package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileChangedWatcherPollsChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	hook := &captureFileChangedHook{}
	registry := NewRegistry()
	if err := registry.Register(hook); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	watcher := NewFileChangedWatcher(NewExecutor(registry), dir, []string{".env"})
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte("A=2\n"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}
	watcher.Poll(context.Background())

	if len(hook.events) != 1 {
		t.Fatalf("expected one FileChanged event, got %+v", hook.events)
	}
	if hook.events[0]["path"] != path || hook.events[0]["event"] != "change" {
		t.Fatalf("unexpected event metadata: %+v", hook.events[0])
	}
}

type captureFileChangedHook struct {
	events []map[string]any
}

func (h *captureFileChangedHook) Name() string           { return "capture" }
func (h *captureFileChangedHook) Event() HookEvent       { return EventFileChanged }
func (h *captureFileChangedHook) IsAsync() bool          { return false }
func (h *captureFileChangedHook) Timeout() time.Duration { return time.Second }
func (h *captureFileChangedHook) Execute(_ context.Context, input *HookInput) (*HookResult, error) {
	h.events = append(h.events, input.Metadata)
	return &HookResult{Continue: true}, nil
}
