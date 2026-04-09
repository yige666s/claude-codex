package analysis

import (
	"context"
	"testing"
	"time"
)

func TestNewPerformanceAnalyzer(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()
	if analyzer == nil {
		t.Fatal("Expected non-nil analyzer")
	}
	if !analyzer.IsEnabled() {
		t.Error("Expected analyzer to be enabled by default")
	}
}

func TestPerformanceAnalyzer_Enable_Disable(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	analyzer.Disable()
	if analyzer.IsEnabled() {
		t.Error("Expected analyzer to be disabled")
	}

	analyzer.Enable()
	if !analyzer.IsEnabled() {
		t.Error("Expected analyzer to be enabled")
	}
}

func TestPerformanceAnalyzer_RecordMetric(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	analyzer.RecordMetric("test_metric", 100.0, map[string]string{"label": "value"})

	series, err := analyzer.GetMetric("test_metric")
	if err != nil {
		t.Fatalf("Failed to get metric: %v", err)
	}

	if len(series.DataPoints) != 1 {
		t.Errorf("Expected 1 data point, got %d", len(series.DataPoints))
	}

	if series.DataPoints[0].Value != 100.0 {
		t.Errorf("Expected value 100.0, got %f", series.DataPoints[0].Value)
	}
}

func TestPerformanceAnalyzer_RecordMetric_Disabled(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()
	analyzer.Disable()

	analyzer.RecordMetric("test_metric", 100.0, nil)

	_, err := analyzer.GetMetric("test_metric")
	if err == nil {
		t.Error("Expected error for metric that shouldn't be recorded")
	}
}

func TestPerformanceAnalyzer_RecordDuration(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	duration := 150 * time.Millisecond
	analyzer.RecordDuration("duration_metric", duration, nil)

	series, err := analyzer.GetMetric("duration_metric")
	if err != nil {
		t.Fatalf("Failed to get metric: %v", err)
	}

	if series.DataPoints[0].Value != 150.0 {
		t.Errorf("Expected value 150.0, got %f", series.DataPoints[0].Value)
	}
}

func TestPerformanceAnalyzer_StartTimer(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	stop := analyzer.StartTimer("timer_metric", nil)
	time.Sleep(50 * time.Millisecond)
	stop()

	series, err := analyzer.GetMetric("timer_metric")
	if err != nil {
		t.Fatalf("Failed to get metric: %v", err)
	}

	if len(series.DataPoints) != 1 {
		t.Errorf("Expected 1 data point, got %d", len(series.DataPoints))
	}

	// Should be at least 50ms
	if series.DataPoints[0].Value < 50.0 {
		t.Errorf("Expected value >= 50.0, got %f", series.DataPoints[0].Value)
	}
}

func TestPerformanceAnalyzer_GetMetric_NotFound(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	_, err := analyzer.GetMetric("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent metric")
	}
}

func TestPerformanceAnalyzer_ListMetrics(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	analyzer.RecordMetric("metric1", 100.0, nil)
	analyzer.RecordMetric("metric2", 200.0, nil)

	metrics := analyzer.ListMetrics()
	if len(metrics) != 2 {
		t.Errorf("Expected 2 metrics, got %d", len(metrics))
	}
}

func TestPerformanceAnalyzer_GenerateReport(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	// Record some metrics
	start := time.Now()
	analyzer.RecordMetric("test_metric", 100.0, nil)
	analyzer.RecordMetric("test_metric", 200.0, nil)
	analyzer.RecordMetric("test_metric", 150.0, nil)
	end := time.Now()

	report, err := analyzer.GenerateReport(context.Background(), start.Add(-1*time.Second), end.Add(1*time.Second))
	if err != nil {
		t.Fatalf("Failed to generate report: %v", err)
	}

	stats, exists := report.Metrics["test_metric"]
	if !exists {
		t.Fatal("Expected test_metric in report")
	}

	if stats.Count != 3 {
		t.Errorf("Expected count 3, got %d", stats.Count)
	}

	if stats.Min != 100.0 {
		t.Errorf("Expected min 100.0, got %f", stats.Min)
	}

	if stats.Max != 200.0 {
		t.Errorf("Expected max 200.0, got %f", stats.Max)
	}

	if stats.Mean != 150.0 {
		t.Errorf("Expected mean 150.0, got %f", stats.Mean)
	}

	if stats.Median != 150.0 {
		t.Errorf("Expected median 150.0, got %f", stats.Median)
	}
}

