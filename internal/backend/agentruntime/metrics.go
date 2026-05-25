package agentruntime

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type MetricsRegistry struct {
	mu                                sync.Mutex
	requestsTotal                     int64
	requestsByStatus                  map[int]int64
	requestsByRoute                   map[string]int64
	governanceEvents                  map[string]int64
	personalizationEnabled            map[string]int64
	personalizationFieldCoverage      map[string]int64
	errorsTotal                       int64
	rateLimitedTotal                  int64
	auditErrorsTotal                  int64
	piiRedactions                     int64
	latencyTotalMS                    int64
	personalizationUpdatesTotal       int64
	personalizationChangesTotal       int64
	personalizationBrowserMemoryTotal int64
	liveActiveSessions                int64
	liveSessionsTotal                 int64
	liveSuccessfulSessions            int64
	liveFailedSessions                int64
	liveDisconnectedSessions          int64
	liveAudioChunksTotal              int64
	liveAudioBytesTotal               int64
	liveDurationTotalMS               int64
	liveFirstTranscriptTotalMS        int64
	liveFirstTranscriptCount          int64
	liveFirstAudioTotalMS             int64
	liveFirstAudioCount               int64
	liveErrorsByCode                  map[string]int64
}

func NewMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{
		requestsByStatus:             make(map[int]int64),
		requestsByRoute:              make(map[string]int64),
		governanceEvents:             make(map[string]int64),
		personalizationEnabled:       make(map[string]int64),
		personalizationFieldCoverage: make(map[string]int64),
		liveErrorsByCode:             make(map[string]int64),
	}
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestsTotal++
	m.requestsByStatus[status]++
	m.requestsByRoute[method+" "+routeLabel(path)]++
	if status >= http.StatusBadRequest {
		m.errorsTotal++
	}
	m.latencyTotalMS += duration.Milliseconds()
}

func (m *MetricsRegistry) IncRateLimited() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.rateLimitedTotal++
	m.mu.Unlock()
}

func (m *MetricsRegistry) IncAuditError() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.auditErrorsTotal++
	m.mu.Unlock()
}

func (m *MetricsRegistry) IncGovernanceEvent(name string) {
	if m == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "unknown"
	}
	m.mu.Lock()
	m.governanceEvents[name]++
	m.mu.Unlock()
}

func (m *MetricsRegistry) AddPIIRedactions(count int) {
	if m == nil || count <= 0 {
		return
	}
	m.mu.Lock()
	m.piiRedactions += int64(count)
	m.mu.Unlock()
}

func (m *MetricsRegistry) RecordPersonalizationUpdate(enabled bool, changed bool, fieldCoverage map[string]bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.personalizationUpdatesTotal++
	if changed {
		m.personalizationChangesTotal++
	}
	m.personalizationEnabled[fmt.Sprintf("%t", enabled)]++
	for field, present := range fieldCoverage {
		key := strings.TrimSpace(field) + "\x00" + fmt.Sprintf("%t", present)
		m.personalizationFieldCoverage[key]++
	}
}

func (m *MetricsRegistry) IncPersonalizationBrowserMemory() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.personalizationBrowserMemoryTotal++
	m.mu.Unlock()
}

func (m *MetricsRegistry) IncLiveActive() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.liveActiveSessions++
	m.mu.Unlock()
}

