package web

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestFetchToolRejectsLoopbackTargets(t *testing.T) {
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain")
		_, _ = w.Write([]byte("hello from web fetch"))
	}))
	defer server.Close()

	tool := NewFetchTool(server.Client())
	input, _ := json.Marshal(map[string]any{"url": server.URL})
	_, err := tool.Execute(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "not publicly routable") {
		t.Fatalf("expected loopback rejection, got %v", err)
	}
}

func TestFetchToolReturnsPublicTextContent(t *testing.T) {
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN", "")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"content-type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("hello from web fetch")),
			Request:    r,
		}, nil
	})}
	tool := NewFetchTool(client)
	input, _ := json.Marshal(map[string]any{"url": "https://example.com/page"})
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

	tool := NewFetchToolWithAllowlist(server.Client(), []string{"example.com"})
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
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_CDP_TIMEOUT", "8s")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_CDP_POLL_INTERVAL", "1ms")

	var closedSession bool
	var acceptedCookies bool
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
					params, _ := request["params"].(map[string]any)
					expression, _ := params["expression"].(string)
					value := `{"readyState":"complete","title":"Rendered","url":"https://example.com/page","content":"Rendered CDP content"}`
					if !strings.Contains(expression, "readyState") {
						acceptedCookies = true
						result = map[string]any{
							"result": map[string]any{
								"type":  "boolean",
								"value": true,
							},
						}
						if err := conn.WriteJSON(map[string]any{"id": id, "result": result}); err != nil {
							return
						}
						continue
					}
					if !acceptedCookies {
						value = `{"readyState":"complete","title":"Rendered","url":"https://example.com/page","content":"This website uses cookies. Accept All"}`
					}
					result = map[string]any{
						"result": map[string]any{
							"type":  "string",
							"value": value,
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

	tool := NewFetchToolWithAllowlist(server.Client(), []string{"example.com"})
	input, _ := json.Marshal(map[string]any{"url": "https://example.com/page", "prompt": "extract content"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch execute: %v", err)
	}
	if !closedSession {
		t.Fatal("expected cdp session cleanup")
	}
	if !acceptedCookies {
		t.Fatal("expected cookie banner dismissal")
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

	tool := NewFetchToolWithAllowlist(client, []string{"example.com"})
	input, _ := json.Marshal(map[string]any{"url": "https://example.com/page", "prompt": "extract content"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch execute: %v", err)
	}
	if !strings.Contains(result.Output, "cloudflare_crawl_error:") || !strings.Contains(result.Output, "cloudflare_cdp_error:") || !strings.Contains(result.Output, "direct fallback content") {
		t.Fatalf("unexpected fallback output: %q", result.Output)
	}
}

func TestFetchToolFallsBackToBrowserlessWhenDirectFetchLooksBlocked(t *testing.T) {
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN", "")
	t.Setenv("AGENT_API_WEBFETCH_BROWSERLESS_API_TOKEN", "bl-token")
	t.Setenv("AGENT_API_WEBFETCH_BROWSERLESS_BASE_URL", "https://browserless.test")

	var sawBrowserless bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "example.com":
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Status:     "403 Forbidden",
				Header:     http.Header{"content-type": []string{"text/html"}},
				Body:       io.NopCloser(strings.NewReader("<html><body>Access denied by Cloudflare</body></html>")),
				Request:    r,
			}, nil
		case "browserless.test":
			sawBrowserless = true
			if r.URL.Path != "/smart-scrape" {
				t.Fatalf("unexpected browserless path: %s", r.URL.Path)
			}
			if r.URL.Query().Get("token") != "bl-token" {
				t.Fatalf("unexpected browserless token query: %s", r.URL.RawQuery)
			}
			if r.Header.Get("authorization") != "" {
				t.Fatalf("unexpected browserless authorization header: %q", r.Header.Get("authorization"))
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode browserless request: %v", err)
			}
			if body["url"] != "https://example.com/page" {
				t.Fatalf("unexpected browserless body: %#v", body)
			}
			if _, ok := body["prompt"]; ok {
				t.Fatalf("unexpected browserless body: %#v", body)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"content-type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"url":"https://example.com/page","title":"Rendered","status":200,"markdown":"# Rendered Browserless content"}`)),
				Request:    r,
			}, nil
		default:
			t.Fatalf("unexpected host: %s", r.URL.Host)
		}
		return nil, nil
	})}

	tool := NewFetchToolWithAllowlist(client, []string{"example.com"})
	input, _ := json.Marshal(map[string]any{"url": "https://example.com/page", "prompt": "extract content"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch execute: %v", err)
	}
	if !sawBrowserless {
		t.Fatal("expected browserless fallback request")
	}
	if !strings.Contains(result.Output, "fallback: browserless_smart_scrape") ||
		!strings.Contains(result.Output, "source: browserless_smart_scrape") ||
		!strings.Contains(result.Output, "Rendered Browserless content") {
		t.Fatalf("unexpected browserless fallback output: %q", result.Output)
	}
}

