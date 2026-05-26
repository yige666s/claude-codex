package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"claude-codex/internal/backend/httpclient"
)

type ElasticsearchMessageIndexManager struct {
	config MessageSearchConfig
	client *http.Client
	logger *slog.Logger
	now    func() time.Time
}

func NewElasticsearchMessageIndexManager(config MessageSearchConfig, logger *log.Logger) *ElasticsearchMessageIndexManager {
	return NewElasticsearchMessageIndexManagerWithLogger(config, newStructuredLogger(logger))
}

func NewElasticsearchMessageIndexManagerWithLogger(config MessageSearchConfig, logger *slog.Logger) *ElasticsearchMessageIndexManager {
	config = normalizeMessageSearchConfig(config)
	return &ElasticsearchMessageIndexManager{
		config: config,
		client: &http.Client{Timeout: config.Timeout},
		logger: componentLogger(logger, "message_search_index_manager"),
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (m *ElasticsearchMessageIndexManager) Bootstrap(ctx context.Context) error {
	if m == nil || strings.TrimSpace(m.config.Endpoint) == "" {
		return errMessageSearchNotConfigured("elasticsearch index manager")
	}
	if m.config.Backend != messageSearchBackendElasticsearch && m.config.Backend != messageSearchBackendHybrid {
		return nil
	}
	if err := m.putJSON(ctx, joinEndpointPath(m.endpoint(), "_ilm", "policy", m.config.IndexLifecyclePolicy), m.lifecyclePolicy()); err != nil {
		return err
	}
	if err := m.putJSON(ctx, joinEndpointPath(m.endpoint(), "_index_template", m.config.IndexTemplateName), m.indexTemplate()); err != nil {
		return err
	}
	return m.ensureWriteAlias(ctx)
}

func (m *ElasticsearchMessageIndexManager) Run(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("elasticsearch index manager is not configured")
	}
	retryPolicy := m.bootstrapRetryPolicy()
	attempt := 1
	for {
		if err := m.Bootstrap(ctx); err != nil {
			if errorsIsContextDone(ctx, err) {
				return err
			}
			logError(ctx, m.logger, "elasticsearch message index bootstrap failed", err)
			if err := retryPolicy.Sleep(ctx, attempt, err); err != nil {
				return err
			}
			attempt++
			continue
		}
		break
	}
	ensureTicker := time.NewTicker(m.indexEnsureInterval())
	defer ensureTicker.Stop()
	maintenanceTicker := time.NewTicker(m.config.IndexMaintenanceInterval)
	defer maintenanceTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ensureTicker.C:
			if err := m.Bootstrap(ctx); err != nil && !errorsIsContextDone(ctx, err) {
				logError(ctx, m.logger, "elasticsearch message index bootstrap failed", err)
			}
		case <-maintenanceTicker.C:
			if err := m.Bootstrap(ctx); err != nil && !errorsIsContextDone(ctx, err) {
				logError(ctx, m.logger, "elasticsearch message index bootstrap failed", err)
				continue
			}
			if _, err := m.Maintain(ctx); err != nil && !errorsIsContextDone(ctx, err) {
				logError(ctx, m.logger, "elasticsearch message index maintenance failed", err)
			}
		}
	}
}

func (m *ElasticsearchMessageIndexManager) bootstrapRetryPolicy() RetryPolicy {
	retryDelay := 30 * time.Second
	if m != nil && m.config.IndexMaintenanceInterval > 0 && m.config.IndexMaintenanceInterval < retryDelay {
		retryDelay = m.config.IndexMaintenanceInterval
	}
	return RetryPolicy{
		BaseDelay: retryDelay,
		MaxDelay:  retryDelay,
	}
}

func (m *ElasticsearchMessageIndexManager) indexEnsureInterval() time.Duration {
	interval := defaultMessageSearchIndexEnsureInterval
	if m != nil && m.config.IndexMaintenanceInterval > 0 && m.config.IndexMaintenanceInterval < interval {
		interval = m.config.IndexMaintenanceInterval
	}
	return interval
}

func (m *ElasticsearchMessageIndexManager) Maintain(ctx context.Context) (int, error) {
	if m == nil || strings.TrimSpace(m.config.Endpoint) == "" {
		return 0, nil
	}
	if m.config.Backend != messageSearchBackendElasticsearch && m.config.Backend != messageSearchBackendHybrid {
		return 0, nil
	}
	indices, err := m.listManagedIndices(ctx)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, index := range indices {
		if m.config.IndexMaintenanceBatchLimit > 0 && processed >= m.config.IndexMaintenanceBatchLimit {
			break
		}
		age := m.now().Sub(index.CreatedAt)
		switch {
		case m.config.IndexCloseAfter > 0 && age >= m.config.IndexCloseAfter:
			if err := m.closeIndex(ctx, index.Name); err != nil {
				return processed, err
			}
			processed++
		case m.config.IndexDowngradeAfter > 0 && age >= m.config.IndexDowngradeAfter:
			if err := m.downgradeIndex(ctx, index.Name); err != nil {
				return processed, err
			}
			processed++
		}
	}
	return processed, nil
}

