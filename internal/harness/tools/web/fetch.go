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

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type FetchTool struct {
	client          *http.Client
	allowedDomains  []string
	cloudflareCrawl *cloudflareCrawlClient
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
		return t.cloudflareCrawl.fetch(ctx, input, requestURL, t.allowedDomains)
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
		"depth":         0,
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
