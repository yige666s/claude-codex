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
}

func NewMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{
		requestsByStatus:             make(map[int]int64),
		requestsByRoute:              make(map[string]int64),
		governanceEvents:             make(map[string]int64),
		personalizationEnabled:       make(map[string]int64),
		personalizationFieldCoverage: make(map[string]int64),
	}
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
