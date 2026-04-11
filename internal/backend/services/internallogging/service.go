package internallogging

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"sync"

	"claude-codex/internal/backend/services/analytics"
	"claude-codex/internal/harness/tool"
)

const (
	permissionContextEventName     = "tengu_internal_record_permission_context"
	kubernetesNamespacePath        = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	namespaceNotFound              = "namespace not found"
	containerIDPath                = "/proc/self/mountinfo"
	containerIDNotFound            = "container ID not found"
	containerIDNotFoundInMountinfo = "container ID not found in mountinfo"
)

var containerIDPattern = regexp.MustCompile(`(?:/docker/containers/|/sandboxes/)([0-9a-f]{64})`)

// PermissionContextMoment identifies when permission context was captured.
type PermissionContextMoment string

const (
	PermissionContextInitialization PermissionContextMoment = "initialization"
	PermissionContextSummary        PermissionContextMoment = "summary"
)

// EventLogger is the analytics surface needed by this service.
type EventLogger interface {
	LogEventAsync(eventName string, metadata analytics.EventMetadata) error
}

// Service ports the TypeScript internalLogging service used for ant-only
// permission-context analytics.
type Service struct {
	logger   EventLogger
	readFile func(string) ([]byte, error)
	userType func() string

	namespaceOnce sync.Once
	namespace     *string

	containerIDOnce sync.Once
	containerID     *string
}

// NewService creates a service using process environment and the local filesystem.
func NewService(logger EventLogger) *Service {
	return &Service{
		logger:   logger,
		readFile: os.ReadFile,
		userType: func() string { return os.Getenv("USER_TYPE") },
	}
}

func (s *Service) isAntUser() bool {
	if s == nil || s.userType == nil {
		return false
	}
	return s.userType() == "ant"
}

// GetKubernetesNamespace returns the ant namespace when available.
// Non-ant users receive nil, mirroring the TypeScript behavior.
func (s *Service) GetKubernetesNamespace() *string {
	if s == nil {
		return nil
	}
	s.namespaceOnce.Do(func() {
		s.namespace = s.lookupKubernetesNamespace()
	})
	return s.namespace
}

func (s *Service) lookupKubernetesNamespace() *string {
	if !s.isAntUser() {
		return nil
	}
	content, err := s.readFile(kubernetesNamespacePath)
	if err != nil {
		return stringPtr(namespaceNotFound)
	}
	return stringPtr(strings.TrimSpace(string(content)))
}

// GetContainerID returns the running container id when available.
// Non-ant users receive nil, mirroring the TypeScript behavior.
func (s *Service) GetContainerID() *string {
	if s == nil {
		return nil
	}
	s.containerIDOnce.Do(func() {
		s.containerID = s.lookupContainerID()
	})
	return s.containerID
}

func (s *Service) lookupContainerID() *string {
	if !s.isAntUser() {
		return nil
	}
	mountInfo, err := s.readFile(containerIDPath)
	if err != nil {
		return stringPtr(containerIDNotFound)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(mountInfo)), "\n") {
		matches := containerIDPattern.FindStringSubmatch(line)
		if len(matches) == 2 {
			return stringPtr(matches[1])
		}
	}

	return stringPtr(containerIDNotFoundInMountinfo)
}

// LogPermissionContextForAnts records permission context analytics for ant users.
func (s *Service) LogPermissionContextForAnts(permissionContext *tool.ToolPermissionContext, moment PermissionContextMoment) {
	if s == nil || s.logger == nil || !s.isAntUser() {
		return
	}

	metadata := analytics.EventMetadata{
		"moment":                string(moment),
		"namespace":             derefString(s.GetKubernetesNamespace()),
		"toolPermissionContext": stringifyPermissionContext(permissionContext),
		"containerId":           derefString(s.GetContainerID()),
	}

	_ = s.logger.LogEventAsync(permissionContextEventName, metadata)
}

func stringifyPermissionContext(permissionContext *tool.ToolPermissionContext) string {
	if permissionContext == nil {
		return "null"
	}

	payload, err := json.Marshal(permissionContext.Clone())
	if err != nil {
		return "null"
	}
	return string(payload)
}

func derefString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func stringPtr(v string) *string {
	return &v
}