func TestPerformanceAnalyzer_GenerateReport_EmptyRange(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	analyzer.RecordMetric("test_metric", 100.0, nil)

	// Query a time range before the metric was recorded
	past := time.Now().Add(-1 * time.Hour)
	report, err := analyzer.GenerateReport(context.Background(), past.Add(-1*time.Minute), past)

	if err != nil {
		t.Fatalf("Failed to generate report: %v", err)
	}

	if len(report.Metrics) != 0 {
		t.Errorf("Expected 0 metrics in report, got %d", len(report.Metrics))
	}
}

func TestPerformanceAnalyzer_Clear(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	analyzer.RecordMetric("metric1", 100.0, nil)
	analyzer.RecordMetric("metric2", 200.0, nil)

	analyzer.Clear()

	metrics := analyzer.ListMetrics()
	if len(metrics) != 0 {
		t.Errorf("Expected 0 metrics after clear, got %d", len(metrics))
	}
}

func TestPerformanceAnalyzer_ClearMetric(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	analyzer.RecordMetric("metric1", 100.0, nil)
	analyzer.RecordMetric("metric2", 200.0, nil)

	err := analyzer.ClearMetric("metric1")
	if err != nil {
		t.Fatalf("Failed to clear metric: %v", err)
	}

	_, err = analyzer.GetMetric("metric1")
	if err == nil {
		t.Error("Expected error for cleared metric")
	}

	_, err = analyzer.GetMetric("metric2")
	if err != nil {
		t.Error("Expected metric2 to still exist")
	}
}

func TestPerformanceAnalyzer_ClearMetric_NotFound(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	err := analyzer.ClearMetric("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent metric")
	}
}

func TestPerformanceAnalyzer_MaxSize(t *testing.T) {
	analyzer := NewPerformanceAnalyzer()

	// Record more than max size
	for i := range 1100 {
		analyzer.RecordMetric("test_metric", float64(i), nil)
	}

	series, _ := analyzer.GetMetric("test_metric")
	if len(series.DataPoints) > 1000 {
		t.Errorf("Expected max 1000 data points, got %d", len(series.DataPoints))
	}
}

func TestCalculateStats(t *testing.T) {
	values := []float64{100, 200, 150, 300, 250}

	stats := calculateStats("test", "ms", values)

	if stats.Count != 5 {
		t.Errorf("Expected count 5, got %d", stats.Count)
	}

	if stats.Min != 100 {
		t.Errorf("Expected min 100, got %f", stats.Min)
	}

	if stats.Max != 300 {
		t.Errorf("Expected max 300, got %f", stats.Max)
	}

	if stats.Mean != 200 {
		t.Errorf("Expected mean 200, got %f", stats.Mean)
	}
}

func TestCalculateStats_Empty(t *testing.T) {
	stats := calculateStats("test", "ms", []float64{})

	if stats.Count != 0 {
		t.Errorf("Expected count 0, got %d", stats.Count)
	}
}

func TestPercentile(t *testing.T) {
	sorted := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	p50 := percentile(sorted, 0.5)
	if p50 != 5 {
		t.Errorf("Expected p50=5, got %f", p50)
	}

	p95 := percentile(sorted, 0.95)
	if p95 != 9 {
		t.Errorf("Expected p95=9, got %f", p95)
	}
}