type managedSearchIndex struct {
	Name      string
	CreatedAt time.Time
}

func (m *ElasticsearchMessageIndexManager) listManagedIndices(ctx context.Context) ([]managedSearchIndex, error) {
	pattern := m.managedIndexPattern()
	var response map[string]struct {
		Settings struct {
			Index struct {
				CreationDate string `json:"creation_date"`
			} `json:"index"`
		} `json:"settings"`
	}
	if err := m.getJSON(ctx, joinEndpointPath(m.endpoint(), pattern, "_settings")+"?expand_wildcards=open,closed", &response); err != nil {
		return nil, err
	}
	out := make([]managedSearchIndex, 0, len(response))
	for name, item := range response {
		if name == "." || strings.HasPrefix(name, ".") {
			continue
		}
		createdAt, err := parseElasticMillis(item.Settings.Index.CreationDate)
		if err != nil || createdAt.IsZero() {
			continue
		}
		out = append(out, managedSearchIndex{Name: name, CreatedAt: createdAt})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (m *ElasticsearchMessageIndexManager) ensureWriteAlias(ctx context.Context) error {
	alias := strings.TrimSpace(m.config.IndexWriteAlias)
	if alias == "" || strings.ContainsAny(alias, "*?,") {
		return nil
	}
	status, _, err := m.doRequest(ctx, http.MethodHead, joinEndpointPath(m.endpoint(), alias), nil)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("check elasticsearch message index alias failed: status %d", status)
	}
	body := map[string]any{
		"aliases": map[string]any{
			alias: map[string]any{"is_write_index": true},
		},
	}
	return m.putJSON(ctx, joinEndpointPath(m.endpoint(), m.initialIndexName()), body)
}

func (m *ElasticsearchMessageIndexManager) downgradeIndex(ctx context.Context, index string) error {
	body := map[string]any{
		"index": map[string]any{
			"blocks.write":       true,
			"number_of_replicas": 0,
			"refresh_interval":   "60s",
			"priority":           0,
		},
	}
	return m.putJSON(ctx, joinEndpointPath(m.endpoint(), index, "_settings"), body)
}

func (m *ElasticsearchMessageIndexManager) closeIndex(ctx context.Context, index string) error {
	status, data, err := m.doRequest(ctx, http.MethodPost, joinEndpointPath(m.endpoint(), index, "_close"), nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		if status == http.StatusBadRequest && strings.Contains(strings.ToLower(string(data)), "index_closed_exception") {
			return nil
		}
		return fmt.Errorf("close elasticsearch message index %s failed: status %d: %s", index, status, string(data))
	}
	return nil
}

func (m *ElasticsearchMessageIndexManager) lifecyclePolicy() map[string]any {
	policy := map[string]any{
		"phases": map[string]any{
			"hot": map[string]any{
				"actions": map[string]any{
					"rollover": map[string]any{
						"max_age":                "30d",
						"max_primary_shard_size": "50gb",
					},
					"set_priority": map[string]any{"priority": 100},
				},
			},
		},
	}
	phases := policy["phases"].(map[string]any)
	if m.config.IndexDowngradeAfter > 0 {
		phases["warm"] = map[string]any{
			"min_age": elasticDuration(m.config.IndexDowngradeAfter),
			"actions": map[string]any{
				"readonly":     map[string]any{},
				"set_priority": map[string]any{"priority": 50},
			},
		}
	}
	if m.config.IndexCloseAfter > 0 {
		phases["cold"] = map[string]any{
			"min_age": elasticDuration(m.config.IndexCloseAfter),
			"actions": map[string]any{
				"set_priority": map[string]any{"priority": 0},
			},
		}
	}
	return map[string]any{"policy": policy}
}

func (m *ElasticsearchMessageIndexManager) indexTemplate() map[string]any {
	return map[string]any{
		"index_patterns": []string{m.managedIndexPattern()},
		"priority":       500,
		"template": map[string]any{
			"settings": map[string]any{
				"index.lifecycle.name":           m.config.IndexLifecyclePolicy,
				"index.lifecycle.rollover_alias": m.config.IndexWriteAlias,
				"number_of_shards":               1,
				"number_of_replicas":             0,
			},
			"mappings": map[string]any{
				"dynamic": false,
				"properties": map[string]any{
					"message_id":    map[string]any{"type": "keyword"},
					"attachment_id": map[string]any{"type": "keyword"},
					"source_type":   map[string]any{"type": "keyword"},
					"user_id":       map[string]any{"type": "keyword"},
					"session_id":    map[string]any{"type": "keyword"},
					"seq_no":        map[string]any{"type": "long"},
					"message_index": map[string]any{"type": "integer"},
					"chunk_index":   map[string]any{"type": "integer"},
					"role":          map[string]any{"type": "keyword"},
					"status":        map[string]any{"type": "integer"},
					"hidden":        map[string]any{"type": "boolean"},
					"created_at":    map[string]any{"type": "date"},
					"session_title": textMapping(m.config.IndexAnalyzer, m.config.IndexSearchAnalyzer),
					"content":       textMapping(m.config.IndexAnalyzer, m.config.IndexSearchAnalyzer),
					"tool_output":   textMapping(m.config.IndexAnalyzer, m.config.IndexSearchAnalyzer),
					"file_name":     textMapping(m.config.IndexAnalyzer, m.config.IndexSearchAnalyzer),
					"file_type":     map[string]any{"type": "keyword"},
					"mime_type":     map[string]any{"type": "keyword"},
					"content_parts": map[string]any{
						"properties": map[string]any{
							"type":      map[string]any{"type": "keyword"},
							"text":      textMapping(m.config.IndexAnalyzer, m.config.IndexSearchAnalyzer),
							"file_name": textMapping(m.config.IndexAnalyzer, m.config.IndexSearchAnalyzer),
						},
					},
				},
			},
		},
	}
}

func textMapping(analyzer, searchAnalyzer string) map[string]any {
	mapping := map[string]any{"type": "text"}
	if strings.TrimSpace(analyzer) != "" {
		mapping["analyzer"] = strings.TrimSpace(analyzer)
	}
	if strings.TrimSpace(searchAnalyzer) != "" {
		mapping["search_analyzer"] = strings.TrimSpace(searchAnalyzer)
	}
	return mapping
}

func (m *ElasticsearchMessageIndexManager) putJSON(ctx context.Context, url string, payload any) error {
	status, data, err := m.doRequest(ctx, http.MethodPut, url, payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("elasticsearch message index request failed: status %d: %s", status, string(data))
	}
	return nil
}

func (m *ElasticsearchMessageIndexManager) getJSON(ctx context.Context, url string, out any) error {
	status, data, err := m.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("elasticsearch message index request failed: status %d: %s", status, string(data))
	}
	return json.Unmarshal(data, out)
}

