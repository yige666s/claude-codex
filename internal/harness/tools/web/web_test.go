package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestFetchToolReturnsTextContent(t *testing.T) {
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain")
		_, _ = w.Write([]byte("hello from web fetch"))
	}))
	defer server.Close()

	tool := NewFetchTool(server.Client())
	input, _ := json.Marshal(map[string]any{"url": server.URL})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch execute: %v", err)
	}
	if !strings.Contains(result.Output, "hello from web fetch") {
		t.Fatalf("unexpected fetch output: %q", result.Output)
	}
}

func TestFetchToolUsesCloudflareCrawlWhenConfigured(t *testing.T) {
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID", "acct-1")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN", "token-1")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_POLL_INTERVAL", "1ms")

	var sawStart bool
	var sawPoll bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer token-1" {
			t.Fatalf("missing authorization header: %q", r.Header.Get("authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/client/v4/accounts/acct-1/browser-rendering/crawl":
			sawStart = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode start body: %v", err)
			}
			if body["url"] != "https://example.com/page" || body["render"] != true || body["depth"] != float64(1) {
				t.Fatalf("unexpected crawl body: %#v", body)
			}
			_, _ = w.Write([]byte(`{"success":true,"result":"job-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/client/v4/accounts/acct-1/browser-rendering/crawl/job-1":
			sawPoll = true
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"job-1","status":"completed","records":[{"url":"https://example.com/page","status":"completed","markdown":"# Rendered page\nUseful content","metadata":{"status":200,"title":"Rendered","url":"https://example.com/page"}}]}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_BASE_URL", server.URL+"/client/v4")

	tool := NewFetchTool(server.Client())
	input, _ := json.Marshal(map[string]any{"url": "https://example.com/page", "prompt": "extract content"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch execute: %v", err)
	}
	if !sawStart || !sawPoll {
		t.Fatal("expected cloudflare crawl start and poll requests")
	}
	if !strings.Contains(result.Output, "source: cloudflare_browser_rendering_crawl") || !strings.Contains(result.Output, "Useful content") {
		t.Fatalf("unexpected fetch output: %q", result.Output)
	}
}

func TestFetchToolFallsBackToCloudflareCDPWhenCrawlFails(t *testing.T) {
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID", "acct-1")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN", "token-1")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_CDP_TIMEOUT", "2s")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_CDP_POLL_INTERVAL", "1ms")

	var closedSession bool
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdp/page" && r.Header.Get("authorization") != "Bearer token-1" {
			t.Fatalf("missing authorization header: %q", r.Header.Get("authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/client/v4/accounts/acct-1/browser-rendering/crawl":
			http.Error(w, `{"success":false,"errors":[{"message":"crawl failed"}]}`, http.StatusBadGateway)
		case r.Method == http.MethodPost && r.URL.Path == "/client/v4/accounts/acct-1/browser-rendering/devtools/browser":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"sessionId":"session-1","webSocketDebuggerUrl":"` + wsURL(serverURL(r), "/cdp/browser") + `"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/client/v4/accounts/acct-1/browser-rendering/devtools/browser/session-1/json/new":
			if r.URL.Query().Get("url") != "https://example.com/page" {
				t.Fatalf("unexpected tab url: %s", r.URL.RawQuery)
			}
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"id":"target-1","type":"page","url":"https://example.com/page","title":"Example","webSocketDebuggerUrl":"` + wsURL(serverURL(r), "/cdp/page") + `"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/client/v4/accounts/acct-1/browser-rendering/devtools/browser/session-1":
			closedSession = true
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"status":"closing"}`))
		case r.URL.Path == "/cdp/page":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Fatalf("upgrade cdp websocket: %v", err)
			}
			defer conn.Close()
			for {
				var request map[string]any
				if err := conn.ReadJSON(&request); err != nil {
					return
				}
				id := int(request["id"].(float64))
				method, _ := request["method"].(string)
				result := map[string]any{}
				if method == "Runtime.evaluate" {
					result = map[string]any{
						"result": map[string]any{
							"type":  "string",
							"value": `{"readyState":"complete","title":"Rendered","url":"https://example.com/page","content":"Rendered CDP content"}`,
						},
					}
				}
				if err := conn.WriteJSON(map[string]any{"id": id, "result": result}); err != nil {
					return
				}
			}
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_BASE_URL", server.URL+"/client/v4")

	tool := NewFetchTool(server.Client())
	input, _ := json.Marshal(map[string]any{"url": "https://example.com/page", "prompt": "extract content"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch execute: %v", err)
	}
	if !closedSession {
		t.Fatal("expected cdp session cleanup")
	}
	if !strings.Contains(result.Output, "fallback: cloudflare_cdp") ||
		!strings.Contains(result.Output, "source: cloudflare_browser_run_cdp") ||
		!strings.Contains(result.Output, "Rendered CDP content") {
		t.Fatalf("unexpected cdp fallback output: %q", result.Output)
	}
}

