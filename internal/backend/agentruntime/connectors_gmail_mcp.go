package agentruntime

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

	mcpcore "claude-codex/internal/harness/mcp"
)

const defaultGmailAPIBaseURL = "https://gmail.googleapis.com/gmail/v1"

func (r *Runtime) callGmailRESTMCPFallback(ctx context.Context, call MCPConnectorToolCall, server MCPServerBinding, cause error) (mcpcore.ToolResult, bool, error) {
	if r == nil || normalizeConnectorProviderID(call.Provider) != "gmail" {
		return mcpcore.ToolResult{}, false, nil
	}
	if !gmailRESTMCPFallbackEnabled() {
		return mcpcore.ToolResult{}, false, nil
	}
	tool := strings.TrimSpace(call.ToolName)
	switch tool {
	case "search_threads", "get_thread", "list_labels":
	default:
		return mcpcore.ToolResult{}, false, nil
	}
	if cause != nil && !gmailMCPPermissionError(cause) {
		return mcpcore.ToolResult{}, false, nil
	}
	token, err := r.connectorTokenVault().GetToken(ctx, server.OAuthTokenRef)
	if err != nil {
		return mcpcore.ToolResult{}, true, err
	}
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return mcpcore.ToolResult{}, true, fmt.Errorf("gmail REST fallback requires connector access token")
	}
	client := &gmailRESTClient{
		baseURL: strings.TrimRight(firstNonEmptyString(os.Getenv("GMAIL_API_BASE_URL"), defaultGmailAPIBaseURL), "/"),
		token:   *token,
		client:  http.DefaultClient,
	}
	var output any
	switch tool {
	case "search_threads":
		output, err = client.searchThreads(ctx, call.Args)
	case "get_thread":
		output, err = client.getThreadFromArgs(ctx, call.Args)
	case "list_labels":
		output, err = client.listLabels(ctx)
	}
	if err != nil {
		return mcpcore.ToolResult{}, true, err
	}
	data, _ := json.Marshal(output)
	return mcpcore.ToolResult{Output: string(data)}, true, nil
}

func gmailRESTMCPFallbackEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(firstNonEmptyString(
		os.Getenv("AGENT_API_GMAIL_REST_FALLBACK_ENABLED"),
		os.Getenv("GMAIL_REST_FALLBACK_ENABLED"),
	)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func gmailMCPPermissionError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "permission") ||
		strings.Contains(text, "403") ||
		strings.Contains(text, "mcp tool error")
}

type gmailRESTClient struct {
	baseURL string
	token   ConnectorToken
	client  *http.Client
}

type gmailSearchArgs struct {
	Query      string `json:"query,omitempty"`
	Q          string `json:"q,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
	Max        int    `json:"max,omitempty"`
}

type gmailThreadArgs struct {
	ThreadID string `json:"thread_id,omitempty"`
	ThreadId string `json:"threadId,omitempty"`
	ID       string `json:"id,omitempty"`
}

type gmailSearchResponse struct {
	Source             string              `json:"source"`
	Query              string              `json:"query,omitempty"`
	ResultSizeEstimate int                 `json:"result_size_estimate,omitempty"`
	NextPageToken      string              `json:"next_page_token,omitempty"`
	Threads            []gmailThreadResult `json:"threads"`
}

type gmailThreadResult struct {
	ThreadID string               `json:"thread_id"`
	Messages []gmailMessageResult `json:"messages"`
}

type gmailMessageResult struct {
	ID       string `json:"id"`
	ThreadID string `json:"thread_id,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Date     string `json:"date,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
}

func (c *gmailRESTClient) searchThreads(ctx context.Context, raw json.RawMessage) (gmailSearchResponse, error) {
	var args gmailSearchArgs
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &args)
	}
	query := strings.TrimSpace(firstNonEmptyString(args.Query, args.Q))
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = args.Max
	}
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 10 {
		maxResults = 10
	}
	values := url.Values{}
	values.Set("maxResults", strconv.Itoa(maxResults))
	if query != "" {
		values.Set("q", query)
	}
	var listed struct {
		Messages []struct {
			ID       string `json:"id"`
			ThreadID string `json:"threadId"`
		} `json:"messages"`
		NextPageToken      string `json:"nextPageToken"`
		ResultSizeEstimate int    `json:"resultSizeEstimate"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/users/me/messages?"+values.Encode(), nil, &listed); err != nil {
		return gmailSearchResponse{}, err
	}
	out := gmailSearchResponse{
		Source:             "gmail_rest_fallback",
		Query:              query,
		ResultSizeEstimate: listed.ResultSizeEstimate,
		NextPageToken:      listed.NextPageToken,
		Threads:            []gmailThreadResult{},
	}
	seen := map[string]bool{}
	for _, message := range listed.Messages {
		threadID := strings.TrimSpace(message.ThreadID)
		if threadID == "" || seen[threadID] {
			continue
		}
		seen[threadID] = true
		thread, err := c.getThread(ctx, threadID)
		if err != nil {
			out.Threads = append(out.Threads, gmailThreadResult{ThreadID: threadID, Messages: []gmailMessageResult{{ID: message.ID, ThreadID: threadID}}})
			continue
		}
		out.Threads = append(out.Threads, thread)
	}
	return out, nil
}

func (c *gmailRESTClient) getThreadFromArgs(ctx context.Context, raw json.RawMessage) (gmailThreadResult, error) {
	var args gmailThreadArgs
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &args)
	}
	threadID := strings.TrimSpace(firstNonEmptyString(args.ThreadID, args.ThreadId, args.ID))
	if threadID == "" {
		return gmailThreadResult{}, fmt.Errorf("gmail get_thread requires thread_id")
	}
	return c.getThread(ctx, threadID)
}

func (c *gmailRESTClient) getThread(ctx context.Context, threadID string) (gmailThreadResult, error) {
	values := url.Values{}
	values.Set("format", "metadata")
	for _, header := range []string{"From", "To", "Subject", "Date"} {
		values.Add("metadataHeaders", header)
	}
	var thread struct {
		ID       string `json:"id"`
		Messages []struct {
			ID       string `json:"id"`
			ThreadID string `json:"threadId"`
			Snippet  string `json:"snippet"`
			Payload  struct {
				Headers []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"headers"`
			} `json:"payload"`
		} `json:"messages"`
	}
	path := "/users/me/threads/" + url.PathEscape(threadID) + "?" + values.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &thread); err != nil {
		return gmailThreadResult{}, err
	}
	out := gmailThreadResult{ThreadID: firstNonEmptyString(thread.ID, threadID), Messages: []gmailMessageResult{}}
	for _, message := range thread.Messages {
		headers := map[string]string{}
		for _, header := range message.Payload.Headers {
			headers[strings.ToLower(header.Name)] = header.Value
		}
		out.Messages = append(out.Messages, gmailMessageResult{
			ID:       message.ID,
			ThreadID: message.ThreadID,
			From:     headers["from"],
			To:       headers["to"],
			Subject:  headers["subject"],
			Date:     headers["date"],
			Snippet:  message.Snippet,
		})
	}
	return out, nil
}

func (c *gmailRESTClient) listLabels(ctx context.Context) (map[string]any, error) {
	var response struct {
		Labels []map[string]any `json:"labels"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/users/me/labels", nil, &response); err != nil {
		return nil, err
	}
	return map[string]any{"source": "gmail_rest_fallback", "labels": response.Labels}, nil
}

func (c *gmailRESTClient) doJSON(ctx context.Context, method, path string, body io.Reader, target any) error {
	client := c.client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", connectorAuthorizationHeader(c.token))
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gmail REST fallback failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(data, target)
}
