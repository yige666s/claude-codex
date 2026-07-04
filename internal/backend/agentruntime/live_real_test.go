package agentruntime

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-codex/internal/harness/skills"
)

func TestRealGeminiLiveNativeFunctionCallingRoutesSkill(t *testing.T) {
	if os.Getenv("RUN_REAL_VERTEX_LIVE") != "1" {
		t.Skip("set RUN_REAL_VERTEX_LIVE=1 to call the real Gemini Live API")
	}
	projectID := firstNonEmpty(
		os.Getenv("AGENT_API_LIVE_VERTEX_PROJECT_ID"),
		os.Getenv("VERTEX_PROJECT_ID"),
		os.Getenv("GOOGLE_CLOUD_PROJECT"),
		mustGcloudProject(t),
	)
	if projectID == "" {
		t.Skip("set VERTEX_PROJECT_ID, GOOGLE_CLOUD_PROJECT, or gcloud core.project")
	}

	root := t.TempDir()
	store := NewFileSessionStore(root)
	catalog := fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "smoke",
		DisplayName:   "smoke skill",
		Description:   "Run this skill when the user asks to use the smoke skill for a live function calling test.",
		UserInvocable: true,
		GetPrompt: func(args string, _ *skills.SkillContext) ([]skills.ContentBlock, error) {
			return []skills.ContentBlock{{Type: "text", Text: "real live smoke skill result: " + args}}, nil
		},
	}}}
	runtime := NewRuntime(RuntimeConfig{
		DefaultWorkingDir: root,
		TurnTimeout:       time.Minute,
		Live: LiveConfig{
			Enabled:                    true,
			Provider:                   "vertex",
			Model:                      firstNonEmpty(os.Getenv("AGENT_API_LIVE_MODEL"), defaultLiveModel),
			VertexProjectID:            projectID,
			VertexLocation:             firstNonEmpty(os.Getenv("AGENT_API_LIVE_VERTEX_LOCATION"), os.Getenv("VERTEX_LOCATION"), "us-central1"),
			VertexAPIVersion:           firstNonEmpty(os.Getenv("AGENT_API_LIVE_VERTEX_API_VERSION"), "v1beta1"),
			InputAudioMIMEType:         "audio/pcm;rate=16000",
			InputTranscriptionEnabled:  true,
			OutputTranscriptionEnabled: true,
			SessionTimeout:             45 * time.Second,
		},
	}, store, nil, catalog, func(Scope) Runner { return echoRunner{} })
	session, err := runtime.CreateSession(context.Background(), "real-live-user", root)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	stream := newLiveRealInputStream()
	sink := newLiveRealEventSink(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Live(ctx, LiveRequest{UserID: "real-live-user", SessionID: session.ID}, stream, sink)
	}()

	sink.waitFor(t, func(event Event) bool { return event.Type == "live_setup_complete" }, "real Live setup", 20*time.Second)
	stream.send(LiveClientEvent{Type: "text", Content: "Please use the smoke skill with args hello from real Gemini Live."})
	start := sink.waitFor(t, func(event Event) bool {
		return event.Type == "tool_call_start" && strings.HasPrefix(event.Tool, "/smoke ")
	}, "real Gemini native run_skill call", 30*time.Second)
	t.Logf("real native function call routed command: %s", start.Tool)
	result := sink.waitFor(t, func(event Event) bool {
		return event.Type == "tool_call_result" && strings.Contains(event.Summary, "real live smoke skill result")
	}, "real Live skill result", 10*time.Second)
	t.Logf("real skill result: %s", result.Summary)

	stream.close()
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !strings.Contains(err.Error(), "EOF") && !strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "use of closed network connection") {
			t.Fatalf("Live returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Live did not stop after the real smoke test")
	}
}

type liveRealInputStream struct {
	ch chan LiveClientEvent
}

func newLiveRealInputStream() *liveRealInputStream {
	return &liveRealInputStream{ch: make(chan LiveClientEvent, 64)}
}

func (s *liveRealInputStream) ReceiveLiveClientEvent(ctx context.Context) (LiveClientEvent, error) {
	select {
	case event, ok := <-s.ch:
		if !ok {
			return LiveClientEvent{}, io.EOF
		}
		return event, nil
	case <-ctx.Done():
		return LiveClientEvent{}, ctx.Err()
	}
}

func (s *liveRealInputStream) send(event LiveClientEvent) {
	s.ch <- event
}

func (s *liveRealInputStream) close() {
	close(s.ch)
}

type liveRealEventSink struct {
	t      *testing.T
	mu     sync.Mutex
	events []Event
	ch     chan Event
}

func newLiveRealEventSink(t *testing.T) *liveRealEventSink {
	return &liveRealEventSink{t: t, ch: make(chan Event, 256)}
}

func (s *liveRealEventSink) Send(_ context.Context, event Event) error {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
	s.t.Logf("real live event type=%s role=%s content=%q error=%q", event.Type, event.Role, event.Content, event.Error)
	s.ch <- event
	return nil
}

func (s *liveRealEventSink) waitFor(t *testing.T, match func(Event) bool, label string, timeout time.Duration) Event {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case event := <-s.ch:
			if event.Type == "error" {
				t.Fatalf("received real Live error while waiting for %s: %s", label, event.Error)
			}
			if match(event) {
				return event
			}
		case <-timer.C:
			s.mu.Lock()
			events := append([]Event(nil), s.events...)
			s.mu.Unlock()
			t.Fatalf("timed out waiting for %s; saw %d events: %#v", label, len(events), events)
		}
	}
}

func mustGcloudProject(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gcloud", "config", "get-value", "project").Output()
	if err != nil {
		return ""
	}
	project := strings.TrimSpace(string(out))
	if project == "(unset)" {
		return ""
	}
	return project
}
