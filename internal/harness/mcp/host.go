package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	"claude-codex/internal/app/config"
)

type HostConfig struct {
	Name            string
	Provider        string
	Transport       string
	URL             string
	Command         []string
	Headers         map[string]string
	InProcessServer *Server
}

type ToolResult struct {
	Output string
}

type Host interface {
	DiscoverTools(ctx context.Context, cfg HostConfig) ([]ToolDefinition, error)
	CallTool(ctx context.Context, cfg HostConfig, toolName string, input json.RawMessage) (ToolResult, error)
	ListResources(ctx context.Context, cfg HostConfig) ([]ResourceDefinition, error)
	ReadResource(ctx context.Context, cfg HostConfig, uri string) ([]ResourceContent, error)
}

type RuntimeHost struct {
	httpClient *http.Client
}

func NewRuntimeHost(httpClient *http.Client) *RuntimeHost {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &RuntimeHost{httpClient: httpClient}
}

func (h *RuntimeHost) DiscoverTools(ctx context.Context, cfg HostConfig) ([]ToolDefinition, error) {
	client, err := h.clientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := client.Initialize(ctx); err != nil {
		return nil, err
	}
	return client.ListTools(ctx)
}

func (h *RuntimeHost) CallTool(ctx context.Context, cfg HostConfig, toolName string, input json.RawMessage) (ToolResult, error) {
	client, err := h.clientForConfig(cfg)
	if err != nil {
		return ToolResult{}, err
	}
	if _, err := client.Initialize(ctx); err != nil {
		return ToolResult{}, err
	}
	output, err := client.CallTool(ctx, toolName, input)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Output: output}, nil
}

func (h *RuntimeHost) ListResources(ctx context.Context, cfg HostConfig) ([]ResourceDefinition, error) {
	client, err := h.clientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := client.Initialize(ctx); err != nil {
		return nil, err
	}
	return client.ListResources(ctx)
}

func (h *RuntimeHost) ReadResource(ctx context.Context, cfg HostConfig, uri string) ([]ResourceContent, error) {
	client, err := h.clientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := client.Initialize(ctx); err != nil {
		return nil, err
	}
	return client.ReadResource(ctx, uri)
}

func (h *RuntimeHost) clientForConfig(cfg HostConfig) (*Client, error) {
	if cfg.InProcessServer != nil {
		return NewInProcessClient(cfg.InProcessServer), nil
	}
	return NewClientFromConfig(config.MCPServerConfig{
		Name:      cfg.Name,
		Transport: cfg.Transport,
		Command:   append([]string(nil), cfg.Command...),
		URL:       cfg.URL,
		Headers:   cloneHostStringMap(cfg.Headers),
	}, h.httpClient)
}

func cloneHostStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
