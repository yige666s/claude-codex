package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type FetchTool struct {
	client          *http.Client
	allowedDomains  []string
	cloudflareCrawl *cloudflareCrawlClient
	cloudflareCDP   *cloudflareCDPClient
}

type fetchInput struct {
	URL      string `json:"url"`
	Prompt   string `json:"prompt,omitempty"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type cloudflareCrawlClient struct {
	client       *http.Client
	accountID    string
	apiToken     string
	baseURL      string
	pollAttempts int
	pollInterval time.Duration
}

type cloudflareCDPClient struct {
	client       *http.Client
	dialer       *websocket.Dialer
	accountID    string
	apiToken     string
	baseURL      string
	timeout      time.Duration
	pollInterval time.Duration
}

func NewFetchTool(client *http.Client) *FetchTool {
	return NewFetchToolWithAllowlist(client, nil)
}

func NewFetchToolWithAllowlist(client *http.Client, allowedDomains []string) *FetchTool {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &FetchTool{
		client:          client,
		allowedDomains:  append([]string(nil), allowedDomains...),
		cloudflareCrawl: newCloudflareCrawlClientFromEnv(client),
		cloudflareCDP:   newCloudflareCDPClientFromEnv(client),
	}
}

func (t *FetchTool) Name() string {
	return "WebFetch"
}

func (t *FetchTool) Description() string {
	return "Fetch a web page or JSON resource and return a text representation."
}

func (t *FetchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"The URL to fetch content from"},"prompt":{"type":"string","description":"The prompt describing what information to extract from the fetched content"},"max_bytes":{"type":"integer","description":"Maximum bytes to read before processing"}},"required":["url","prompt"]}`)
}

func (t *FetchTool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *FetchTool) IsConcurrencySafe() bool {
	return true // web fetch is read-only and safe to run concurrently
}

func (t *FetchTool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var input fetchInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return toolkit.Result{}, err
	}
	if strings.TrimSpace(input.URL) == "" {
		return toolkit.Result{}, fmt.Errorf("url is required")
	}

	requestURL := strings.TrimSpace(input.URL)
	if strings.HasPrefix(requestURL, "http://") && !strings.HasPrefix(requestURL, "http://localhost") && !strings.HasPrefix(requestURL, "http://127.0.0.1") {
		requestURL = "https://" + strings.TrimPrefix(requestURL, "http://")
	}
	if err := validateURLAllowed(requestURL, t.allowedDomains); err != nil {
		return toolkit.Result{}, err
	}

	if t.cloudflareCrawl != nil && shouldUseCloudflareCrawl(requestURL) {
		result, err := t.cloudflareCrawl.fetch(ctx, input, requestURL, t.allowedDomains)
		if err == nil {
			return result, nil
		}
		crawlErr := err
		cdpErr := error(nil)
		if t.cloudflareCDP != nil {
			cdp, err := t.cloudflareCDP.fetch(ctx, input, requestURL, t.allowedDomains)
			if err == nil {
				cdp.Output = fmt.Sprintf("cloudflare_crawl_error: %s\nfallback: cloudflare_cdp\n%s", crawlErr.Error(), cdp.Output)
				return cdp, nil
			}
			cdpErr = err
		}
		direct, directErr := t.fetchDirect(ctx, input, requestURL)
		if directErr != nil {
			if cdpErr != nil {
				return toolkit.Result{}, fmt.Errorf("cloudflare crawl failed: %w; cloudflare cdp failed: %v; direct fetch failed: %v", crawlErr, cdpErr, directErr)
			}
			return toolkit.Result{}, fmt.Errorf("cloudflare crawl failed: %w; direct fetch failed: %v", crawlErr, directErr)
		}
		if cdpErr != nil {
			direct.Output = fmt.Sprintf("cloudflare_crawl_error: %s\ncloudflare_cdp_error: %s\nfallback: direct_http\n%s", crawlErr.Error(), cdpErr.Error(), direct.Output)
		} else {
			direct.Output = fmt.Sprintf("cloudflare_crawl_error: %s\nfallback: direct_http\n%s", crawlErr.Error(), direct.Output)
		}
		return direct, nil
	}
	return t.fetchDirect(ctx, input, requestURL)
}

