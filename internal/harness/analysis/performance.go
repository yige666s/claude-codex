package analysis

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PerformanceAnalyzer tracks and analyzes performance metrics.
type PerformanceAnalyzer struct {
	mu      sync.RWMutex
	metrics map[string]*MetricSeries
	enabled bool
}

// MetricSeries stores a time series of metric values.
type MetricSeries struct {
	Name       string
	Unit       string
	DataPoints []DataPoint
	MaxSize    int
}

// DataPoint represents a single metric measurement.
type DataPoint struct {
	Timestamp time.Time
	Value     float64
	Labels    map[string]string
}

// PerformanceReport contains aggregated performance metrics.
type PerformanceReport struct {
	StartTime time.Time
	EndTime   time.Time
	Metrics   map[string]*MetricStats
}

// MetricStats contains statistical analysis of a metric.
type MetricStats struct {
	Name   string
	Unit   string
	Count  int
	Min    float64
	Max    float64
	Mean   float64
	Median float64
	P95    float64
	P99    float64
}

// NewPerformanceAnalyzer creates a new performance analyzer.
func NewPerformanceAnalyzer() *PerformanceAnalyzer {
	return &PerformanceAnalyzer{
		metrics: make(map[string]*MetricSeries),
		enabled: true,
	}
}

// Enable enables performance tracking.
func (a *PerformanceAnalyzer) Enable() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enabled = true
}

// Disable disables performance tracking.
func (a *PerformanceAnalyzer) Disable() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enabled = false
}

// IsEnabled returns whether tracking is enabled.
func (a *PerformanceAnalyzer) IsEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.enabled
}

// RecordMetric records a metric value.
func (a *PerformanceAnalyzer) RecordMetric(name string, value float64, labels map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.enabled {
		return
	}

	series, exists := a.metrics[name]
	if !exists {
		series = &MetricSeries{
			Name:       name,
			Unit:       "",
			DataPoints: make([]DataPoint, 0),
			MaxSize:    1000,
		}
		a.metrics[name] = series
	}

	dataPoint := DataPoint{
		Timestamp: time.Now(),
		Value:     value,
		Labels:    labels,
	}

	series.DataPoints = append(series.DataPoints, dataPoint)

	// Trim if exceeds max size
	if len(series.DataPoints) > series.MaxSize {
		series.DataPoints = series.DataPoints[len(series.DataPoints)-series.MaxSize:]
	}
}

// RecordDuration records a duration metric in milliseconds.
func (a *PerformanceAnalyzer) RecordDuration(name string, duration time.Duration, labels map[string]string) {
	a.RecordMetric(name, float64(duration.Milliseconds()), labels)
}

// StartTimer returns a function that records the elapsed time when called.
func (a *PerformanceAnalyzer) StartTimer(name string, labels map[string]string) func() {
	start := time.Now()
	return func() {
		a.RecordDuration(name, time.Since(start), labels)
	}
}

// GetMetric returns a metric series by name.
func (a *PerformanceAnalyzer) GetMetric(name string) (*MetricSeries, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	series, exists := a.metrics[name]
	if !exists {
		return nil, fmt.Errorf("metric not found: %s", name)
	}

	return series, nil
}

// ListMetrics returns all metric names.
func (a *PerformanceAnalyzer) ListMetrics() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	names := make([]string, 0, len(a.metrics))
	for name := range a.metrics {
		names = append(names, name)
	}

	return names
}

// GenerateReport generates a performance report for the specified time range.
func (a *PerformanceAnalyzer) GenerateReport(ctx context.Context, start, end time.Time) (*PerformanceReport, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	report := &PerformanceReport{
		StartTime: start,
		EndTime:   end,
		Metrics:   make(map[string]*MetricStats),
	}

	for name, series := range a.metrics {
		// Filter data points within time range
		var values []float64
		for _, dp := range series.DataPoints {
			if dp.Timestamp.After(start) && dp.Timestamp.Before(end) {
				values = append(values, dp.Value)
			}
		}

		if len(values) == 0 {
			continue
		}

		stats := calculateStats(name, series.Unit, values)
		report.Metrics[name] = stats
	}

	return report, nil
}

// Clear clears all metrics.
func (a *PerformanceAnalyzer) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.metrics = make(map[string]*MetricSeries)
}

// ClearMetric clears a specific metric.
func (a *PerformanceAnalyzer) ClearMetric(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.metrics[name]; !exists {
		return fmt.Errorf("metric not found: %s", name)
	}

	delete(a.metrics, name)
	return nil
}

// calculateStats calculates statistical metrics from values.
func calculateStats(name, unit string, values []float64) *MetricStats {
	if len(values) == 0 {
		return &MetricStats{Name: name, Unit: unit}
	}

	// Sort values for percentile calculation
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sortFloat64(sorted)

	stats := &MetricStats{
		Name:  name,
		Unit:  unit,
		Count: len(values),
		Min:   sorted[0],
		Max:   sorted[len(sorted)-1],
	}

	// Calculate mean
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	stats.Mean = sum / float64(len(values))

	// Calculate median
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		stats.Median = (sorted[mid-1] + sorted[mid]) / 2
	} else {
		stats.Median = sorted[mid]
	}

	// Calculate percentiles
	stats.P95 = percentile(sorted, 0.95)
	stats.P99 = percentile(sorted, 0.99)

	return stats
}

// percentile calculates the percentile value from sorted data.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}

	index := int(float64(len(sorted)-1) * p)
	return sorted[index]
}

// sortFloat64 sorts a slice of float64 in ascending order.
func sortFloat64(data []float64) {
	// Simple bubble sort for small datasets
	n := len(data)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if data[j] > data[j+1] {
				data[j], data[j+1] = data[j+1], data[j]
			}
		}
	}
}
