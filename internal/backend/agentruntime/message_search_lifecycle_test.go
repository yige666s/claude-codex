package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestElasticsearchMessageIndexManagerBootstrapCreatesILMTemplateAndAlias(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]any
	}
	var requests []capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method == http.MethodGet && (r.URL.Path == "/_ilm/policy/agent_messages_ilm" || r.URL.Path == "/_index_template/agent_messages_template") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodHead && r.URL.Path == "/agent_messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		requests = append(requests, capturedRequest{Method: r.Method, Path: r.URL.Path, Body: body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"acknowledged":true}`))
	}))
	defer server.Close()

	manager := NewElasticsearchMessageIndexManager(MessageSearchConfig{
		Backend:  messageSearchBackendElasticsearch,
		Endpoint: server.URL,
		Index:    "agent_messages",
		Timeout:  time.Second,
	}, nil)

	if err := manager.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	byPath := make(map[string]capturedRequest, len(requests))
	for _, req := range requests {
		byPath[req.Path] = req
	}
	if byPath["/_ilm/policy/agent_messages_ilm"].Method != http.MethodPut {
		t.Fatalf("expected ILM policy PUT, got %#v", byPath["/_ilm/policy/agent_messages_ilm"])
	}
	if byPath["/_index_template/agent_messages_template"].Method != http.MethodPut {
		t.Fatalf("expected index template PUT, got %#v", byPath["/_index_template/agent_messages_template"])
	}
	if byPath["/agent_messages-000001"].Method != http.MethodPut {
		t.Fatalf("expected initial write index PUT, got %#v", byPath["/agent_messages-000001"])
	}

	template := byPath["/_index_template/agent_messages_template"].Body
	patterns := template["index_patterns"].([]any)
	if got := patterns[0].(string); got != "agent_messages-*" {
		t.Fatalf("index pattern = %q, want agent_messages-*", got)
	}
	content := template["template"].(map[string]any)["mappings"].(map[string]any)["properties"].(map[string]any)["content"].(map[string]any)
	if got := content["analyzer"]; got != "ik_max_word" {
		t.Fatalf("content analyzer = %v, want ik_max_word", got)
	}
	if got := content["search_analyzer"]; got != "ik_smart" {
		t.Fatalf("content search_analyzer = %v, want ik_smart", got)
	}
	fields := content["fields"].(map[string]any)
	raw := fields["raw"].(map[string]any)
	if got := raw["type"]; got != "keyword" {
		t.Fatalf("content.raw type = %v, want keyword", got)
	}
	settings := template["template"].(map[string]any)["settings"].(map[string]any)
	if got := settings["index.lifecycle.rollover_alias"]; got != "agent_messages" {
		t.Fatalf("rollover alias = %v, want agent_messages", got)
	}
	if got := settings["number_of_replicas"]; got != float64(0) {
		t.Fatalf("number_of_replicas = %v, want 0", got)
	}
}

