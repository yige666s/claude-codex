package internallogging

import (
	"errors"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/backend/services/analytics"
	"github.com/ding/claude-code/claude-go/internal/harness/tool"
)

type stubLogger struct {
	events []analytics.Event
}

func (s *stubLogger) LogEventAsync(eventName string, metadata analytics.EventMetadata) error {
	s.events = append(s.events, analytics.Event{Name: eventName, Metadata: metadata, Async: true})
	return nil
}

func newTestService(logger EventLogger, userType string, reader func(string) ([]byte, error)) *Service {
	return &Service{
		logger: logger,
		readFile: func(path string) ([]byte, error) {
			if reader == nil {
				return nil, errors.New("unexpected read")
			}
			return reader(path)
		},
		userType: func() string { return userType },
	}
}

func TestGetKubernetesNamespaceNonAntReturnsNil(t *testing.T) {
	readCalls := 0
	service := newTestService(nil, "human", func(path string) ([]byte, error) {
		readCalls++
		return []byte("ignored"), nil
	})

	if got := service.GetKubernetesNamespace(); got != nil {
		t.Fatalf("expected nil namespace, got %v", *got)
	}
	if readCalls != 0 {
		t.Fatalf("expected no reads, got %d", readCalls)
	}
}

func TestGetKubernetesNamespaceMemoizesResult(t *testing.T) {
	readCalls := 0
	service := newTestService(nil, "ant", func(path string) ([]byte, error) {
		readCalls++
		if path != kubernetesNamespacePath {
			t.Fatalf("unexpected path %s", path)
		}
		return []byte("ts\n"), nil
	})

	first := service.GetKubernetesNamespace()
	second := service.GetKubernetesNamespace()

	if first == nil || *first != "ts" {
		t.Fatalf("expected ts namespace, got %v", first)
	}
	if second == nil || *second != "ts" {
		t.Fatalf("expected memoized ts namespace, got %v", second)
	}
	if readCalls != 1 {
		t.Fatalf("expected 1 read, got %d", readCalls)
	}
}

func TestGetContainerIDParsesDockerMountinfo(t *testing.T) {
	const containerID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	service := newTestService(nil, "ant", func(path string) ([]byte, error) {
		if path == containerIDPath {
			return []byte("42 35 0:38 / / rw,relatime - overlay overlay rw,lowerdir=/var/lib/docker/containers/" + containerID + "/mounts/shm\n"), nil
		}
		return nil, errors.New("unexpected path")
	})

	got := service.GetContainerID()
	if got == nil || *got != containerID {
		t.Fatalf("expected parsed container id, got %v", got)
	}
}

func TestGetContainerIDHandlesMissingMountinfoMatch(t *testing.T) {
	service := newTestService(nil, "ant", func(path string) ([]byte, error) {
		return []byte("42 35 0:38 / / rw,relatime - overlay overlay rw\n"), nil
	})

	got := service.GetContainerID()
	if got == nil || *got != containerIDNotFoundInMountinfo {
		t.Fatalf("expected missing match sentinel, got %v", got)
	}
}

func TestLogPermissionContextForAnts(t *testing.T) {
	logger := &stubLogger{}
	service := newTestService(logger, "ant", func(path string) ([]byte, error) {
		switch path {
		case kubernetesNamespacePath:
			return []byte("default\n"), nil
		case containerIDPath:
			return []byte("42 35 0:38 / / rw,relatime - overlay overlay rw,lowerdir=/sandboxes/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"), nil
		default:
			return nil, errors.New("unexpected path")
		}
	})

	ctx := tool.NewToolPermissionContext()
	ctx.SetMode(tool.PermissionModeAuto)
	ctx.AddAlwaysAllowRule("user", tool.PermissionRule{Pattern: "Read(*)", Enabled: true})

	service.LogPermissionContextForAnts(ctx, PermissionContextSummary)

	if len(logger.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(logger.events))
	}
	event := logger.events[0]
	if event.Name != permissionContextEventName {
		t.Fatalf("expected event %s, got %s", permissionContextEventName, event.Name)
	}
	if got := event.Metadata["moment"]; got != string(PermissionContextSummary) {
		t.Fatalf("expected summary moment, got %#v", got)
	}
	if got := event.Metadata["namespace"]; got != "default" {
		t.Fatalf("expected default namespace, got %#v", got)
	}
	if got := event.Metadata["containerId"]; got != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected container id %#v", got)
	}
	serialized, ok := event.Metadata["toolPermissionContext"].(string)
	if !ok {
		t.Fatalf("expected serialized permission context, got %#v", event.Metadata["toolPermissionContext"])
	}
	if serialized == "null" || serialized == "" {
		t.Fatalf("expected non-empty permission context JSON, got %q", serialized)
	}
}

func TestLogPermissionContextForAntsSkipsNonAntUsers(t *testing.T) {
	logger := &stubLogger{}
	service := newTestService(logger, "human", nil)

	service.LogPermissionContextForAnts(tool.NewToolPermissionContext(), PermissionContextInitialization)

	if len(logger.events) != 0 {
		t.Fatalf("expected no events, got %d", len(logger.events))
	}
}
