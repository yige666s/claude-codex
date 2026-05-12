package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	defaultEndpoint string
	allowedDomains  []string
}

type searchInput struct {
	Query          string   `json:"query"`
	MaxResults     int      `json:"max_results,omitempty"`
	Endpoint       string   `json:"endpoint,omitempty"`
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
		defaultEndpoint: "https://duckduckgo.com/html/",
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
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"The search query to use"},"allowed_domains":{"type":"array","items":{"type":"string"},"description":"Only include search results from these domains"},"blocked_domains":{"type":"array","items":{"type":"string"},"description":"Never include search results from these domains"},"max_results":{"type":"integer"},"endpoint":{"type":"string"}},"required":["query"]}`)
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

	endpoint := strings.TrimSpace(input.Endpoint)
	if endpoint == "" {
		endpoint = t.defaultEndpoint
	}

	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return toolkit.Result{}, err
	}
	if err := validateURLAllowed(requestURL.String(), t.allowedDomains); err != nil {
		return toolkit.Result{}, err
	}

	values := requestURL.Query()
	values.Set("q", input.Query)
	requestURL.RawQuery = values.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return toolkit.Result{}, err
	}
	request.Header.Set("user-agent", "claude-codex-phase2/1.0")

	response, err := t.client.Do(request)
	if err != nil {
		return toolkit.Result{}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return toolkit.Result{}, err
	}

	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}

	results := filterDomainResults(parseSearchResults(string(body), maxResults*3), input.AllowedDomains, input.BlockedDomains)
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	if len(results) == 0 {
		results = []string{strings.TrimSpace(stripHTML(string(body)))}
	}

	return toolkit.Result{Output: strings.Join(results, "\n")}, nil
}

func filterDomainResults(results []string, allowed, blocked []string) []string {
	if len(allowed) == 0 && len(blocked) == 0 {
		return results
	}
	filtered := make([]string, 0, len(results))
	for _, result := range results {
		host := resultHost(result)
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

func resultHost(result string) string {
	idx := strings.LastIndex(result, " - ")
	if idx < 0 {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(result[idx+3:]))
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

var resultAnchorPattern = regexp.MustCompile(`(?is)<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)

func parseSearchResults(html string, maxResults int) []string {
	matches := resultAnchorPattern.FindAllStringSubmatch(html, -1)
	results := make([]string, 0, maxResults)
	for _, match := range matches {
		if len(results) >= maxResults {
			break
		}
		link := strings.TrimSpace(match[1])
		label := strings.TrimSpace(stripHTML(match[2]))
		if label == "" || link == "" {
			continue
		}
		results = append(results, fmt.Sprintf("%s - %s", label, link))
	}
	return results
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
