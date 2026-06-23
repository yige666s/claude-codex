package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"claude-codex/internal/app/config"
)

type Client struct {
	cfg          config.MCPServerConfig
	httpClient   *http.Client
	mu           sync.Mutex
	nextID       int64
	sessionID    string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	decoder      *json.Decoder
	inProcess    func(context.Context, Request) Response
	sdkControl   SendMCPMessageCallback
	Instructions string // populated from initialize handshake
}

func NewClientFromConfig(cfg config.MCPServerConfig, httpClient *http.Client) (*Client, error) {
	client := &Client{
		cfg:        cfg,
		httpClient: httpClient,
	}
	if client.httpClient == nil {
		client.httpClient = &http.Client{}
	}
	switch transport(cfg) {
	case "stdio":
		if len(cfg.Command) == 0 {
			return nil, fmt.Errorf("mcp stdio server %s has no command", cfg.Name)
		}
		cmd := exec.Command(cfg.Command[0], cfg.Command[1:]...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		cmd.Stderr = &bytes.Buffer{}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		client.cmd = cmd
		client.stdin = stdin
		client.decoder = json.NewDecoder(bufio.NewReader(stdout))
	case "sdk":
		handler, ok := getSDKControlHandler(cfg.Name)
		if !ok {
			return nil, fmt.Errorf("mcp sdk server %s has no registered handler", cfg.Name)
		}
		client.sdkControl = handler
	}
	return client, nil
}

// NewInProcessClient creates an MCP client that dispatches requests directly to
// an in-process server without spawning a subprocess or opening a socket.
func NewInProcessClient(server *Server) *Client {
	name := "in-process"
	if server != nil && strings.TrimSpace(server.Name) != "" {
		name = server.Name
	}
	return &Client{
		cfg:        config.MCPServerConfig{Name: name, Transport: "inprocess"},
		httpClient: &http.Client{},
		inProcess: func(ctx context.Context, request Request) Response {
			if server == nil {
				return Response{
					JSONRPC: "2.0",
					ID:      request.ID,
					Error:   &RPCError{Code: -32000, Message: "mcp server is nil"},
				}
			}
			return server.handleRequest(ctx, request)
		},
	}
}

func NewSDKControlClient(name string, handler SendMCPMessageCallback) *Client {
	return &Client{
		cfg:        config.MCPServerConfig{Name: name, Transport: "sdk"},
		httpClient: &http.Client{},
		sdkControl: handler,
	}
}

// Name returns the configured server name.
func (c *Client) Name() string { return c.cfg.Name }

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Name != "" {
		activeClients.Delete(c.cfg.Name)
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

func (c *Client) Initialize(ctx context.Context) (*Client, error) {
	var result InitializeResult
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "claude-codex", "version": "0.1.0"},
	}
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		// Non-fatal: many servers don't implement initialize strictly
		return c, nil
	}
	if result.Instructions != "" {
		const maxInstructionLen = 8192
		instr := result.Instructions
		if len(instr) > maxInstructionLen {
			instr = instr[:maxInstructionLen]
		}
		c.Instructions = instr
	}
	return c, nil
}

func (c *Client) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	var result ListToolsResult
	if err := c.call(ctx, "list_tools", nil, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, input json.RawMessage) (string, error) {
	var result CallToolResult
	params := CallToolParams{Name: name, Input: input}
	if err := c.call(ctx, "call_tool", params, &result); err != nil {
		return "", err
	}
	return result.Output, nil
}

func (c *Client) ListResources(ctx context.Context) ([]ResourceDefinition, error) {
	var result ListResourcesResult
	if err := c.call(ctx, "list_resources", nil, &result); err != nil {
		return nil, err
	}
	return result.Resources, nil
}

func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	var result ReadResourceResult
	if err := c.call(ctx, "read_resource", ReadResourceParams{URI: uri}, &result); err != nil {
		return nil, err
	}
	return result.Contents, nil
}

