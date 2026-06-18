package agentruntime

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type MetricsRegistry struct {
	mu                                      sync.Mutex
	registry                                *prometheus.Registry
	requestsTotal                           prometheus.Counter
	requestsByStatus                        *prometheus.CounterVec
	requestsByRoute                         *prometheus.CounterVec
	requestErrorsTotal                      prometheus.Counter
	requestLatencyMSTotal                   prometheus.Counter
	requestDurationSeconds                  *prometheus.HistogramVec
	rateLimitedTotal                        prometheus.Counter
	auditErrorsTotal                        prometheus.Counter
	governanceEvents                        *prometheus.CounterVec
	piiRedactions                           prometheus.Counter
	personalizationUpdates                  prometheus.Counter
	personalizationChanges                  prometheus.Counter
	personalizationEnabled                  *prometheus.CounterVec
	personalizationFields                   *prometheus.CounterVec
	browserMemoryTotal                      prometheus.Counter
	liveActiveSessionsMetric                prometheus.Gauge
	liveSessionsTotalMetric                 prometheus.Counter
	liveSucceededTotalMetric                prometheus.Counter
	liveFailedTotalMetric                   prometheus.Counter
	liveDisconnectedMetric                  prometheus.Counter
	liveAudioChunksMetric                   prometheus.Counter
	liveAudioBytesMetric                    prometheus.Counter
	liveErrorsMetric                        *prometheus.CounterVec
	loopAutomationEnabledMetric             prometheus.Gauge
	loopAutomationRunningMetric             prometheus.Gauge
	loopAutomationRunsMetric                prometheus.Counter
	loopAutomationScannedMetric             prometheus.Counter
	loopAutomationTriggeredMetric           *prometheus.CounterVec
	loopAutomationSkippedMetric             prometheus.Counter
	loopAutomationFailedMetric              prometheus.Counter
	loopAutomationDedupeConflictMetric      prometheus.Counter
	loopAutomationExpiredPrunedMetric       prometheus.Counter
	loopAutomationConsecutiveFailuresMetric prometheus.Gauge
	loopAutomationLastRunUnixMetric         prometheus.Gauge
	loopAutomationNextDueUnixMetric         prometheus.Gauge
	loopAutomationQuotaBlockedMetric        prometheus.Counter
	liveActiveSessions                      int64
	liveSessionsTotal                       int64
	liveSuccessfulSessions                  int64
	liveFailedSessions                      int64
	liveDisconnectedSessions                int64
	liveAudioChunksTotal                    int64
	liveAudioBytesTotal                     int64
	liveDurationTotalMS                     int64
	liveFirstTranscriptTotalMS              int64
	liveFirstTranscriptCount                int64
	liveFirstAudioTotalMS                   int64
	liveFirstAudioCount                     int64
	liveErrorsByCode                        map[string]int64
}

