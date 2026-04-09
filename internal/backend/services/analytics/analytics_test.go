package analytics

import (
	"testing"
)

// Mock sink for testing
type mockSink struct {
	events      []Event
	asyncEvents []Event
}

func (m *mockSink) LogEvent(eventName string, metadata EventMetadata) {
	m.events = append(m.events, Event{
		Name:     eventName,
		Metadata: metadata,
		Async:    false,
	})
}

func (m *mockSink) LogEventAsync(eventName string, metadata EventMetadata) error {
	m.asyncEvents = append(m.asyncEvents, Event{
		Name:     eventName,
		Metadata: metadata,
		Async:    true,
	})
	return nil
}

func TestNewService(t *testing.T) {
	service := NewService(nil)
	if service == nil {
		t.Fatal("expected non-nil service")
	}
	if service.config == nil {
		t.Error("expected non-nil config")
	}
}

func TestLogEvent(t *testing.T) {
	service := NewService(&Config{})
	sink := &mockSink{}
	service.AttachSink(sink)

	metadata := EventMetadata{
		"key": "value",
	}

	service.LogEvent("test_event", metadata)

	if len(sink.events) != 1 {
		t.Errorf("expected 1 event, got %d", len(sink.events))
	}
	if sink.events[0].Name != "test_event" {
		t.Errorf("expected event name 'test_event', got %s", sink.events[0].Name)
	}
}

func TestLogEventAsync(t *testing.T) {
	service := NewService(&Config{})
	sink := &mockSink{}
	service.AttachSink(sink)

	metadata := EventMetadata{
		"key": "value",
	}

	err := service.LogEventAsync("test_event_async", metadata)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(sink.asyncEvents) != 1 {
		t.Errorf("expected 1 async event, got %d", len(sink.asyncEvents))
	}
}

func TestQueueing(t *testing.T) {
	service := NewService(&Config{})

	// Log events before sink is attached
	service.LogEvent("queued_event_1", EventMetadata{"key": 1})
	service.LogEvent("queued_event_2", EventMetadata{"key": 2})

	// Attach sink
	sink := &mockSink{}
	service.AttachSink(sink)

	// Give goroutine time to drain queue
	// In real code, this would be handled by proper synchronization
	// For testing, we check the queue was cleared
	service.mu.RLock()
	queueLen := len(service.queue)
	service.mu.RUnlock()

	if queueLen != 0 {
		t.Errorf("expected queue to be drained, got %d events", queueLen)
	}
}

func TestSampling(t *testing.T) {
	tests := []struct {
		name       string
		rate       float64
		shouldPass bool
	}{
		{
			name:       "always sample",
			rate:       1.0,
			shouldPass: true,
		},
		{
			name:       "never sample",
			rate:       0.0,
			shouldPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSample(tt.rate)
			if result != tt.shouldPass {
				t.Errorf("expected %v, got %v", tt.shouldPass, result)
			}
		})
	}
}

func TestStripProtoFields(t *testing.T) {
	tests := []struct {
		name     string
		input    EventMetadata
		expected int
	}{
		{
			name: "no proto fields",
			input: EventMetadata{
				"key1": "value1",
				"key2": "value2",
			},
			expected: 2,
		},
		{
			name: "with proto fields",
			input: EventMetadata{
				"key1":        "value1",
				"_PROTO_key2": "value2",
				"key3":        "value3",
			},
			expected: 2,
		},
		{
			name: "all proto fields",
			input: EventMetadata{
				"_PROTO_key1": "value1",
				"_PROTO_key2": "value2",
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripProtoFields(tt.input)
			if len(result) != tt.expected {
				t.Errorf("expected %d fields, got %d", tt.expected, len(result))
			}

			// Verify no _PROTO_ fields remain
			for key := range result {
				if len(key) >= 7 && key[:7] == "_PROTO_" {
					t.Errorf("found _PROTO_ field in result: %s", key)
				}
			}
		})
	}
}

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "built-in tool",
			input:    "Bash",
			expected: "Bash",
		},
		{
			name:     "mcp tool",
			input:    "mcp__server__tool",
			expected: "mcp_tool",
		},
		{
			name:     "another built-in",
			input:    "Read",
			expected: "Read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeToolName(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestIsAnalyticsDisabled(t *testing.T) {
	// Save original functions
	origTest := IsTestEnvironment
	origBedrock := IsBedrockEnabled
	origVertex := IsVertexEnabled
	origFoundry := IsFoundryEnabled
	origTelemetry := IsTelemetryDisabled

	// Restore after test
	defer func() {
		IsTestEnvironment = origTest
		IsBedrockEnabled = origBedrock
		IsVertexEnabled = origVertex
		IsFoundryEnabled = origFoundry
		IsTelemetryDisabled = origTelemetry
	}()

	tests := []struct {
		name     string
		setup    func()
		expected bool
	}{
		{
			name: "all enabled",
			setup: func() {
				IsTestEnvironment = func() bool { return false }
				IsBedrockEnabled = func() bool { return false }
				IsVertexEnabled = func() bool { return false }
				IsFoundryEnabled = func() bool { return false }
				IsTelemetryDisabled = func() bool { return false }
			},
			expected: false,
		},
		{
			name: "test environment",
			setup: func() {
				IsTestEnvironment = func() bool { return true }
				IsBedrockEnabled = func() bool { return false }
				IsVertexEnabled = func() bool { return false }
				IsFoundryEnabled = func() bool { return false }
				IsTelemetryDisabled = func() bool { return false }
			},
			expected: true,
		},
		{
			name: "bedrock enabled",
			setup: func() {
				IsTestEnvironment = func() bool { return false }
				IsBedrockEnabled = func() bool { return true }
				IsVertexEnabled = func() bool { return false }
				IsFoundryEnabled = func() bool { return false }
				IsTelemetryDisabled = func() bool { return false }
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			result := IsAnalyticsDisabled()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestUpdateSamplingRates(t *testing.T) {
	service := NewService(&Config{})

	newRates := map[string]float64{
		"event1": 0.5,
		"event2": 0.8,
	}

	service.UpdateSamplingRates(newRates)

	rates := service.GetSamplingRates()
	if len(rates) != 2 {
		t.Errorf("expected 2 rates, got %d", len(rates))
	}
	if rates["event1"] != 0.5 {
		t.Errorf("expected rate 0.5 for event1, got %f", rates["event1"])
	}
}

func TestReset(t *testing.T) {
	service := NewService(&Config{})
	sink := &mockSink{}
	service.AttachSink(sink)

	service.LogEvent("test_event", EventMetadata{})

	service.Reset()

	service.mu.RLock()
	attached := service.attached
	service.mu.RUnlock()

	if attached {
		t.Error("expected service to be detached after reset")
	}
}

func TestCheckSampling(t *testing.T) {
	rates := map[string]float64{
		"sampled_event": 0.5,
	}

	result := CheckSampling("sampled_event", rates)
	if result.SampleRate != 0.5 {
		t.Errorf("expected sample rate 0.5, got %f", result.SampleRate)
	}

	result = CheckSampling("unknown_event", rates)
	if result.SampleRate != DefaultSampleRate {
		t.Errorf("expected default sample rate, got %f", result.SampleRate)
	}
}