// SubscribeEvents connects to an HTTP/SSE MCP server and forwards events to
// handler until the context is cancelled, the stream ends, or handler fails.
func (c *Client) SubscribeEvents(ctx context.Context, handler func(Event) error) error {
	if transport(c.cfg) != "sse" {
		return fmt.Errorf("event subscription requires sse transport")
	}

	baseURL := strings.TrimRight(c.cfg.URL, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/sse", nil)
	if err != nil {
		return err
	}
	for key, value := range c.cfg.Headers {
		request.Header.Set(key, value)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("mcp sse subscribe failed (%d): %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)

	current := Event{}
	hasData := false
	dispatch := func() error {
		if current.Name == "" && !hasData {
			return nil
		}
		if current.Name == "" {
			current.Name = "message"
		}
		if err := handler(current); err != nil {
			return err
		}
		current = Event{}
		hasData = false
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if err := dispatch(); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
			continue
		case strings.HasPrefix(line, "event:"):
			current.Name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLine := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if !hasData {
				current.Data = append([]byte(nil), dataLine...)
				hasData = true
				continue
			}
			current.Data = append(current.Data, '\n')
			current.Data = append(current.Data, dataLine...)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	if err := dispatch(); err != nil {
		return err
	}
	return ctx.Err()
}

func (c *Client) call(ctx context.Context, method string, params any, target any) error {
	if c.inProcess != nil {
		return c.callInProcess(ctx, method, params, target)
	}
	if c.sdkControl != nil {
		return c.callSDK(ctx, method, params, target)
	}
	switch transport(c.cfg) {
	case "sse":
		return c.callHTTP(ctx, method, params, target)
	case "http", "streamable_http":
		return c.callStreamableHTTP(ctx, method, params, target)
	default:
		return c.callStdio(ctx, method, params, target)
	}
}

func (c *Client) callSDK(ctx context.Context, method string, params any, target any) error {
	c.mu.Lock()
	c.nextID++
	request := Request{JSONRPC: "2.0", ID: c.nextID, Method: method}
	c.mu.Unlock()

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		request.Params = data
	}

	response, err := c.sdkControl(c.cfg.Name, JSONRPCMessage{
		ID:      request.ID,
		Method:  request.Method,
		Params:  request.Params,
		JSONRPC: request.JSONRPC,
	})
	if err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("mcp error: %s", response.Error.Message)
	}
	if target == nil || len(response.Result) == 0 {
		return nil
	}
	return json.Unmarshal(response.Result, target)
}

func (c *Client) callInProcess(ctx context.Context, method string, params any, target any) error {
	c.mu.Lock()
	c.nextID++
	request := Request{JSONRPC: "2.0", ID: c.nextID, Method: method}
	c.mu.Unlock()

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		request.Params = data
	}

	type result struct {
		response Response
	}
	done := make(chan result, 1)
	go func() {
		done <- result{response: c.inProcess(ctx, request)}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case outcome := <-done:
		if outcome.response.Error != nil {
			return fmt.Errorf("mcp error: %s", outcome.response.Error.Message)
		}
		if target == nil || len(outcome.response.Result) == 0 {
			return nil
		}
		return json.Unmarshal(outcome.response.Result, target)
	}
}

func (c *Client) callStdio(ctx context.Context, method string, params any, target any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil || c.decoder == nil {
		return fmt.Errorf("stdio client is not initialized")
	}

	c.nextID++
	request := Request{JSONRPC: "2.0", ID: c.nextID, Method: method}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		request.Params = data
	}
	if err := json.NewEncoder(c.stdin).Encode(request); err != nil {
		return err
	}

	type result struct {
		response Response
		err      error
	}
	done := make(chan result, 1)
	go func() {
		var response Response
		done <- result{response: response, err: c.decoder.Decode(&response)}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case outcome := <-done:
		if outcome.err != nil {
			return outcome.err
		}
		if outcome.response.Error != nil {
			return fmt.Errorf("mcp error: %s", outcome.response.Error.Message)
		}
		if target == nil {
			return nil
		}
		return json.Unmarshal(outcome.response.Result, target)
	}
}

