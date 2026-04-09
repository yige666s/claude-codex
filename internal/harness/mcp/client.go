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

	"github.com/ding/claude-code/claude-go/internal/app/config"
)

type Client struct {
	cfg          config.MCPServerConfig
	httpClient   *http.Client
	mu           sync.Mutex
	nextID       int64
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	decoder      *json.Decoder
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
	if transport(cfg) == "stdio" {
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
	}
	return client, nil
}

// Name returns the configured server name.
func (c *Client) Name() string { return c.cfg.Name }

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
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
		"clientInfo":      map[string]any{"name": "claude-go", "version": "0.1.0"},
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

func (c *Client) call(ctx context.Context, method string, params any, target any) error {
	switch transport(c.cfg) {
	case "sse":
		return c.callHTTP(ctx, method, params, target)
	default:
		return c.callStdio(ctx, method, params, target)
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
	default:
		return fmt.Errorf("unsupported http method %s", method)
	}
}

func transport(cfg config.MCPServerConfig) string {
	if strings.TrimSpace(cfg.Transport) != "" {
		return cfg.Transport
	}
	if cfg.URL != "" {
		return "sse"
	}
	return "stdio"
}