func (t *FetchTool) fetchDirect(ctx context.Context, input fetchInput, requestURL string) (toolkit.Result, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return toolkit.Result{}, err
	}
	request.Header.Set("user-agent", "claude-codex-phase2/1.0")

	response, err := t.client.Do(request)
	if err != nil {
		return toolkit.Result{}, err
	}
	defer response.Body.Close()
	if response.Request != nil && response.Request.URL != nil {
		if err := validateURLAllowed(response.Request.URL.String(), t.allowedDomains); err != nil {
			return toolkit.Result{}, err
		}
	}

	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 32 * 1024
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes))
	if err != nil {
		return toolkit.Result{}, err
	}

	contentType := response.Header.Get("content-type")
	payload := strings.TrimSpace(string(body))
	if strings.Contains(contentType, "text/html") {
		payload = stripHTML(payload)
	}

	finalURL := response.Request.URL.String()
	var builder strings.Builder
	fmt.Fprintf(&builder, "status: %s\ncontent_type: %s\nurl: %s\n", response.Status, contentType, finalURL)
	if finalURL != requestURL {
		fmt.Fprintf(&builder, "redirected_from: %s\n", requestURL)
	}
	if strings.TrimSpace(input.Prompt) != "" {
		fmt.Fprintf(&builder, "prompt: %s\n", strings.TrimSpace(input.Prompt))
	}
	fmt.Fprintf(&builder, "\n%s", payload)
	return toolkit.Result{Output: builder.String()}, nil
}

func newCloudflareCrawlClientFromEnv(client *http.Client) *cloudflareCrawlClient {
	accountID := strings.TrimSpace(firstEnvValue(
		"AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID",
		"CLOUDFLARE_BROWSER_RENDERING_ACCOUNT_ID",
	))
	apiToken := strings.TrimSpace(firstEnvValue(
		"AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN",
		"CLOUDFLARE_BROWSER_RENDERING_API_TOKEN",
	))
	if accountID == "" || apiToken == "" {
		return nil
	}
	return &cloudflareCrawlClient{
		client:       client,
		accountID:    accountID,
		apiToken:     apiToken,
		baseURL:      strings.TrimRight(firstNonEmpty(firstEnvValue("AGENT_API_WEBFETCH_CLOUDFLARE_BASE_URL"), "https://api.cloudflare.com/client/v4"), "/"),
		pollAttempts: envIntValue("AGENT_API_WEBFETCH_CLOUDFLARE_POLL_ATTEMPTS", 8),
		pollInterval: envDurationValue("AGENT_API_WEBFETCH_CLOUDFLARE_POLL_INTERVAL", time.Second),
	}
}

func newCloudflareCDPClientFromEnv(client *http.Client) *cloudflareCDPClient {
	accountID := strings.TrimSpace(firstEnvValue(
		"AGENT_API_WEBFETCH_CLOUDFLARE_ACCOUNT_ID",
		"CLOUDFLARE_BROWSER_RENDERING_ACCOUNT_ID",
	))
	apiToken := strings.TrimSpace(firstEnvValue(
		"AGENT_API_WEBFETCH_CLOUDFLARE_API_TOKEN",
		"CLOUDFLARE_BROWSER_RENDERING_API_TOKEN",
	))
	if accountID == "" || apiToken == "" {
		return nil
	}
	return &cloudflareCDPClient{
		client:       client,
		dialer:       websocket.DefaultDialer,
		accountID:    accountID,
		apiToken:     apiToken,
		baseURL:      strings.TrimRight(firstNonEmpty(firstEnvValue("AGENT_API_WEBFETCH_CLOUDFLARE_BASE_URL"), "https://api.cloudflare.com/client/v4"), "/"),
		timeout:      envDurationValue("AGENT_API_WEBFETCH_CLOUDFLARE_CDP_TIMEOUT", 20*time.Second),
		pollInterval: envDurationValue("AGENT_API_WEBFETCH_CLOUDFLARE_CDP_POLL_INTERVAL", 500*time.Millisecond),
	}
}

func shouldUseCloudflareCrawl(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return parsed.Scheme == "https" && host != "localhost" && host != "127.0.0.1" && host != "::1"
}

