package analytics

import (
	"sync"
)

// Service manages analytics event logging
type Service struct {
	sink       Sink
	queue      []Event
	mu         sync.RWMutex
	attached   bool
	config     *Config
	sampleRates map[string]float64
}

// NewService creates a new analytics service
func NewService(config *Config) *Service {
	if config == nil {
		config = &Config{
			SamplingRates: make(map[string]float64),
		}
	}

	return &Service{
		queue:       make([]Event, 0, MaxQueueSize),
		config:      config,
		sampleRates: config.SamplingRates,
	}
}

// AttachSink attaches the analytics sink
// Events queued before attachment are drained asynchronously
func (s *Service) AttachSink(sink Sink) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.attached {
		return
	}

	s.sink = sink
	s.attached = true

	// Drain queue asynchronously
	if len(s.queue) > 0 {
		queuedEvents := make([]Event, len(s.queue))
		copy(queuedEvents, s.queue)
		s.queue = s.queue[:0]

		go func() {
			for _, event := range queuedEvents {
				if event.Async {
					_ = sink.LogEventAsync(event.Name, event.Metadata)
				} else {
					sink.LogEvent(event.Name, event.Metadata)
				}
			}
		}()
	}
}

// LogEvent logs an event synchronously
func (s *Service) LogEvent(eventName string, metadata EventMetadata) {
	if s.config.Disabled || IsAnalyticsDisabled() {
		return
	}

	// Check sampling
	sampleRate := s.getSampleRate(eventName)
	if !shouldSample(sampleRate) {
		return
	}

	// Add sample rate to metadata if sampled
	if sampleRate < 1.0 {
		metadata = s.addSampleRate(metadata, sampleRate)
	}

	s.mu.RLock()
	attached := s.attached
	sink := s.sink
	s.mu.RUnlock()

	if !attached {
		s.queueEvent(eventName, metadata, false)
		return
	}

	sink.LogEvent(eventName, metadata)
}

// LogEventAsync logs an event asynchronously
func (s *Service) LogEventAsync(eventName string, metadata EventMetadata) error {
	if s.config.Disabled || IsAnalyticsDisabled() {
		return nil
	}

	// Check sampling
	sampleRate := s.getSampleRate(eventName)
	if !shouldSample(sampleRate) {
		return nil
	}

	// Add sample rate to metadata if sampled
	if sampleRate < 1.0 {
		metadata = s.addSampleRate(metadata, sampleRate)
	}

	s.mu.RLock()
	attached := s.attached
	sink := s.sink
	s.mu.RUnlock()

	if !attached {
		s.queueEvent(eventName, metadata, true)
		return nil
	}

	return sink.LogEventAsync(eventName, metadata)
}

// queueEvent adds an event to the queue
func (s *Service) queueEvent(eventName string, metadata EventMetadata, async bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.queue) >= MaxQueueSize {
		// Drop oldest event
		s.queue = s.queue[1:]
	}

	s.queue = append(s.queue, Event{
		Name:     eventName,
		Metadata: metadata,
		Async:    async,
	})
}

// getSampleRate returns the sample rate for an event
func (s *Service) getSampleRate(eventName string) float64 {
	if rate, ok := s.sampleRates[eventName]; ok {
		return rate
	}
	return DefaultSampleRate
}

// addSampleRate adds sample rate to metadata
func (s *Service) addSampleRate(metadata EventMetadata, rate float64) EventMetadata {
	result := make(EventMetadata, len(metadata)+1)
	for k, v := range metadata {
		result[k] = v
	}
	result["sample_rate"] = rate
	return result
}

// Reset resets the service for testing
func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sink = nil
	s.attached = false
	s.queue = s.queue[:0]
}

// IsAnalyticsDisabled checks if analytics should be disabled
func IsAnalyticsDisabled() bool {
	return IsTestEnvironment() ||
		IsBedrockEnabled() ||
		IsVertexEnabled() ||
		IsFoundryEnabled() ||
		IsTelemetryDisabled()
}

// StripProtoFields removes _PROTO_ prefixed keys from metadata
// These keys are for PII-tagged proto columns and should not go to general-access backends
func StripProtoFields(metadata EventMetadata) EventMetadata {
	hasProtoFields := false
	for key := range metadata {
		if len(key) >= 7 && key[:7] == "_PROTO_" {
			hasProtoFields = true
			break
		}
	}

	if !hasProtoFields {
		return metadata
	}

	result := make(EventMetadata, len(metadata))
	for key, value := range metadata {
		if len(key) < 7 || key[:7] != "_PROTO_" {
			result[key] = value
		}
	}
	return result
}

// SanitizeToolName sanitizes tool names for analytics
// MCP tool names are redacted to avoid PII exposure
func SanitizeToolName(toolName string) string {
	if len(toolName) >= 5 && toolName[:5] == "mcp__" {
		return "mcp_tool"
	}
	return toolName
}