func TestFetchToolDoesNotDelegateUnallowlistedTargetsToRemoteBrowsers(t *testing.T) {
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID", "acct-1")
	t.Setenv("AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN", "cf-token")
	t.Setenv("AGENT_API_WEBFETCH_BROWSERLESS_API_TOKEN", "bl-token")
	t.Setenv("AGENT_API_WEBFETCH_BROWSERLESS_BASE_URL", "https://browserless.test")

	var remoteRendererCalled bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "example.com" {
			remoteRendererCalled = true
			t.Fatalf("unallowlisted target delegated to remote renderer: %s", r.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"content-type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("direct only")),
			Request:    r,
		}, nil
	})}

	tool := NewFetchTool(client)
	input, _ := json.Marshal(map[string]any{"url": "https://example.com/page"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch execute: %v", err)
	}
	if remoteRendererCalled {
		t.Fatal("remote renderer must require an explicit target-domain allowlist")
	}
	if !strings.Contains(result.Output, "direct only") {
		t.Fatalf("expected direct fetch result, got %q", result.Output)
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

type staticIPResolver map[string][]net.IP

func (r staticIPResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ips := r[host]
	out := make([]net.IPAddr, 0, len(ips))
	for _, ip := range ips {
		out = append(out, net.IPAddr{IP: ip})
	}
	return out, nil
}

type rebindingResolver struct {
	calls int
}

func disableFetchFallbacks(tool *FetchTool) {
	tool.cloudflareCrawl = nil
	tool.cloudflareCDP = nil
	tool.browserless = nil
}

func (r *rebindingResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	r.calls++
	if r.calls == 1 {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
}

func TestFetchToolRejectsPrivateDNSResolutionBeforeRequest(t *testing.T) {
	called := false
	tool := NewFetchTool(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})})
	disableFetchFallbacks(tool)
	tool.resolver = staticIPResolver{"internal.example": {net.ParseIP("10.0.0.9")}}
	input, _ := json.Marshal(map[string]any{"url": "https://internal.example/data"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "not publicly routable") {
		t.Fatalf("expected private DNS rejection, got %v", err)
	}
	if called {
		t.Fatal("request executed before private DNS address was rejected")
	}
}

func TestFetchToolChecksRedirectBeforeFollowing(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     http.Header{"Location": []string{"http://127.0.0.1/admin"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})}
	tool := NewFetchTool(client)
	disableFetchFallbacks(tool)
	tool.resolver = staticIPResolver{"safe.example": {net.ParseIP("93.184.216.34")}}
	input, _ := json.Marshal(map[string]any{"url": "https://safe.example/page"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "redirect blocked") {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("private redirect was followed; requests=%d", requests)
	}
}

func TestFetchToolDialerRejectsDNSRebinding(t *testing.T) {
	tool := NewFetchTool(&http.Client{Timeout: time.Second})
	disableFetchFallbacks(tool)
	resolver := &rebindingResolver{}
	tool.resolver = resolver
	input, _ := json.Marshal(map[string]any{"url": "https://rebind.example/page"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "not publicly routable") {
		t.Fatalf("expected DNS rebinding rejection, got %v", err)
	}
	if resolver.calls < 2 {
		t.Fatalf("expected validation and dial-time DNS checks, calls=%d", resolver.calls)
	}
}

func TestFetchToolCapsMaxBytesAtServerLimit(t *testing.T) {
	payload := strings.Repeat("x", int(serverFetchMaxBytes*2))
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"content-type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
	})}
	tool := NewFetchTool(client)
	disableFetchFallbacks(tool)
	tool.resolver = staticIPResolver{"safe.example": {net.ParseIP("93.184.216.34")}}
	input, _ := json.Marshal(map[string]any{"url": "https://safe.example/page", "max_bytes": serverFetchMaxBytes * 4})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(result.Output) > int(serverFetchMaxBytes)+512 {
		t.Fatalf("server max_bytes cap was not enforced: output=%d", len(result.Output))
	}
}

