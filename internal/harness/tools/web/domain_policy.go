package web

import (
	"fmt"
	"net/url"
	"strings"
)

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
	if len(allowed) == 0 {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("url host is required")
	}
	if domainListed(host, allowed) {
		return nil
	}
	return fmt.Errorf("domain %q is not allowed", host)
}
