package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type SearchTool struct {
	client          *http.Client
	resolver        ipResolver
	defaultEndpoint string
	apiKey          string
	allowedDomains  []string
}

type searchInput struct {
	Query          string   `json:"query"`
	MaxResults     int      `json:"max_results,omitempty"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
}

func NewSearchTool(client *http.Client) *SearchTool {
	return NewSearchToolWithAllowlist(client, nil)
}

func NewSearchToolWithAllowlist(client *http.Client, allowedDomains []string) *SearchTool {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &SearchTool{
		client:          client,
		resolver:        net.DefaultResolver,
		defaultEndpoint: firstNonEmpty(firstEnvValue("AGENT_API_TAVILY_SEARCH_ENDPOINT"), "https://api.tavily.com/search"),
		apiKey:          firstEnvValue("AGENT_API_TAVILY_API_KEY", "TAVILY_API_KEY"),
		allowedDomains:  append([]string(nil), allowedDomains...),
	}
}

func (t *SearchTool) Name() string {
	return "WebSearch"
}

func (t *SearchTool) Description() string {
	return "Search the web and return a small set of result snippets."
}

func (t *SearchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"The search query to use"},"allowed_domains":{"type":"array","items":{"type":"string"},"description":"Only include search results from these domains"},"blocked_domains":{"type":"array","items":{"type":"string"},"description":"Never include search results from these domains"},"max_results":{"type":"integer","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`)
}

func (t *SearchTool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *SearchTool) IsConcurrencySafe() bool {
	return true // web search is read-only and safe to run concurrently
}

func (t *SearchTool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var input searchInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return toolkit.Result{}, err
	}
	if strings.TrimSpace(input.Query) == "" {
		return toolkit.Result{}, fmt.Errorf("query is required")
	}

	requestURL, err := validateTavilyEndpoint(t.defaultEndpoint, t.allowedDomains)
	if err != nil {
		return toolkit.Result{}, err
	}
	// Resolve before attaching credentials, then resolve again in the dialer.
	// The two checks prevent both ordinary SSRF and DNS rebinding from turning
	// the fixed Tavily endpoint into a credential-bearing request to a private IP.
	if _, err := validateFetchURL(ctx, requestURL.String(), t.allowedDomains, t.resolver); err != nil {
		return toolkit.Result{}, fmt.Errorf("Tavily endpoint blocked: %w", err)
	}

	apiKey := strings.TrimSpace(t.apiKey)
	if apiKey == "" {
		return toolkit.Result{}, fmt.Errorf("Tavily API key is required; set AGENT_API_TAVILY_API_KEY")
	}

	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 10 {
		maxResults = 10
	}

	payload := tavilySearchRequest{
		Query:          strings.TrimSpace(input.Query),
		SearchDepth:    "basic",
		MaxResults:     maxResults,
		IncludeAnswer:  true,
		IncludeDomains: cleanDomainList(input.AllowedDomains),
		ExcludeDomains: cleanDomainList(input.BlockedDomains),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return toolkit.Result{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), bytes.NewReader(encoded))
	if err != nil {
		return toolkit.Result{}, err
	}
	request.Header.Set("authorization", "Bearer "+apiKey)
	request.Header.Set("content-type", "application/json")
	request.Header.Set("accept", "application/json")
	request.Header.Set("user-agent", "claude-codex-phase2/1.0")

	response, err := tavilyHTTPClient(t.client, t.allowedDomains, t.resolver).Do(request)
	if err != nil {
		return toolkit.Result{}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return toolkit.Result{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return toolkit.Result{}, fmt.Errorf("tavily search failed: status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var searchResponse tavilySearchResponse
	if err := json.Unmarshal(body, &searchResponse); err != nil {
		return toolkit.Result{}, fmt.Errorf("decode tavily search response: %w", err)
	}
	results := filterTavilyResults(searchResponse.Results, input.AllowedDomains, input.BlockedDomains)
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return toolkit.Result{Output: formatTavilySearchOutput(searchResponse.Answer, results)}, nil
}

func validateTavilyEndpoint(raw string, allowedDomains []string) (*url.URL, error) {
	parsed, err := parseWebURL(raw)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("Tavily endpoint must use HTTPS")
	}
	if host != "api.tavily.com" && !strings.HasSuffix(host, ".tavily.com") {
		return nil, fmt.Errorf("Tavily endpoint host %q is not trusted", host)
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return nil, fmt.Errorf("Tavily endpoint port %q is not trusted", port)
	}
	if err := validateURLAllowed(parsed.String(), allowedDomains); err != nil {
		return nil, err
	}
	return parsed, nil
}

func tavilyHTTPClient(base *http.Client, allowedDomains []string, resolver ipResolver) *http.Client {
	if base == nil {
		base = &http.Client{Timeout: 20 * time.Second}
	}
	secureClient := publicNetworkHTTPClient(base, allowedDomains, resolver)
	client := *secureClient
	baseRedirect := base.CheckRedirect
	publicRedirect := secureClient.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if _, err := validateTavilyEndpoint(req.URL.String(), allowedDomains); err != nil {
			return fmt.Errorf("Tavily redirect blocked: %w", err)
		}
		if publicRedirect != nil {
			if err := publicRedirect(req, via); err != nil {
				return fmt.Errorf("Tavily redirect blocked: %w", err)
			}
		}
		if baseRedirect != nil {
			return baseRedirect(req, via)
		}
		return nil
	}
	return &client
}

