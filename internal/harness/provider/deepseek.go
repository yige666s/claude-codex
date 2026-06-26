package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultDeepSeekBaseURL = "https://api.deepseek.com"
	defaultDeepSeekModel   = "deepseek-chat"
)

// DeepSeekProvider uses DeepSeek's OpenAI-compatible chat completions endpoint.
type DeepSeekProvider struct {
	*OpenAIProvider
}

func NewDeepSeekProvider(cfg Config) (*DeepSeekProvider, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultDeepSeekBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = defaultDeepSeekModel
	}
	cfg.Provider = "deepseek"
	openai, err := NewOpenAIProvider(cfg)
	if err != nil {
		return nil, err
	}
	if transport := deepSeekFailoverTransport(cfg.BaseURL); transport != nil {
		openai.httpClient.Transport = transport
	}
	return &DeepSeekProvider{OpenAIProvider: openai}, nil
}

func (p *DeepSeekProvider) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	if strings.TrimSpace(request.Model) == "" {
		request.Model = defaultDeepSeekModel
	}
	request.Tools = deepSeekCompatibleTools(request.Tools)
	return p.OpenAIProvider.CreateMessage(ctx, request)
}

func (p *DeepSeekProvider) Name() string {
	return "deepseek"
}

func (p *DeepSeekProvider) SupportedModels() []string {
	return []string{
		"deepseek-chat",
		"deepseek-reasoner",
	}
}

func deepSeekCompatibleTools(tools []Tool) []Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		tool.InputSchema = geminiCompatibleToolSchema(tool.InputSchema)
		out = append(out, tool)
	}
	return out
}

func deepSeekFailoverTransport(baseURL string) http.RoundTripper {
	parsed, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return nil
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "api.deepseek.com") {
		return nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false
	transport.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		port := "443"
		if _, addressPort, err := net.SplitHostPort(address); err == nil && addressPort != "" {
			port = addressPort
		}
		dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
		tlsConfig := &tls.Config{
			ServerName: host,
			NextProtos: []string{"http/1.1"},
			MinVersion: tls.VersionTLS12,
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return tls.DialWithDialer(dialer, network, net.JoinHostPort(host, port), tlsConfig)
		}
		var lastErr error
		for _, addr := range addresses {
			ipAddress := net.JoinHostPort(addr.IP.String(), port)
			conn, err := dialer.DialContext(ctx, network, ipAddress)
			if err != nil {
				lastErr = err
				continue
			}
			tlsConn := tls.Client(conn, tlsConfig)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				lastErr = err
				_ = conn.Close()
				continue
			}
			return tlsConn, nil
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no resolved addresses for %s", host)
		}
		return nil, lastErr
	}
	return transport
}
