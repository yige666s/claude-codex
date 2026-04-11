package deeplink

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

const (
	Protocol       = "claude-cli"
	MaxQueryLength = 5000
	MaxCWDLength   = 4096
)

var repoSlugPattern = regexp.MustCompile(`^[\w.-]+/[\w.-]+$`)

type Action struct {
	Query string
	CWD   string
	Repo  string
}

func Parse(uri string) (Action, error) {
	normalized := normalizeURI(uri)
	if normalized == "" {
		return Action{}, fmt.Errorf("invalid deep link: expected %s:// scheme, got %q", Protocol, uri)
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return Action{}, fmt.Errorf("invalid deep link URL: %q", uri)
	}
	if parsed.Host != "open" {
		return Action{}, fmt.Errorf("unknown deep link action: %q", parsed.Host)
	}

	action := Action{
		Query: sanitizePrintable(parsed.Query().Get("q")),
		CWD:   parsed.Query().Get("cwd"),
		Repo:  parsed.Query().Get("repo"),
	}
	if action.CWD != "" {
		if !strings.HasPrefix(action.CWD, "/") && !regexp.MustCompile(`^[a-zA-Z]:[/\\]`).MatchString(action.CWD) {
			return Action{}, fmt.Errorf("invalid cwd in deep link: must be an absolute path, got %q", action.CWD)
		}
		if containsControlChars(action.CWD) {
			return Action{}, fmt.Errorf("deep link cwd contains disallowed control characters")
		}
		if len(action.CWD) > MaxCWDLength {
			return Action{}, fmt.Errorf("deep link cwd exceeds %d characters (got %d)", MaxCWDLength, len(action.CWD))
		}
	}
	if action.Repo != "" && !repoSlugPattern.MatchString(action.Repo) {
		return Action{}, fmt.Errorf("invalid repo in deep link: expected \"owner/repo\", got %q", action.Repo)
	}
	if action.Query != "" {
		if containsControlChars(action.Query) {
			return Action{}, fmt.Errorf("deep link query contains disallowed control characters")
		}
		if len(action.Query) > MaxQueryLength {
			return Action{}, fmt.Errorf("deep link query exceeds %d characters (got %d)", MaxQueryLength, len(action.Query))
		}
	}
	return action, nil
}

func Build(action Action) string {
	value := &url.URL{
		Scheme: Protocol,
		Host:   "open",
	}
	query := url.Values{}
	if strings.TrimSpace(action.Query) != "" {
		query.Set("q", action.Query)
	}
	if strings.TrimSpace(action.CWD) != "" {
		query.Set("cwd", action.CWD)
	}
	if strings.TrimSpace(action.Repo) != "" {
		query.Set("repo", action.Repo)
	}
	value.RawQuery = query.Encode()
	return value.String()
}

func normalizeURI(uri string) string {
	switch {
	case strings.HasPrefix(uri, Protocol+"://"):
		return uri
	case strings.HasPrefix(uri, Protocol+":"):
		return strings.Replace(uri, Protocol+":", Protocol+"://", 1)
	default:
		return ""
	}
}

func containsControlChars(value string) bool {
	for _, r := range value {
		if r <= 0x1f || r == 0x7f {
			return true
		}
	}
	return false
}

func sanitizePrintable(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			continue
		}
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}