func (m *MetricsRegistry) DecLiveActive() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.liveActiveSessions > 0 {
		m.liveActiveSessions--
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
	if record.Success {
		m.liveSuccessfulSessions++
	} else {
		m.liveFailedSessions++
	}
	if record.Disconnected {
		m.liveDisconnectedSessions++
	}
	m.liveDurationTotalMS += maxInt64Value(record.DurationMS, 0)
	m.liveAudioChunksTotal += maxInt64Value(record.AudioChunks, 0)
	m.liveAudioBytesTotal += maxInt64Value(record.AudioBytes, 0)
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

func (m *MetricsRegistry) WritePrometheus(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if m == nil {
		_, _ = fmt.Fprintln(w, "agentapi_requests_total 0")
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = fmt.Fprintln(w, "# HELP agentapi_requests_total Total HTTP requests.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_requests_total counter")
	_, _ = fmt.Fprintf(w, "agentapi_requests_total %d\n", m.requestsTotal)
	_, _ = fmt.Fprintln(w, "# HELP agentapi_request_errors_total Total HTTP requests with 4xx/5xx status.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_request_errors_total counter")
	_, _ = fmt.Fprintf(w, "agentapi_request_errors_total %d\n", m.errorsTotal)
	_, _ = fmt.Fprintln(w, "# HELP agentapi_rate_limited_total Total rate-limited requests.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_rate_limited_total counter")
	_, _ = fmt.Fprintf(w, "agentapi_rate_limited_total %d\n", m.rateLimitedTotal)
	_, _ = fmt.Fprintln(w, "# HELP agentapi_audit_errors_total Total audit log write failures.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_audit_errors_total counter")
	_, _ = fmt.Fprintf(w, "agentapi_audit_errors_total %d\n", m.auditErrorsTotal)
	_, _ = fmt.Fprintln(w, "# HELP agentapi_pii_redactions_total Total PII redactions observed in user-facing governance operations.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_pii_redactions_total counter")
	_, _ = fmt.Fprintf(w, "agentapi_pii_redactions_total %d\n", m.piiRedactions)
	_, _ = fmt.Fprintln(w, "# HELP agentapi_request_latency_ms_total Total HTTP request latency in milliseconds.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_request_latency_ms_total counter")
	_, _ = fmt.Fprintf(w, "agentapi_request_latency_ms_total %d\n", m.latencyTotalMS)
	_, _ = fmt.Fprintln(w, "# HELP agentapi_personalization_updates_total Total personalization setting save/reset operations.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_personalization_updates_total counter")
	_, _ = fmt.Fprintf(w, "agentapi_personalization_updates_total %d\n", m.personalizationUpdatesTotal)
	_, _ = fmt.Fprintln(w, "# HELP agentapi_personalization_changes_total Total personalization operations that changed persisted settings.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_personalization_changes_total counter")
	_, _ = fmt.Fprintf(w, "agentapi_personalization_changes_total %d\n", m.personalizationChangesTotal)
	_, _ = fmt.Fprintln(w, "# HELP agentapi_personalization_browser_memory_total Total browser memory submissions accepted.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_personalization_browser_memory_total counter")
	_, _ = fmt.Fprintf(w, "agentapi_personalization_browser_memory_total %d\n", m.personalizationBrowserMemoryTotal)
	_, _ = fmt.Fprintln(w, "# HELP agentapi_live_sessions_total Total Live websocket sessions.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_live_sessions_total counter")
	_, _ = fmt.Fprintf(w, "agentapi_live_sessions_total %d\n", m.liveSessionsTotal)
	_, _ = fmt.Fprintf(w, "agentapi_live_sessions_active %d\n", m.liveActiveSessions)
	_, _ = fmt.Fprintf(w, "agentapi_live_sessions_failed_total %d\n", m.liveFailedSessions)
	_, _ = fmt.Fprintf(w, "agentapi_live_sessions_disconnected_total %d\n", m.liveDisconnectedSessions)
	_, _ = fmt.Fprintf(w, "agentapi_live_audio_chunks_total %d\n", m.liveAudioChunksTotal)
	_, _ = fmt.Fprintf(w, "agentapi_live_audio_bytes_total %d\n", m.liveAudioBytesTotal)
	liveErrorCodes := make([]string, 0, len(m.liveErrorsByCode))
	for code := range m.liveErrorsByCode {
		liveErrorCodes = append(liveErrorCodes, code)
	}
	sort.Strings(liveErrorCodes)
	for _, code := range liveErrorCodes {
		_, _ = fmt.Fprintf(w, "agentapi_live_errors_total{code=%q} %d\n", code, m.liveErrorsByCode[code])
	}

	statuses := make([]int, 0, len(m.requestsByStatus))
	for status := range m.requestsByStatus {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)
	for _, status := range statuses {
		_, _ = fmt.Fprintf(w, "agentapi_requests_by_status_total{status=\"%d\"} %d\n", status, m.requestsByStatus[status])
	}
	routes := make([]string, 0, len(m.requestsByRoute))
	for route := range m.requestsByRoute {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	for _, route := range routes {
		_, _ = fmt.Fprintf(w, "agentapi_requests_by_route_total{route=%q} %d\n", route, m.requestsByRoute[route])
	}
	_, _ = fmt.Fprintln(w, "# HELP agentapi_governance_events_total Total C-end governance events.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_governance_events_total counter")
	eventNames := make([]string, 0, len(m.governanceEvents))
	for name := range m.governanceEvents {
		eventNames = append(eventNames, name)
	}
	sort.Strings(eventNames)
	for _, name := range eventNames {
		_, _ = fmt.Fprintf(w, "agentapi_governance_events_total{event=%q} %d\n", name, m.governanceEvents[name])
	}
	_, _ = fmt.Fprintln(w, "# HELP agentapi_personalization_enabled_total Personalization operations by whether effective personalization is enabled.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_personalization_enabled_total counter")
	enabledLabels := make([]string, 0, len(m.personalizationEnabled))
	for label := range m.personalizationEnabled {
		enabledLabels = append(enabledLabels, label)
	}
	sort.Strings(enabledLabels)
	for _, label := range enabledLabels {
		_, _ = fmt.Fprintf(w, "agentapi_personalization_enabled_total{enabled=%q} %d\n", label, m.personalizationEnabled[label])
	}
	_, _ = fmt.Fprintln(w, "# HELP agentapi_personalization_field_coverage_total Personalization operations by field and whether that field is populated.")
	_, _ = fmt.Fprintln(w, "# TYPE agentapi_personalization_field_coverage_total counter")
	fieldKeys := make([]string, 0, len(m.personalizationFieldCoverage))
	for key := range m.personalizationFieldCoverage {
		fieldKeys = append(fieldKeys, key)
	}
	sort.Strings(fieldKeys)
	for _, key := range fieldKeys {
		parts := strings.SplitN(key, "\x00", 2)
		field, present := parts[0], ""
		if len(parts) > 1 {
			present = parts[1]
		}
		_, _ = fmt.Fprintf(w, "agentapi_personalization_field_coverage_total{field=%q,present=%q} %d\n", field, present, m.personalizationFieldCoverage[key])
	}
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
