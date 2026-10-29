package sandboxnode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ProbeClient is the Broker-side connectivity client. It cannot submit jobs.
type ProbeClient struct {
	base   *url.URL
	token  string
	client *http.Client
}

func NewProbeClient(endpoint, token string, client *http.Client) (*ProbeClient, error) {
	base, err := url.Parse(endpoint)
	if err != nil || base == nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("invalid sandbox node endpoint")
	}
	ip := net.ParseIP(base.Hostname())
	if base.Scheme != "https" && !(base.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return nil, errors.New("sandbox node endpoint must use HTTPS or loopback HTTP")
	}
	if len(token) < 32 {
		return nil, errors.New("sandbox node token must contain at least 32 bytes")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &ProbeClient{base: base, token: token, client: &copyClient}, nil
}

func (c *ProbeClient) get(ctx context.Context, path string, out any) error {
	endpoint := *c.base
	endpoint.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sandbox node probe returned HTTP %d", resp.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 16<<10))
	if err := dec.Decode(out); err != nil {
		return err
	}
	return nil
}

func (c *ProbeClient) Health(ctx context.Context) error {
	var result struct {
		Status  string `json:"status"`
		Version string `json:"protocol_version"`
	}
	if err := c.get(ctx, "/v1/health", &result); err != nil {
		return err
	}
	if result.Status != "alive" || result.Version != ProtocolVersion {
		return errors.New("sandbox node health/version mismatch")
	}
	return nil
}

func (c *ProbeClient) Capabilities(ctx context.Context) (Capabilities, error) {
	var result Capabilities
	if err := c.get(ctx, "/v1/capabilities", &result); err != nil {
		return Capabilities{}, err
	}
	if strings.TrimSpace(result.ProtocolVersion) != ProtocolVersion {
		return Capabilities{}, errors.New("sandbox node protocol version mismatch")
	}
	return result, nil
}