func (c *Client) callHTTP(ctx context.Context, method string, params any, target any) error {
	baseURL := strings.TrimRight(c.cfg.URL, "/")
	switch method {
	case "initialize":
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/initialize", nil)
		if err != nil {
			return err
		}
		for key, value := range c.cfg.Headers {
			request.Header.Set(key, value)
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		return json.NewDecoder(response.Body).Decode(target)
	case "list_tools":
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/tools", nil)
		if err != nil {
			return err
		}
		for key, value := range c.cfg.Headers {
			request.Header.Set(key, value)
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		return json.NewDecoder(response.Body).Decode(target)
	case "list_resources":
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/resources", nil)
		if err != nil {
			return err
		}
		for key, value := range c.cfg.Headers {
			request.Header.Set(key, value)
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		return json.NewDecoder(response.Body).Decode(target)
	case "call_tool":
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/call", bytes.NewReader(data))
		if err != nil {
			return err
		}
		request.Header.Set("content-type", "application/json")
		for key, value := range c.cfg.Headers {
			request.Header.Set(key, value)
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		return json.NewDecoder(response.Body).Decode(target)
	case "read_resource":
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/resource", bytes.NewReader(data))
		if err != nil {
			return err
		}
		request.Header.Set("content-type", "application/json")
		for key, value := range c.cfg.Headers {
			request.Header.Set(key, value)
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		return json.NewDecoder(response.Body).Decode(target)
	default:
		return fmt.Errorf("unsupported http method %s", method)
	}
}

func (c *Client) callStreamableHTTP(ctx context.Context, method string, params any, target any) error {
	c.mu.Lock()
	c.nextID++
	request := Request{JSONRPC: "2.0", ID: c.nextID, Method: streamableHTTPMethod(method)}
	sessionID := c.sessionID
	c.mu.Unlock()
	if params != nil {
		data, err := json.Marshal(streamableHTTPParams(method, params))
		if err != nil {
			return err
		}
		request.Params = data
	}
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.URL, "/"), bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "application/json, text/event-stream")
	httpReq.Header.Set("mcp-protocol-version", "2024-11-05")
	if sessionID != "" {
		httpReq.Header.Set("mcp-session-id", sessionID)
	}
	for key, value := range c.cfg.Headers {
		httpReq.Header.Set(key, value)
	}
	response, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("mcp http %s failed (%d): %s", request.Method, response.StatusCode, strings.TrimSpace(string(body)))
	}
	if value := strings.TrimSpace(response.Header.Get("mcp-session-id")); value != "" {
		c.mu.Lock()
		c.sessionID = value
		c.mu.Unlock()
	}
	body = firstStreamableHTTPJSONPayload(body)
	var rpc Response
	if err := json.Unmarshal(body, &rpc); err != nil {
		return err
	}
	if rpc.Error != nil {
		return fmt.Errorf("mcp error: %s", rpc.Error.Message)
	}
	if target == nil || len(rpc.Result) == 0 {
		return nil
	}
	if method == "call_tool" {
		output, err := streamableHTTPToolOutput(rpc.Result)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(CallToolResult{Output: output})
		return json.Unmarshal(data, target)
	}
	return json.Unmarshal(rpc.Result, target)
}

func streamableHTTPMethod(method string) string {
	switch method {
	case "list_tools":
		return "tools/list"
	case "call_tool":
		return "tools/call"
	case "list_resources":
		return "resources/list"
	case "read_resource":
		return "resources/read"
	default:
		return method
	}
}

func streamableHTTPParams(method string, params any) any {
	switch method {
	case "call_tool":
		if typed, ok := params.(CallToolParams); ok {
			return map[string]any{"name": typed.Name, "arguments": json.RawMessage(typed.Input)}
		}
	case "read_resource":
		return params
	}
	return params
}

func firstStreamableHTTPJSONPayload(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if !bytes.HasPrefix(trimmed, []byte("event:")) && !bytes.HasPrefix(trimmed, []byte("data:")) {
		return trimmed
	}
	for _, line := range bytes.Split(trimmed, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if payload, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			payload = bytes.TrimSpace(payload)
			if len(payload) > 0 {
				return payload
			}
		}
	}
	return trimmed
}

func streamableHTTPToolOutput(result json.RawMessage) (string, error) {
	var payload struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent any    `json:"structuredContent"`
		Output            string `json:"output"`
		IsError           bool   `json:"isError"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", err
	}
	if payload.IsError {
		var parts []string
		for _, item := range payload.Content {
			if strings.EqualFold(item.Type, "text") && strings.TrimSpace(item.Text) != "" {
				parts = append(parts, item.Text)
			}
		}
		if len(parts) > 0 {
			return "", fmt.Errorf("mcp tool error: %s", strings.Join(parts, "\n"))
		}
		return "", fmt.Errorf("mcp tool error")
	}
	if strings.TrimSpace(payload.Output) != "" {
		return payload.Output, nil
	}
	var parts []string
	for _, item := range payload.Content {
		if strings.EqualFold(item.Type, "text") && strings.TrimSpace(item.Text) != "" {
			parts = append(parts, item.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n"), nil
	}
	if payload.StructuredContent != nil {
		data, _ := json.Marshal(payload.StructuredContent)
		return string(data), nil
	}
	return string(result), nil
}

func transport(cfg config.MCPServerConfig) string {
	if strings.EqualFold(strings.TrimSpace(cfg.Transport), "inprocess") {
		return "inprocess"
	}
	if strings.TrimSpace(cfg.Transport) != "" {
		return cfg.Transport
	}
	if cfg.URL != "" {
		return "sse"
	}
	return "stdio"
}