func (c *cloudflareCrawlClient) fetch(ctx context.Context, input fetchInput, requestURL string, allowedDomains []string) (toolkit.Result, error) {
	jobID, err := c.start(ctx, requestURL)
	if err != nil {
		return toolkit.Result{}, err
	}
	job, err := c.wait(ctx, jobID)
	if err != nil {
		return toolkit.Result{}, err
	}
	record, err := bestCrawlRecord(job)
	if err != nil {
		return toolkit.Result{}, err
	}
	finalURL := firstNonEmpty(record.Metadata.URL, record.URL, requestURL)
	if err := validateURLAllowed(finalURL, allowedDomains); err != nil {
		return toolkit.Result{}, err
	}
	content := strings.TrimSpace(firstNonEmpty(record.Markdown, stripHTML(record.HTML)))
	if content == "" {
		return toolkit.Result{}, fmt.Errorf("cloudflare crawl returned no readable content for %s (job=%s status=%s)", requestURL, jobID, record.Status)
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "status: cloudflare_crawl %s", job.Status)
	if record.Status != "" {
		fmt.Fprintf(&builder, " / %s", record.Status)
	}
	fmt.Fprintf(&builder, "\ncontent_type: text/markdown\nurl: %s\nsource: cloudflare_browser_rendering_crawl\ncloudflare_job_id: %s\n", finalURL, jobID)
	if record.Metadata.Status > 0 {
		fmt.Fprintf(&builder, "http_status: %d\n", record.Metadata.Status)
	}
	if finalURL != requestURL {
		fmt.Fprintf(&builder, "redirected_from: %s\n", requestURL)
	}
	if strings.TrimSpace(input.Prompt) != "" {
		fmt.Fprintf(&builder, "prompt: %s\n", strings.TrimSpace(input.Prompt))
	}
	fmt.Fprintf(&builder, "\n%s", content)
	return toolkit.Result{Output: builder.String()}, nil
}

func (c *cloudflareCrawlClient) start(ctx context.Context, requestURL string) (string, error) {
	payload := map[string]any{
		"url":           requestURL,
		"limit":         1,
		"depth":         1,
		"formats":       []string{"markdown"},
		"render":        true,
		"crawlPurposes": []string{"search", "ai-input"},
	}
	var response cloudflareStartResponse
	if err := c.doJSON(ctx, http.MethodPost, c.crawlURL(""), payload, &response); err != nil {
		return "", err
	}
	if !response.Success || strings.TrimSpace(response.Result) == "" {
		return "", fmt.Errorf("cloudflare crawl did not return a job id")
	}
	return strings.TrimSpace(response.Result), nil
}

func (c *cloudflareCrawlClient) wait(ctx context.Context, jobID string) (cloudflareCrawlJob, error) {
	attempts := c.pollAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var job cloudflareCrawlJob
	for attempt := 0; attempt < attempts; attempt++ {
		polled, err := c.load(ctx, jobID, "limit=1")
		if err != nil {
			return cloudflareCrawlJob{}, err
		}
		job = polled
		switch strings.TrimSpace(job.Status) {
		case "completed":
			return c.load(ctx, jobID, "")
		case "running", "":
			if attempt == attempts-1 {
				break
			}
			timer := time.NewTimer(c.pollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return cloudflareCrawlJob{}, ctx.Err()
			case <-timer.C:
			}
		default:
			return cloudflareCrawlJob{}, fmt.Errorf("cloudflare crawl job %s ended with status %s", jobID, job.Status)
		}
	}
	return cloudflareCrawlJob{}, fmt.Errorf("cloudflare crawl job %s did not complete within %d polls", jobID, attempts)
}

func (c *cloudflareCrawlClient) load(ctx context.Context, jobID, query string) (cloudflareCrawlJob, error) {
	endpoint := c.crawlURL(jobID)
	if strings.TrimSpace(query) != "" {
		endpoint += "?" + query
	}
	var response cloudflareJobResponse
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return cloudflareCrawlJob{}, err
	}
	return response.Result, nil
}

func (c *cloudflareCrawlClient) doJSON(ctx context.Context, method, endpoint string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(raw))
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "Bearer "+c.apiToken)
	if payload != nil {
		request.Header.Set("content-type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("cloudflare crawl API failed: %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode cloudflare crawl response: %w", err)
	}
	return nil
}

func (c *cloudflareCrawlClient) crawlURL(jobID string) string {
	base := c.baseURL + "/accounts/" + url.PathEscape(c.accountID) + "/browser-rendering/crawl"
	if strings.TrimSpace(jobID) == "" {
		return base
	}
	return base + "/" + url.PathEscape(jobID)
}