func TestFetchToolFallsBackWhenCloudflareCrawlFails(t *testing.T) {
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID", "acct-1")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN", "bad-token")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_BASE_URL", "https://cloudflare.test/client/v4")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_POLL_INTERVAL", "1ms")

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := "direct fallback content"
		if strings.Contains(r.URL.Path, "/browser-rendering/crawl") {
			status = http.StatusUnauthorized
			body = `{"success":false,"errors":[{"message":"Authentication error"}]}`
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     http.Header{"content-type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}

	tool := NewFetchTool(client)
	input, _ := json.Marshal(map[string]any{"url": "https://example.com/page", "prompt": "extract content"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch execute: %v", err)
	}
	if !strings.Contains(result.Output, "cloudflare_crawl_error:") || !strings.Contains(result.Output, "cloudflare_cdp_error:") || !strings.Contains(result.Output, "direct fallback content") {
		t.Fatalf("unexpected fallback output: %q", result.Output)
	}
}

func serverURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func wsURL(baseURL, path string) string {
	return "ws" + strings.TrimPrefix(baseURL, "http") + path
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestFetchToolRejectsDomainsOutsideAllowlist(t *testing.T) {
	tool := NewFetchToolWithAllowlist(http.DefaultClient, []string{"allowed.example.com"})
	input, _ := json.Marshal(map[string]any{"url": "https://blocked.example.com/resource"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected allowlist rejection")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected not allowed error, got %v", err)
	}
}

func TestSearchToolUsesTavilyAPI(t *testing.T) {
	t.Setenv("AGENT_API_TAVILY_API_KEY", "tvly-test")

	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("authorization") != "Bearer tvly-test" {
			t.Fatalf("missing authorization header: %q", r.Header.Get("authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode search body: %v", err)
		}
		if body["query"] != "example" || body["search_depth"] != "basic" || body["max_results"] != float64(2) || body["include_answer"] != true {
			t.Fatalf("unexpected search body: %#v", body)
		}
		includeDomains, _ := body["include_domains"].([]any)
		excludeDomains, _ := body["exclude_domains"].([]any)
		if len(includeDomains) != 1 || includeDomains[0] != "example.com" || len(excludeDomains) != 1 || excludeDomains[0] != "blocked.example.com" {
			t.Fatalf("unexpected domain filters: %#v", body)
		}

		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"answer":"brief answer","results":[{"title":"Example Result","url":"https://example.com","content":"Useful snippet","score":0.9}]}`))
	}))
	defer server.Close()

	tool := NewSearchTool(server.Client())
	input, _ := json.Marshal(map[string]any{
		"query":           "example",
		"endpoint":        server.URL,
		"max_results":     2,
		"allowed_domains": []string{"www.example.com"},
		"blocked_domains": []string{"blocked.example.com"},
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("search execute: %v", err)
	}
	if !sawRequest {
		t.Fatal("expected tavily request")
	}
	if !strings.Contains(result.Output, "answer: brief answer") || !strings.Contains(result.Output, "Example Result - https://example.com") || !strings.Contains(result.Output, "Useful snippet") {
		t.Fatalf("unexpected search output: %q", result.Output)
	}
}

func TestSearchToolRejectsEndpointOutsideAllowlist(t *testing.T) {
	tool := NewSearchToolWithAllowlist(http.DefaultClient, []string{"allowed.example.com"})
	input, _ := json.Marshal(map[string]any{"query": "example", "endpoint": "https://blocked.example.com/html/"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected allowlist rejection")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected not allowed error, got %v", err)
	}
}

func TestSearchToolFiltersDomains(t *testing.T) {
	t.Setenv("AGENT_API_TAVILY_API_KEY", "tvly-test")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"title":"Allowed","url":"https://allowed.example.com/one","content":"Allowed content","score":0.9},
			{"title":"Blocked","url":"https://blocked.example.com/two","content":"Blocked content","score":0.8}
		]}`))
	}))
	defer server.Close()

	tool := NewSearchTool(server.Client())
	input, _ := json.Marshal(map[string]any{
		"query":           "example",
		"endpoint":        server.URL,
		"allowed_domains": []string{"example.com"},
		"blocked_domains": []string{"blocked.example.com"},
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("search execute: %v", err)
	}
	if !strings.Contains(result.Output, "Allowed") || strings.Contains(result.Output, "Blocked") {
		t.Fatalf("unexpected filtered output: %q", result.Output)
	}
}
