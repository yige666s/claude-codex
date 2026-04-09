package analytics

// AnalyticsMetadata is a marker type for verified non-sensitive metadata
// Usage: value as AnalyticsMetadata (type assertion in calling code)
type AnalyticsMetadata interface{}

// EventMetadata contains metadata for analytics events
type EventMetadata map[string]interface{}

// Event represents a queued analytics event
type Event struct {
	Name     string
	Metadata EventMetadata
	Async    bool
}

// Sink is the interface for analytics backends
type Sink interface {
	LogEvent(eventName string, metadata EventMetadata)
	LogEventAsync(eventName string, metadata EventMetadata) error
}

// SamplingConfig contains event sampling configuration
type SamplingConfig struct {
	EventName  string
	SampleRate float64
}

// Config contains analytics configuration
type Config struct {
	Disabled         bool
	DatadogEnabled   bool
	FirstPartyEnabled bool
	SamplingRates    map[string]float64
}

// Constants for analytics
const (
	// MaxQueueSize is the maximum number of events to queue before sink attachment
	MaxQueueSize = 1000

	// DefaultSampleRate is the default sampling rate (100%)
	DefaultSampleRate = 1.0
)

// Environment check functions
var (
	// IsTestEnvironment checks if running in test mode
	IsTestEnvironment = func() bool { return false }

	// IsTelemetryDisabled checks if telemetry is disabled
	IsTelemetryDisabled = func() bool { return false }

	// IsBedrockEnabled checks if using Bedrock
	IsBedrockEnabled = func() bool { return false }

	// IsVertexEnabled checks if using Vertex
	IsVertexEnabled = func() bool { return false }

	// IsFoundryEnabled checks if using Foundry
	IsFoundryEnabled = func() bool { return false }
)