func TestFetchToolRejectsUnsupportedScheme(t *testing.T) {
	tool := NewFetchTool(http.DefaultClient)
	input, _ := json.Marshal(map[string]any{"url": "file:///etc/passwd"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Fatalf("expected scheme rejection, got %v", err)
	}
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
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sawRequest = true
		if r.URL.Scheme != "https" || r.URL.Host != "api.tavily.com" || r.URL.Path != "/search" {
			t.Fatalf("unexpected Tavily URL: %s", r.URL)
		}
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

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"content-type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"answer":"brief answer","results":[{"title":"Example Result","url":"https://example.com","content":"Useful snippet","score":0.9}]}`)),
			Request:    r,
		}, nil
	})}

	tool := NewSearchTool(client)
	input, _ := json.Marshal(map[string]any{
		"query":           "example",
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
	t.Setenv("AGENT_API_TAVILY_API_KEY", "tvly-test")
	t.Setenv("AGENT_API_TAVILY_SEARCH_ENDPOINT", "https://attacker.example/search")
	called := false
	tool := NewSearchTool(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})})
	input, _ := json.Marshal(map[string]any{"query": "example"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected untrusted endpoint rejection")
	}
	if !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("expected trusted endpoint error, got %v", err)
	}
	if called {
		t.Fatal("untrusted endpoint must be rejected before any request or credential transmission")
	}
}

func TestSearchToolRejectsPrivateTavilyDNSBeforeAttachingCredentials(t *testing.T) {
	t.Setenv("AGENT_API_TAVILY_API_KEY", "tvly-test")
	called := false
	tool := NewSearchTool(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})})
	tool.resolver = staticIPResolver{"api.tavily.com": {net.ParseIP("127.0.0.1")}}
	input, _ := json.Marshal(map[string]any{"query": "example"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "not publicly routable") {
		t.Fatalf("expected private Tavily DNS rejection, got %v", err)
	}
	if called {
		t.Fatal("credential-bearing Tavily request ran before DNS validation")
	}
}

func TestSearchToolDoesNotForwardCredentialsAcrossRedirect(t *testing.T) {
	t.Setenv("AGENT_API_TAVILY_API_KEY", "tvly-test")
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "api.tavily.com" {
			t.Fatalf("credential-bearing request reached untrusted host %q", r.URL.Host)
		}
		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     http.Header{"Location": []string{"https://attacker.example/collect"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})}
	tool := NewSearchTool(client)
	input, _ := json.Marshal(map[string]any{"query": "example"})
	_, err := tool.Execute(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "redirect blocked") {
		t.Fatalf("expected Tavily redirect rejection, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("untrusted redirect was followed; requests=%d", requests)
	}
}

func TestSearchToolSchemaDoesNotExposeEndpoint(t *testing.T) {
	schema := string(NewSearchTool(http.DefaultClient).InputSchema())
	if strings.Contains(schema, "endpoint") {
		t.Fatalf("search endpoint must not be model-controlled: %s", schema)
	}
}

func TestFetchToolSchemaMatchesOptionalPromptAndServerByteCap(t *testing.T) {
	schema := string(NewFetchTool(http.DefaultClient).InputSchema())
	if strings.Contains(schema, `"required":["url","prompt"]`) {
		t.Fatalf("optional prompt must not be required: %s", schema)
	}
	if !strings.Contains(schema, `"maximum":1048576`) || !strings.Contains(schema, `"additionalProperties":false`) {
		t.Fatalf("schema must expose the server byte cap and reject unknown fields: %s", schema)
	}
}

func TestSearchToolFiltersDomains(t *testing.T) {
	t.Setenv("AGENT_API_TAVILY_API_KEY", "tvly-test")

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"content-type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"results":[
			{"title":"Allowed","url":"https://allowed.example.com/one","content":"Allowed content","score":0.9},
			{"title":"Blocked","url":"https://blocked.example.com/two","content":"Blocked content","score":0.8}
		]}`)),
			Request: r,
		}, nil
	})}

	tool := NewSearchTool(client)
	input, _ := json.Marshal(map[string]any{
		"query":           "example",
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