func (m *ElasticsearchMessageIndexManager) doRequest(ctx context.Context, method, url string, payload any) (int, []byte, error) {
	headers := make(http.Header)
	if payload != nil {
		headers.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(m.config.APIKey) != "" {
		headers.Set("Authorization", "ApiKey "+strings.TrimSpace(m.config.APIKey))
	}
	if strings.TrimSpace(m.config.Username) != "" || strings.TrimSpace(m.config.Password) != "" {
		headers.Set("Authorization", basicAuthHeader(m.config.Username, m.config.Password))
	}
	status, data, _, err := httpclient.New(
		httpclient.WithHTTPClient(m.client),
		httpclient.WithComponent("message_search_index_manager"),
		httpclient.WithMaxBodyBytes(4096),
	).Bytes(ctx, method, url, payload, httpclient.WithHeaders(headers), httpclient.WithAnyStatus())
	return status, data, err
}

func (m *ElasticsearchMessageIndexManager) endpoint() string {
	return strings.TrimRight(strings.TrimSpace(m.config.Endpoint), "/")
}

func (m *ElasticsearchMessageIndexManager) managedIndexPattern() string {
	return strings.TrimSpace(m.config.IndexWriteAlias) + "-*"
}

func (m *ElasticsearchMessageIndexManager) initialIndexName() string {
	return strings.TrimSpace(m.config.IndexWriteAlias) + "-000001"
}

func parseElasticMillis(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	millis, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(millis).UTC(), nil
}

func elasticDuration(value time.Duration) string {
	if value <= 0 {
		return "0ms"
	}
	if value%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(value/(24*time.Hour)))
	}
	if value%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(value/time.Hour))
	}
	if value%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(value/time.Minute))
	}
	return fmt.Sprintf("%dms", value.Milliseconds())
}