func NewMetricsRegistry() *MetricsRegistry {
	m := &MetricsRegistry{
		registry:         prometheus.NewRegistry(),
		liveErrorsByCode: make(map[string]int64),
	}
	m.requestsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_requests_total",
		Help: "Total HTTP requests.",
	})
	m.requestsByStatus = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentapi_requests_by_status_total",
		Help: "Total HTTP requests by status.",
	}, []string{"status"})
	m.requestsByRoute = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentapi_requests_by_route_total",
		Help: "Total HTTP requests by normalized route.",
	}, []string{"route"})
	m.requestErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_request_errors_total",
		Help: "Total HTTP requests with 4xx/5xx status.",
	})
	m.requestLatencyMSTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_request_latency_ms_total",
		Help: "Total HTTP request latency in milliseconds.",
	})
	m.requestDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentapi_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method", "status"})
	m.rateLimitedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_rate_limited_total",
		Help: "Total rate-limited requests.",
	})
	m.auditErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_audit_errors_total",
		Help: "Total audit log write failures.",
	})
	m.governanceEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentapi_governance_events_total",
		Help: "Total C-end governance events.",
	}, []string{"event"})
	m.piiRedactions = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_pii_redactions_total",
		Help: "Total PII redactions observed in user-facing governance operations.",
	})
	m.personalizationUpdates = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_personalization_updates_total",
		Help: "Total personalization setting save/reset operations.",
	})
	m.personalizationChanges = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_personalization_changes_total",
		Help: "Total personalization operations that changed persisted settings.",
	})
	m.personalizationEnabled = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentapi_personalization_enabled_total",
		Help: "Personalization operations by whether effective personalization is enabled.",
	}, []string{"enabled"})
	m.personalizationFields = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentapi_personalization_field_coverage_total",
		Help: "Personalization operations by field and whether that field is populated.",
	}, []string{"field", "present"})
	m.browserMemoryTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_personalization_browser_memory_total",
		Help: "Total browser memory submissions accepted.",
	})
	m.liveActiveSessionsMetric = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentapi_live_sessions_active",
		Help: "Current active Live websocket sessions.",
	})
	m.liveSessionsTotalMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_live_sessions_total",
		Help: "Total Live websocket sessions.",
	})
	m.liveSucceededTotalMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_live_sessions_succeeded_total",
		Help: "Total successful Live websocket sessions.",
	})
	m.liveFailedTotalMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_live_sessions_failed_total",
		Help: "Total failed Live websocket sessions.",
	})
	m.liveDisconnectedMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_live_sessions_disconnected_total",
		Help: "Total disconnected Live websocket sessions.",
	})
	m.liveAudioChunksMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_live_audio_chunks_total",
		Help: "Total Live audio chunks.",
	})
	m.liveAudioBytesMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_live_audio_bytes_total",
		Help: "Total Live audio bytes.",
	})
	m.liveErrorsMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentapi_live_errors_total",
		Help: "Total Live websocket errors by code.",
	}, []string{"code"})
	m.loopAutomationEnabledMetric = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentapi_loop_automation_enabled",
		Help: "Whether loop trigger automation is configured as enabled.",
	})
	m.loopAutomationRunningMetric = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentapi_loop_automation_running",
		Help: "Whether loop trigger automation worker is currently running.",
	})
	m.loopAutomationRunsMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_loop_automation_runs_total",
		Help: "Total loop trigger automation scan runs.",
	})
	m.loopAutomationScannedMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_loop_automation_scanned_total",
		Help: "Total loop goals scanned by automation.",
	})
	m.loopAutomationTriggeredMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentapi_loop_automation_triggered_total",
		Help: "Total loop automation triggers by trigger type and source.",
	}, []string{"trigger_type", "source"})
	m.loopAutomationSkippedMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_loop_automation_skipped_total",
		Help: "Total loop goals skipped by automation.",
	})
	m.loopAutomationFailedMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_loop_automation_failed_total",
		Help: "Total loop automation trigger failures.",
	})
	m.loopAutomationDedupeConflictMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_loop_automation_dedupe_conflicts_total",
		Help: "Total loop automation dedupe conflicts.",
	})
	m.loopAutomationExpiredPrunedMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_loop_automation_expired_pruned_total",
		Help: "Total expired loop trigger ledger rows pruned by automation.",
	})
	m.loopAutomationConsecutiveFailuresMetric = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentapi_loop_automation_consecutive_failures",
		Help: "Current consecutive loop automation scan failures.",
	})
	m.loopAutomationLastRunUnixMetric = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentapi_loop_automation_last_run_unix",
		Help: "Unix timestamp of the last loop automation scan.",
	})
	m.loopAutomationNextDueUnixMetric = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentapi_loop_automation_next_due_unix",
		Help: "Unix timestamp of the next expected loop automation scan.",
	})
	m.loopAutomationQuotaBlockedMetric = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentapi_loop_automation_quota_blocked_total",
		Help: "Total loop automation trigger attempts blocked by quota or policy.",
	})
	m.registry.MustRegister(
		m.requestsTotal,
		m.requestsByStatus,
		m.requestsByRoute,
		m.requestErrorsTotal,
		m.requestLatencyMSTotal,
		m.requestDurationSeconds,
		m.rateLimitedTotal,
		m.auditErrorsTotal,
		m.governanceEvents,
		m.piiRedactions,
		m.personalizationUpdates,
		m.personalizationChanges,
		m.personalizationEnabled,
		m.personalizationFields,
		m.browserMemoryTotal,
		m.liveActiveSessionsMetric,
		m.liveSessionsTotalMetric,
		m.liveSucceededTotalMetric,
		m.liveFailedTotalMetric,
		m.liveDisconnectedMetric,
		m.liveAudioChunksMetric,
		m.liveAudioBytesMetric,
		m.liveErrorsMetric,
		m.loopAutomationEnabledMetric,
		m.loopAutomationRunningMetric,
		m.loopAutomationRunsMetric,
		m.loopAutomationScannedMetric,
		m.loopAutomationTriggeredMetric,
		m.loopAutomationSkippedMetric,
		m.loopAutomationFailedMetric,
		m.loopAutomationDedupeConflictMetric,
		m.loopAutomationExpiredPrunedMetric,
		m.loopAutomationConsecutiveFailuresMetric,
		m.loopAutomationLastRunUnixMetric,
		m.loopAutomationNextDueUnixMetric,
		m.loopAutomationQuotaBlockedMetric,
	)
	return m
}

type LiveMetricsRecord struct {
	DurationMS        int64
	FirstTranscriptMS int64
	FirstAudioMS      int64
	AudioChunks       int64
	AudioBytes        int64
	ErrorCode         string
	Disconnected      bool
	Success           bool
}