func TestElasticsearchMessageIndexManagerBootstrapSkipsExistingMetadata(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/_ilm/policy/agent_messages_ilm":
			_, _ = w.Write([]byte(`{"agent_messages_ilm":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/_index_template/agent_messages_template":
			_, _ = w.Write([]byte(`{"index_templates":[{"name":"agent_messages_template"}]}`))
		case r.Method == http.MethodHead && r.URL.Path == "/agent_messages":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	manager := NewElasticsearchMessageIndexManager(MessageSearchConfig{
		Backend:  messageSearchBackendElasticsearch,
		Endpoint: server.URL,
		Index:    "agent_messages",
		Timeout:  time.Second,
	}, nil)

	if err := manager.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	for _, request := range requests {
		if strings.HasPrefix(request, http.MethodPut+" ") {
			t.Fatalf("Bootstrap() should not PUT existing metadata, requests=%v", requests)
		}
	}
}

func TestElasticsearchMessageIndexManagerEnsureIntervalIsShorterThanMaintenance(t *testing.T) {
	manager := NewElasticsearchMessageIndexManager(MessageSearchConfig{
		Backend:                  messageSearchBackendElasticsearch,
		Endpoint:                 "http://localhost:9200",
		Index:                    "agent_messages",
		IndexMaintenanceInterval: 24 * time.Hour,
		Timeout:                  time.Second,
	}, nil)

	if got := manager.indexEnsureInterval(); got != defaultMessageSearchIndexEnsureInterval {
		t.Fatalf("indexEnsureInterval() = %s, want %s", got, defaultMessageSearchIndexEnsureInterval)
	}
}

func TestElasticsearchMessageIndexManagerEnsureIntervalHonorsShortMaintenance(t *testing.T) {
	manager := NewElasticsearchMessageIndexManager(MessageSearchConfig{
		Backend:                  messageSearchBackendElasticsearch,
		Endpoint:                 "http://localhost:9200",
		Index:                    "agent_messages",
		IndexMaintenanceInterval: 10 * time.Second,
		Timeout:                  time.Second,
	}, nil)

	if got := manager.indexEnsureInterval(); got != 10*time.Second {
		t.Fatalf("indexEnsureInterval() = %s, want 10s", got)
	}
}

func TestElasticsearchMessageIndexManagerBootstrapRetryPolicyHonorsShortMaintenance(t *testing.T) {
	manager := NewElasticsearchMessageIndexManager(MessageSearchConfig{
		Backend:                  messageSearchBackendElasticsearch,
		Endpoint:                 "http://localhost:9200",
		Index:                    "agent_messages",
		IndexMaintenanceInterval: 10 * time.Second,
		Timeout:                  time.Second,
	}, nil)

	policy := manager.bootstrapRetryPolicy()
	if got := policy.Delay(1, nil); got != 10*time.Second {
		t.Fatalf("bootstrap retry delay = %s, want 10s", got)
	}
	if got := policy.Delay(3, nil); got != 10*time.Second {
		t.Fatalf("bootstrap retry max delay = %s, want 10s", got)
	}
}

func TestElasticsearchMessageIndexManagerMaintainDowngradesAndClosesOldIndices(t *testing.T) {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	type action struct {
		Method string
		Path   string
	}
	var actions []action
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/agent_messages-*/_settings":
			if got := r.URL.Query().Get("expand_wildcards"); got != "open,closed" {
				t.Fatalf("expand_wildcards = %q, want open,closed", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agent_messages-warm": map[string]any{
					"settings": map[string]any{
						"index": map[string]any{
							"creation_date": strconv.FormatInt(now.Add(-100*24*time.Hour).UnixMilli(), 10),
						},
					},
				},
				"agent_messages-cold": map[string]any{
					"settings": map[string]any{
						"index": map[string]any{
							"creation_date": strconv.FormatInt(now.Add(-200*24*time.Hour).UnixMilli(), 10),
						},
					},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/agent_messages-warm/_settings":
			actions = append(actions, action{Method: r.Method, Path: r.URL.Path})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/agent_messages-cold/_close":
			actions = append(actions, action{Method: r.Method, Path: r.URL.Path})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	manager := NewElasticsearchMessageIndexManager(MessageSearchConfig{
		Backend:             messageSearchBackendElasticsearch,
		Endpoint:            server.URL,
		Index:               "agent_messages",
		Timeout:             time.Second,
		IndexDowngradeAfter: 90 * 24 * time.Hour,
		IndexCloseAfter:     180 * 24 * time.Hour,
	}, nil)
	manager.now = func() time.Time { return now }

	processed, err := manager.Maintain(context.Background())
	if err != nil {
		t.Fatalf("Maintain() error = %v", err)
	}
	if processed != 2 {
		t.Fatalf("processed = %d, want 2", processed)
	}
	want := []action{
		{Method: http.MethodPost, Path: "/agent_messages-cold/_close"},
		{Method: http.MethodPut, Path: "/agent_messages-warm/_settings"},
	}
	if len(actions) != len(want) {
		t.Fatalf("actions = %#v, want %#v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("actions[%d] = %#v, want %#v", i, actions[i], want[i])
		}
	}
}

func TestElasticsearchMessageIndexManagerCloseIsIdempotentForClosedIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost || r.URL.Path != "/agent_messages-cold/_close" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"index_closed_exception"}}`))
	}))
	defer server.Close()

	manager := NewElasticsearchMessageIndexManager(MessageSearchConfig{
		Backend:  messageSearchBackendElasticsearch,
		Endpoint: server.URL,
		Index:    "agent_messages",
		Timeout:  time.Second,
	}, nil)

	if err := manager.closeIndex(context.Background(), "agent_messages-cold"); err != nil {
		t.Fatalf("closeIndex() error = %v", err)
	}
}