type tavilySearchRequest struct {
	Query          string   `json:"query"`
	SearchDepth    string   `json:"search_depth,omitempty"`
	MaxResults     int      `json:"max_results,omitempty"`
	IncludeAnswer  bool     `json:"include_answer,omitempty"`
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
}

type tavilySearchResponse struct {
	Answer  string         `json:"answer"`
	Results []tavilyResult `json:"results"`
}

type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

func filterTavilyResults(results []tavilyResult, allowed, blocked []string) []tavilyResult {
	if len(allowed) == 0 && len(blocked) == 0 {
		return results
	}
	filtered := make([]tavilyResult, 0, len(results))
	for _, result := range results {
		host := resultHost(result.URL)
		if host == "" {
			continue
		}
		if len(allowed) > 0 && !domainListed(host, allowed) {
			continue
		}
		if domainListed(host, blocked) {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered
}

func resultHost(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func domainListed(host string, domains []string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	for _, domain := range domains {
		domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "www.")
		if domain == "" {
			continue
		}
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func cleanDomainList(domains []string) []string {
	cleaned := make([]string, 0, len(domains))
	for _, domain := range domains {
		domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "www.")
		if domain == "" {
			continue
		}
		cleaned = append(cleaned, domain)
	}
	return cleaned
}

func formatTavilySearchOutput(answer string, results []tavilyResult) string {
	var builder strings.Builder
	answer = strings.TrimSpace(answer)
	if answer != "" {
		fmt.Fprintf(&builder, "answer: %s", answer)
	}
	for _, result := range results {
		title := strings.TrimSpace(result.Title)
		rawURL := strings.TrimSpace(result.URL)
		content := strings.TrimSpace(result.Content)
		if title == "" && rawURL == "" && content == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		if title != "" && rawURL != "" {
			fmt.Fprintf(&builder, "%s - %s", title, rawURL)
		} else {
			builder.WriteString(firstNonEmpty(title, rawURL))
		}
		if content != "" {
			fmt.Fprintf(&builder, "\n%s", content)
		}
	}
	if builder.Len() == 0 {
		return "No search results."
	}
	return builder.String()
}

var tagPattern = regexp.MustCompile(`(?s)<[^>]+>`)

func stripHTML(value string) string {
	value = tagPattern.ReplaceAllString(value, " ")
	value = strings.ReplaceAll(value, "&nbsp;", " ")
	value = strings.ReplaceAll(value, "&amp;", "&")
	value = strings.ReplaceAll(value, "&lt;", "<")
	value = strings.ReplaceAll(value, "&gt;", ">")
	return strings.Join(strings.Fields(value), " ")
}