type LiveHealthSnapshot struct {
	ActiveSessions           int64        `json:"active_sessions"`
	Sessions                 int64        `json:"sessions"`
	Succeeded                int64        `json:"succeeded"`
	Failed                   int64        `json:"failed"`
	Disconnected             int64        `json:"disconnected"`
	AudioChunks              int64        `json:"audio_chunks"`
	AudioBytes               int64        `json:"audio_bytes"`
	AverageDurationMS        int64        `json:"average_duration_ms"`
	AverageFirstTranscriptMS int64        `json:"average_first_transcript_ms"`
	AverageFirstAudioMS      int64        `json:"average_first_audio_ms"`
	TranscriptionSuccessRate float64      `json:"transcription_success_rate"`
	ErrorRate                float64      `json:"error_rate"`
	ErrorsByCode             []MetricPair `json:"errors_by_code"`
}

type MetricPair struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

func (m *MetricsRegistry) RecordRequest(method, path string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	route := routeLabel(path)
	statusCode := statusString(status)
	m.requestsTotal.Inc()
	m.requestsByStatus.WithLabelValues(statusCode).Inc()
	m.requestsByRoute.WithLabelValues(method + " " + route).Inc()
	m.requestLatencyMSTotal.Add(float64(duration.Milliseconds()))
	m.requestDurationSeconds.WithLabelValues(route, method, statusCode).Observe(duration.Seconds())
	if status >= http.StatusBadRequest {
		m.requestErrorsTotal.Inc()
	}
}

func (m *MetricsRegistry) IncRateLimited() {
	if m != nil {
		m.rateLimitedTotal.Inc()
	}
}

func (m *MetricsRegistry) IncAuditError() {
	if m != nil {
		m.auditErrorsTotal.Inc()
	}
}

func (m *MetricsRegistry) IncGovernanceEvent(name string) {
	if m == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "unknown"
	}
	m.governanceEvents.WithLabelValues(name).Inc()
}

func (m *MetricsRegistry) AddPIIRedactions(count int) {
	if m != nil && count > 0 {
		m.piiRedactions.Add(float64(count))
	}
}

func (m *MetricsRegistry) RecordPersonalizationUpdate(enabled bool, changed bool, fieldCoverage map[string]bool) {
	if m == nil {
		return
	}
	m.personalizationUpdates.Inc()
	if changed {
		m.personalizationChanges.Inc()
	}
	m.personalizationEnabled.WithLabelValues(boolString(enabled)).Inc()
	for field, present := range fieldCoverage {
		m.personalizationFields.WithLabelValues(strings.TrimSpace(field), boolString(present)).Inc()
	}
}

func (m *MetricsRegistry) IncPersonalizationBrowserMemory() {
	if m != nil {
		m.browserMemoryTotal.Inc()
	}
}

func (m *MetricsRegistry) IncLiveActive() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.liveActiveSessions++
	m.mu.Unlock()
	m.liveActiveSessionsMetric.Inc()
}

func (m *MetricsRegistry) DecLiveActive() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.liveActiveSessions > 0 {
		m.liveActiveSessions--
		m.liveActiveSessionsMetric.Dec()
	}
	m.mu.Unlock()
}

func (m *MetricsRegistry) RecordLiveSession(record LiveMetricsRecord) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.liveSessionsTotal++
	m.liveSessionsTotalMetric.Inc()
	if record.Success {
		m.liveSuccessfulSessions++
		m.liveSucceededTotalMetric.Inc()
	} else {
		m.liveFailedSessions++
		m.liveFailedTotalMetric.Inc()
	}
	if record.Disconnected {
		m.liveDisconnectedSessions++
		m.liveDisconnectedMetric.Inc()
	}
	audioChunks := maxInt64Value(record.AudioChunks, 0)
	audioBytes := maxInt64Value(record.AudioBytes, 0)
	m.liveDurationTotalMS += maxInt64Value(record.DurationMS, 0)
	m.liveAudioChunksTotal += audioChunks
	m.liveAudioBytesTotal += audioBytes
	m.liveAudioChunksMetric.Add(float64(audioChunks))
	m.liveAudioBytesMetric.Add(float64(audioBytes))
	if record.FirstTranscriptMS > 0 {
		m.liveFirstTranscriptTotalMS += record.FirstTranscriptMS
		m.liveFirstTranscriptCount++
	}
	if record.FirstAudioMS > 0 {
		m.liveFirstAudioTotalMS += record.FirstAudioMS
		m.liveFirstAudioCount++
	}
	if code := strings.TrimSpace(record.ErrorCode); code != "" {
		m.liveErrorsByCode[code]++
		m.liveErrorsMetric.WithLabelValues(code).Inc()
	}
}