func (c *cloudflareCDPClient) fetch(ctx context.Context, input fetchInput, requestURL string, allowedDomains []string) (toolkit.Result, error) {
	timeout := c.timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	session, err := c.createSession(ctx)
	if err != nil {
		return toolkit.Result{}, err
	}
	defer c.closeSession(session.SessionID)

	target, err := c.createTarget(ctx, session.SessionID, requestURL)
	if err != nil {
		return toolkit.Result{}, err
	}
	if strings.TrimSpace(target.WebSocketDebuggerURL) == "" {
		return toolkit.Result{}, fmt.Errorf("cloudflare cdp target did not return a websocket URL")
	}

	page, err := c.readPage(ctx, target.WebSocketDebuggerURL)
	if err != nil {
		return toolkit.Result{}, err
	}
	finalURL := firstNonEmpty(page.URL, target.URL, requestURL)
	if err := validateURLAllowed(finalURL, allowedDomains); err != nil {
		return toolkit.Result{}, err
	}
	content := strings.TrimSpace(page.Content)
	if content == "" {
		return toolkit.Result{}, fmt.Errorf("cloudflare cdp returned no readable content for %s", requestURL)
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "status: cloudflare_cdp rendered\ncontent_type: text/plain\nurl: %s\nsource: cloudflare_browser_run_cdp\ncloudflare_session_id: %s\n", finalURL, session.SessionID)
	if strings.TrimSpace(page.Title) != "" {
		fmt.Fprintf(&builder, "title: %s\n", strings.TrimSpace(page.Title))
	}
	if finalURL != requestURL {
		fmt.Fprintf(&builder, "redirected_from: %s\n", requestURL)
	}
	if strings.TrimSpace(input.Prompt) != "" {
		fmt.Fprintf(&builder, "prompt: %s\n", strings.TrimSpace(input.Prompt))
	}
	fmt.Fprintf(&builder, "\n%s", content)
	return toolkit.Result{Output: builder.String()}, nil
}

func (c *cloudflareCDPClient) createSession(ctx context.Context) (cloudflareCDPSession, error) {
	endpoint := c.devtoolsURL("") + "?keep_alive=" + url.QueryEscape(strconv.FormatInt(int64(c.timeout/time.Millisecond), 10))
	var session cloudflareCDPSession
	if err := c.doJSON(ctx, http.MethodPost, endpoint, nil, &session); err != nil {
		return cloudflareCDPSession{}, err
	}
	if strings.TrimSpace(session.SessionID) == "" {
		return cloudflareCDPSession{}, fmt.Errorf("cloudflare cdp did not return a session id")
	}
	return session, nil
}

func (c *cloudflareCDPClient) createTarget(ctx context.Context, sessionID, requestURL string) (cloudflareCDPTarget, error) {
	endpoint := c.devtoolsURL(sessionID) + "/json/new?url=" + url.QueryEscape(requestURL)
	var target cloudflareCDPTarget
	if err := c.doJSON(ctx, http.MethodPut, endpoint, nil, &target); err != nil {
		return cloudflareCDPTarget{}, err
	}
	return target, nil
}

func (c *cloudflareCDPClient) closeSession(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.doJSON(ctx, http.MethodDelete, c.devtoolsURL(sessionID), nil, nil)
}

func (c *cloudflareCDPClient) readPage(ctx context.Context, webSocketURL string) (cloudflareCDPPage, error) {
	headers := http.Header{}
	headers.Set("authorization", "Bearer "+c.apiToken)
	conn, _, err := c.dialer.DialContext(ctx, webSocketURL, headers)
	if err != nil {
		return cloudflareCDPPage{}, fmt.Errorf("connect cloudflare cdp websocket: %w", err)
	}
	defer conn.Close()

	client := &cdpSocket{conn: conn}
	_ = client.call(ctx, "Page.enable", nil, nil)
	_ = client.call(ctx, "Runtime.enable", nil, nil)

	expression := cdpPageSnapshotExpression()
	var cookieRetryUntil time.Time
	for {
		page, err := client.evaluatePage(ctx, expression)
		if err == nil && strings.TrimSpace(page.Content) != "" && (page.ReadyState == "interactive" || page.ReadyState == "complete") {
			if clicked, _ := client.evaluateBool(ctx, cdpCookieDismissExpression()); clicked {
				if waited, waitErr := client.waitForRenderedText(ctx, expression, c.pollInterval, len(strings.TrimSpace(page.Content))); waitErr == nil {
					return waited, nil
				}
			}
			if looksLikeCookieBanner(page.Content) {
				if cookieRetryUntil.IsZero() {
					cookieRetryUntil = time.Now().Add(5 * time.Second)
				}
				if time.Now().Before(cookieRetryUntil) {
					select {
					case <-ctx.Done():
						return page, nil
					case <-time.After(c.pollInterval):
					}
					continue
				}
			}
			return page, nil
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return cloudflareCDPPage{}, fmt.Errorf("read cloudflare cdp page: %w", err)
			}
			return cloudflareCDPPage{}, ctx.Err()
		case <-time.After(c.pollInterval):
		}
	}
}

