package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type ipResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

func splitDomains(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func validateURLAllowed(raw string, allowed []string) error {
	parsed, err := parseWebURL(raw)
	if err != nil {
		return err
	}
	if len(allowed) > 0 && !domainListed(parsed.Hostname(), allowed) {
		return fmt.Errorf("domain %q is not allowed", strings.ToLower(parsed.Hostname()))
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		return validatePublicIP(ip)
	}
	if isLocalHostname(parsed.Hostname()) {
		return fmt.Errorf("host %q is not publicly routable", parsed.Hostname())
	}
	return nil
}

func validateFetchURL(ctx context.Context, raw string, allowed []string, resolver ipResolver) (*url.URL, error) {
	if err := validateURLAllowed(raw, allowed); err != nil {
		return nil, err
	}
	parsed, _ := url.Parse(raw)
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolvePublicHost(ctx, resolver, parsed.Hostname())
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("host %q resolved to no addresses", parsed.Hostname())
	}
	return parsed, nil
}

func parseWebURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("url host is required")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("URL userinfo is not allowed")
	}
	return parsed, nil
}

func resolvePublicHost(ctx context.Context, resolver ipResolver, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if err := validatePublicIP(ip); err != nil {
			return nil, err
		}
		return []net.IP{ip}, nil
	}
	if isLocalHostname(host) {
		return nil, fmt.Errorf("host %q is not publicly routable", host)
	}
	resolved, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %q: %w", host, err)
	}
	addresses := make([]net.IP, 0, len(resolved))
	for _, address := range resolved {
		if err := validatePublicIP(address.IP); err != nil {
			return nil, fmt.Errorf("host %q: %w", host, err)
		}
		addresses = append(addresses, address.IP)
	}
	return addresses, nil
}

func validatePublicIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("invalid IP address")
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("IP address %q is not publicly routable", ip.String())
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 0x40 {
		return fmt.Errorf("IP address %q is carrier-grade private space", ip.String())
	}
	return nil
}

func isLocalHostname(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "localhost.localdomain"
}

func publicNetworkHTTPClient(base *http.Client, allowed []string, resolver ipResolver) *http.Client {
	if base == nil {
		base = &http.Client{}
	}
	client := *base
	baseRedirect := base.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if _, err := validateFetchURL(req.Context(), req.URL.String(), allowed, resolver); err != nil {
			return fmt.Errorf("redirect blocked: %w", err)
		}
		if baseRedirect != nil {
			return baseRedirect(req, via)
		}
		return nil
	}

	transport, ok := base.Transport.(*http.Transport)
	if base.Transport == nil {
		transport = http.DefaultTransport.(*http.Transport)
		ok = true
	}
	if !ok {
		// A custom RoundTripper is treated as an explicitly trusted test or
		// embedding boundary. Redirect validation still applies.
		return &client
	}
	cloned := transport.Clone()
	dialer := &net.Dialer{}
	cloned.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := resolvePublicHost(ctx, resolver, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range addresses {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("host %q resolved to no addresses", host)
		}
		return nil, lastErr
	}
	client.Transport = cloned
	return &client
}