func (m *MetricsRegistry) LiveHealthSnapshot() LiveHealthSnapshot {
	if m == nil {
		return LiveHealthSnapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := LiveHealthSnapshot{
		ActiveSessions:           m.liveActiveSessions,
		Sessions:                 m.liveSessionsTotal,
		Succeeded:                m.liveSuccessfulSessions,
		Failed:                   m.liveFailedSessions,
		Disconnected:             m.liveDisconnectedSessions,
		AudioChunks:              m.liveAudioChunksTotal,
		AudioBytes:               m.liveAudioBytesTotal,
		AverageDurationMS:        averageInt64(m.liveDurationTotalMS, m.liveSessionsTotal),
		AverageFirstTranscriptMS: averageInt64(m.liveFirstTranscriptTotalMS, m.liveFirstTranscriptCount),
		AverageFirstAudioMS:      averageInt64(m.liveFirstAudioTotalMS, m.liveFirstAudioCount),
		ErrorsByCode:             metricPairs(m.liveErrorsByCode),
	}
	if m.liveSessionsTotal > 0 {
		snapshot.ErrorRate = float64(m.liveFailedSessions) / float64(m.liveSessionsTotal)
		snapshot.TranscriptionSuccessRate = float64(m.liveFirstTranscriptCount) / float64(m.liveSessionsTotal)
	}
	return snapshot
}

func (m *MetricsRegistry) SetLoopAutomationStatus(enabled, running bool, consecutiveFailures int, lastRunAt, nextDueAt *time.Time) {
	if m == nil {
		return
	}
	m.loopAutomationEnabledMetric.Set(boolFloat(enabled))
	m.loopAutomationRunningMetric.Set(boolFloat(running))
	m.loopAutomationConsecutiveFailuresMetric.Set(float64(consecutiveFailures))
	if lastRunAt != nil && !lastRunAt.IsZero() {
		m.loopAutomationLastRunUnixMetric.Set(float64(lastRunAt.UTC().Unix()))
	}
	if nextDueAt != nil && !nextDueAt.IsZero() {
		m.loopAutomationNextDueUnixMetric.Set(float64(nextDueAt.UTC().Unix()))
	}
}

func (m *MetricsRegistry) RecordLoopAutomationReport(report LoopTriggerAutomationReport) {
	if m == nil {
		return
	}
	m.loopAutomationRunsMetric.Inc()
	if report.Scanned > 0 {
		m.loopAutomationScannedMetric.Add(float64(report.Scanned))
	}
	if report.Skipped > 0 {
		m.loopAutomationSkippedMetric.Add(float64(report.Skipped))
	}
	if report.Failed > 0 {
		m.loopAutomationFailedMetric.Add(float64(report.Failed))
	}
	if report.DedupeConflicts > 0 {
		m.loopAutomationDedupeConflictMetric.Add(float64(report.DedupeConflicts))
	}
	if report.PrunedExpired > 0 {
		m.loopAutomationExpiredPrunedMetric.Add(float64(report.PrunedExpired))
	}
	if report.QuotaBlocked > 0 {
		m.loopAutomationQuotaBlockedMetric.Add(float64(report.QuotaBlocked))
	}
}

func (m *MetricsRegistry) IncLoopAutomationTrigger(triggerType, source string) {
	if m == nil {
		return
	}
	m.loopAutomationTriggeredMetric.WithLabelValues(nonEmptyMetricLabel(triggerType), nonEmptyMetricLabel(source)).Inc()
}

func (m *MetricsRegistry) IncLoopAutomationQuotaBlocked() {
	if m != nil {
		m.loopAutomationQuotaBlockedMetric.Inc()
	}
}

func (m *MetricsRegistry) WritePrometheus(w http.ResponseWriter) {
	if m == nil {
		promhttp.HandlerFor(prometheus.NewRegistry(), promhttp.HandlerOpts{}).ServeHTTP(w, &http.Request{})
		return
	}
	promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}).ServeHTTP(w, &http.Request{})
}

func averageInt64(total, count int64) int64 {
	if count <= 0 {
		return 0
	}
	return total / count
}

func metricPairs(values map[string]int64) []MetricPair {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]MetricPair, 0, len(keys))
	for _, key := range keys {
		out = append(out, MetricPair{Key: key, Count: values[key]})
	}
	return out
}

func maxInt64Value(value, fallback int64) int64 {
	if value < fallback {
		return fallback
	}
	return value
}

func routeLabel(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		if looksLikeID(part) {
			parts[i] = ":id"
		}
	}
	return "/" + strings.Join(parts, "/")
}

func looksLikeID(part string) bool {
	if len(part) >= 16 {
		return true
	}
	if strings.Contains(part, "-") && len(part) >= 8 {
		return true
	}
	return false
}

func statusString(status int) string {
	if status <= 0 {
		return "0"
	}
	return strconv.Itoa(status)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func nonEmptyMetricLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