func cdpPageSnapshotExpression() string {
	return `(() => JSON.stringify({readyState: document.readyState, title: document.title, url: location.href, content: (document.body && document.body.innerText) || document.documentElement.innerText || ""}))()`
}

func cdpCookieDismissExpression() string {
	return `(() => {
  const rejectPatterns = [
    /reject( all)?/i,
    /reject non[- ]?essential/i,
    /necessary only/i,
    /essential only/i,
    /only necessary/i,
    /仅必要/,
    /拒绝/,
    /全部拒绝/
  ];
  const acceptPatterns = [
    /accept all/i,
    /allow all/i,
    /agree/i,
    /got it/i,
    /ok/i,
    /同意/,
    /接受/,
    /全部接受/
  ];
  const candidates = [...document.querySelectorAll('button,[role="button"],a,input[type="button"],input[type="submit"]')]
    .map((el) => ({ el, text: ((el.innerText || el.textContent || el.value || el.getAttribute('aria-label') || '') + '').trim() }))
    .filter((item) => {
      if (!item.text) return false;
      const style = window.getComputedStyle(item.el);
      const rect = item.el.getBoundingClientRect();
      return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
    });
  const choose = (patterns) => candidates.find(({ text }) => patterns.some((pattern) => pattern.test(text)));
  const chosen = choose(rejectPatterns) || choose(acceptPatterns);
  if (!chosen) return false;
  chosen.el.click();
  return true;
})()`
}

func (c *cloudflareCDPClient) doJSON(ctx context.Context, method, endpoint string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(raw))
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "Bearer "+c.apiToken)
	if payload != nil {
		request.Header.Set("content-type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("cloudflare cdp API failed: %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode cloudflare cdp response: %w", err)
	}
	return nil
}

func (c *cloudflareCDPClient) devtoolsURL(sessionID string) string {
	base := c.baseURL + "/accounts/" + url.PathEscape(c.accountID) + "/browser-rendering/devtools/browser"
	if strings.TrimSpace(sessionID) == "" {
		return base
	}
	return base + "/" + url.PathEscape(sessionID)
}

type cdpSocket struct {
	conn   *websocket.Conn
	nextID int
}

func (c *cdpSocket) evaluatePage(ctx context.Context, expression string) (cloudflareCDPPage, error) {
	params := map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	}
	var evaluated cdpEvaluateResult
	if err := c.call(ctx, "Runtime.evaluate", params, &evaluated); err != nil {
		return cloudflareCDPPage{}, err
	}
	if evaluated.ExceptionDetails != nil {
		return cloudflareCDPPage{}, fmt.Errorf("runtime evaluation raised an exception")
	}
	raw, ok := evaluated.Result.Value.(string)
	if !ok {
		return cloudflareCDPPage{}, fmt.Errorf("runtime evaluation returned %T, expected string", evaluated.Result.Value)
	}
	var page cloudflareCDPPage
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		return cloudflareCDPPage{}, fmt.Errorf("decode cdp page snapshot: %w", err)
	}
	return page, nil
}

func (c *cdpSocket) evaluateBool(ctx context.Context, expression string) (bool, error) {
	params := map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	}
	var evaluated cdpEvaluateResult
	if err := c.call(ctx, "Runtime.evaluate", params, &evaluated); err != nil {
		return false, err
	}
	if evaluated.ExceptionDetails != nil {
		return false, fmt.Errorf("runtime evaluation raised an exception")
	}
	value, ok := evaluated.Result.Value.(bool)
	if !ok {
		return false, fmt.Errorf("runtime evaluation returned %T, expected bool", evaluated.Result.Value)
	}
	return value, nil
}