func TestElasticsearchMessageIndexManagerRealIntegration(t *testing.T) {
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("AGENTRUNTIME_REAL_ES_URL")), "/")
	if endpoint == "" {
		t.Skip("set AGENTRUNTIME_REAL_ES_URL to run against a real Elasticsearch node with IK installed")
	}
	ctx := context.Background()
	alias := "agent_messages_real_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	manager := NewElasticsearchMessageIndexManager(MessageSearchConfig{
		Backend:                    messageSearchBackendElasticsearch,
		Endpoint:                   endpoint,
		Index:                      alias,
		Timeout:                    10 * time.Second,
		IndexDowngradeAfter:        time.Hour,
		IndexCloseAfter:            0,
		IndexMaintenanceInterval:   time.Hour,
		IndexMaintenanceBatchLimit: 10,
	}, nil)
	defer cleanupRealElasticsearchMessageIndex(t, manager, alias)

	if err := manager.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() against real ES error = %v", err)
	}

	var policy map[string]any
	if err := manager.getJSON(ctx, joinEndpointPath(endpoint, "_ilm", "policy", alias+"_ilm"), &policy); err != nil {
		t.Fatalf("get ILM policy error = %v", err)
	}
	if _, ok := policy[alias+"_ilm"]; !ok {
		t.Fatalf("ILM policy %s_ilm not found in response %#v", alias, policy)
	}

	var mapping map[string]any
	if err := manager.getJSON(ctx, joinEndpointPath(endpoint, alias+"-000001", "_mapping"), &mapping); err != nil {
		t.Fatalf("get mapping error = %v", err)
	}
	contentMapping, ok := nestedMap(mapping, alias+"-000001", "mappings", "properties", "content")
	if !ok {
		t.Fatalf("content mapping missing: %#v", mapping)
	}
	if got := contentMapping["analyzer"]; got != "ik_max_word" {
		t.Fatalf("content analyzer = %v, want ik_max_word", got)
	}
	if got := contentMapping["search_analyzer"]; got != "ik_smart" {
		t.Fatalf("content search_analyzer = %v, want ik_smart", got)
	}

	var analyzed map[string]any
	if err := postRealElasticsearchJSON(ctx, manager, joinEndpointPath(endpoint, "_analyze"), map[string]any{
		"analyzer": "ik_smart",
		"text":     "我想查询昨天的订单消息",
	}, &analyzed); err != nil {
		t.Fatalf("IK _analyze error = %v", err)
	}
	tokens, ok := analyzed["tokens"].([]any)
	if !ok || len(tokens) == 0 {
		t.Fatalf("IK _analyze returned no tokens: %#v", analyzed)
	}

	manager.now = func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
	processed, err := manager.Maintain(ctx)
	if err != nil {
		t.Fatalf("Maintain() downgrade against real ES error = %v", err)
	}
	if processed == 0 {
		t.Fatalf("Maintain() processed 0 indices, want at least 1")
	}
	var settings map[string]any
	if err := manager.getJSON(ctx, joinEndpointPath(endpoint, alias+"-000001", "_settings"), &settings); err != nil {
		t.Fatalf("get downgraded settings error = %v", err)
	}
	indexSettings, ok := nestedMap(settings, alias+"-000001", "settings", "index")
	if !ok {
		t.Fatalf("index settings missing: %#v", settings)
	}
	blocks, ok := indexSettings["blocks"].(map[string]any)
	if !ok {
		t.Fatalf("index blocks settings missing: %#v", indexSettings)
	}
	if got := fmt.Sprint(blocks["write"]); got != "true" {
		t.Fatalf("index blocks.write = %v, want true", blocks["write"])
	}

	manager.config.IndexDowngradeAfter = 0
	manager.config.IndexCloseAfter = time.Hour
	processed, err = manager.Maintain(ctx)
	if err != nil {
		t.Fatalf("Maintain() close against real ES error = %v", err)
	}
	if processed == 0 {
		t.Fatalf("Maintain() close processed 0 indices, want at least 1")
	}
	var cat []map[string]any
	if err := manager.getJSON(ctx, joinEndpointPath(endpoint, "_cat", "indices", alias+"-000001")+"?expand_wildcards=open,closed&format=json&h=status,index", &cat); err != nil {
		t.Fatalf("cat closed index error = %v", err)
	}
	if len(cat) != 1 || cat[0]["status"] != "close" {
		t.Fatalf("closed index cat response = %#v, want status close", cat)
	}
}

func postRealElasticsearchJSON(ctx context.Context, manager *ElasticsearchMessageIndexManager, url string, payload any, out any) error {
	status, data, err := manager.doRequest(ctx, http.MethodPost, url, payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("elasticsearch POST failed: status %d: %s", status, string(data))
	}
	return json.Unmarshal(data, out)
}

func cleanupRealElasticsearchMessageIndex(t *testing.T, manager *ElasticsearchMessageIndexManager, alias string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint := manager.endpoint()
	_, _, _ = manager.doRequest(ctx, http.MethodPost, joinEndpointPath(endpoint, alias+"-*", "_open")+"?expand_wildcards=closed", nil)
	_, _, _ = manager.doRequest(ctx, http.MethodDelete, joinEndpointPath(endpoint, alias+"-*")+"?expand_wildcards=open,closed", nil)
	_, _, _ = manager.doRequest(ctx, http.MethodDelete, joinEndpointPath(endpoint, "_index_template", alias+"_template"), nil)
	_, _, _ = manager.doRequest(ctx, http.MethodDelete, joinEndpointPath(endpoint, "_ilm", "policy", alias+"_ilm"), nil)
}

func nestedMap(value map[string]any, keys ...string) (map[string]any, bool) {
	current := value
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}
