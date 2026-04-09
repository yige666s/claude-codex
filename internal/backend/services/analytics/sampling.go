package analytics

import (
	"math/rand"
)

// shouldSample determines if an event should be sampled based on rate
func shouldSample(rate float64) bool {
	if rate >= 1.0 {
		return true
	}
	if rate <= 0.0 {
		return false
	}
	return rand.Float64() < rate
}

// SamplingResult represents the result of sampling decision
type SamplingResult struct {
	ShouldLog  bool
	SampleRate float64
}

// CheckSampling checks if an event should be sampled
func CheckSampling(eventName string, sampleRates map[string]float64) SamplingResult {
	rate := DefaultSampleRate
	if r, ok := sampleRates[eventName]; ok {
		rate = r
	}

	return SamplingResult{
		ShouldLog:  shouldSample(rate),
		SampleRate: rate,
	}
}

// UpdateSamplingRates updates the sampling rates configuration
func (s *Service) UpdateSamplingRates(rates map[string]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampleRates = rates
}

// GetSamplingRates returns the current sampling rates
func (s *Service) GetSamplingRates() map[string]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]float64, len(s.sampleRates))
	for k, v := range s.sampleRates {
		result[k] = v
	}
	return result
}