func (c *cdpSocket) waitForRenderedText(ctx context.Context, expression string, interval time.Duration, previousLen int) (cloudflareCDPPage, error) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	deadline := time.Now().Add(10 * time.Second)
	var best cloudflareCDPPage
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if strings.TrimSpace(best.Content) != "" {
				return best, nil
			}
			return cloudflareCDPPage{}, ctx.Err()
		case <-timer.C:
		}
		page, err := c.evaluatePage(ctx, expression)
		if err != nil {
			return cloudflareCDPPage{}, err
		}
		contentLen := len(strings.TrimSpace(page.Content))
		if contentLen > len(strings.TrimSpace(best.Content)) {
			best = page
		}
		if contentLen > previousLen+1000 {
			return page, nil
		}
		if time.Now().After(deadline) && strings.TrimSpace(best.Content) != "" {
			return best, nil
		}
	}
}

func looksLikeCookieBanner(content string) bool {
	lower := strings.ToLower(content)
	signals := []string{
		"cookie",
		"cookies",
		"privacy policy",
		"storage preferences",
		"targeted advertising",
		"personalization",
		"analytics",
		"reject non-essential",
		"accept all",
		"隐私政策",
		"接受",
		"同意",
	}
	for _, signal := range signals {
		if strings.Contains(lower, strings.ToLower(signal)) {
			return true
		}
	}
	return false
}

func (c *cdpSocket) call(ctx context.Context, method string, params any, out any) error {
	c.nextID++
	id := c.nextID
	request := map[string]any{
		"id":     id,
		"method": method,
	}
	if params != nil {
		request["params"] = params
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(deadline)
		_ = c.conn.SetReadDeadline(deadline)
	}
	if err := c.conn.WriteJSON(request); err != nil {
		return err
	}
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return err
		}
		var response cdpResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return err
		}
		if response.ID != id {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("cdp %s failed: %s", method, response.Error.Message)
		}
		if out != nil && len(response.Result) > 0 {
			if err := json.Unmarshal(response.Result, out); err != nil {
				return fmt.Errorf("decode cdp %s response: %w", method, err)
			}
		}
		return nil
	}
}

func bestCrawlRecord(job cloudflareCrawlJob) (cloudflareCrawlRecord, error) {
	if len(job.Records) == 0 {
		return cloudflareCrawlRecord{}, fmt.Errorf("cloudflare crawl completed with no records")
	}
	for _, record := range job.Records {
		if strings.EqualFold(record.Status, "completed") && strings.TrimSpace(firstNonEmpty(record.Markdown, stripHTML(record.HTML))) != "" {
			return record, nil
		}
	}
	record := job.Records[0]
	if strings.TrimSpace(firstNonEmpty(record.Markdown, stripHTML(record.HTML))) == "" {
		return cloudflareCrawlRecord{}, fmt.Errorf("cloudflare crawl record status=%s returned no content", record.Status)
	}
	return record, nil
}

type cloudflareStartResponse struct {
	Success bool   `json:"success"`
	Result  string `json:"result"`
}

type cloudflareJobResponse struct {
	Success bool               `json:"success"`
	Result  cloudflareCrawlJob `json:"result"`
}

type cloudflareCrawlJob struct {
	ID      string                  `json:"id"`
	Status  string                  `json:"status"`
	Records []cloudflareCrawlRecord `json:"records"`
}

type cloudflareCrawlRecord struct {
	URL      string                  `json:"url"`
	Status   string                  `json:"status"`
	Markdown string                  `json:"markdown"`
	HTML     string                  `json:"html"`
	Metadata cloudflareCrawlMetadata `json:"metadata"`
}

type cloudflareCrawlMetadata struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

type cloudflareCDPSession struct {
	SessionID            string `json:"sessionId"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type cloudflareCDPTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	Title                string `json:"title"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type cloudflareCDPPage struct {
	ReadyState string `json:"readyState"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Content    string `json:"content"`
}

type cdpResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *cdpError       `json:"error"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cdpEvaluateResult struct {
	Result struct {
		Type        string `json:"type"`
		Value       any    `json:"value"`
		Description string `json:"description"`
	} `json:"result"`
	ExceptionDetails any `json:"exceptionDetails"`
}

func firstEnvValue(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func envIntValue(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envDurationValue(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
