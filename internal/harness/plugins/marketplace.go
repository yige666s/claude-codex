package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const officialGitHubOrg = "anthropics"

var allowedOfficialMarketplaceNames = map[string]bool{
	"claude-code-marketplace": true,
	"claude-code-plugins":     true,
	"claude-plugins-official": true,
	"anthropic-marketplace":   true,
	"anthropic-plugins":       true,
	"agent-skills":            true,
	"life-sciences":           true,
	"knowledge-work-plugins":  true,
}

var blockedOfficialNamePattern = regexp.MustCompile(`(?i)(?:official[^a-z0-9]*(anthropic|claude)|(?:anthropic|claude)[^a-z0-9]*official|^(?:anthropic|claude)[^a-z0-9]*(marketplace|plugins|official))`)

type MarketplaceSource struct {
	Source     string `json:"source,omitempty"`
	Type       string `json:"type,omitempty"`
	Repo       string `json:"repo,omitempty"`
	Owner      string `json:"owner,omitempty"`
	URL        string `json:"url,omitempty"`
	Path       string `json:"path,omitempty"`
	Ref        string `json:"ref,omitempty"`
	AutoUpdate *bool  `json:"autoUpdate,omitempty"`
}

func (s MarketplaceSource) SourceType() string {
	if strings.TrimSpace(s.Source) != "" {
		return strings.TrimSpace(s.Source)
	}
	return strings.TrimSpace(s.Type)
}

type KnownMarketplaces struct {
	Version      int                          `json:"version"`
	Marketplaces map[string]MarketplaceSource `json:"marketplaces"`
}

type KnownMarketplaceStore struct {
	Path string
}

func ValidateMarketplaceName(name string) error {
	trimmed := strings.TrimSpace(name)
	lower := strings.ToLower(trimmed)
	if trimmed == "" {
		return fmt.Errorf("marketplace name is required")
	}
	if strings.Contains(trimmed, " ") {
		return fmt.Errorf("marketplace name %q cannot contain spaces", name)
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, `\`) || strings.Contains(trimmed, "..") || trimmed == "." {
		return fmt.Errorf("marketplace name %q cannot contain path separators or dot traversal", name)
	}
	if lower == InlineMarketplaceName || lower == BuiltinMarketplaceName {
		return fmt.Errorf("marketplace name %q is reserved", name)
	}
	if allowedOfficialMarketplaceNames[lower] {
		return nil
	}
	for _, r := range trimmed {
		if r < 0x20 || r > 0x7e {
			return fmt.Errorf("marketplace name %q contains non-ASCII characters", name)
		}
	}
	if blockedOfficialNamePattern.MatchString(trimmed) {
		return fmt.Errorf("marketplace name %q impersonates an official Anthropic/Claude marketplace", name)
	}
	return nil
}

func IsOfficialMarketplaceName(name string) bool {
	return allowedOfficialMarketplaceNames[strings.ToLower(strings.TrimSpace(name))]
}

func ValidateOfficialNameSource(name string, source MarketplaceSource) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if !allowedOfficialMarketplaceNames[normalized] {
		return nil
	}

	switch source.SourceType() {
	case "github":
		repo := strings.ToLower(strings.TrimSpace(source.Repo))
		if repo == "" && source.Owner != "" {
			repo = strings.ToLower(strings.TrimSpace(source.Owner)) + "/"
		}
		if strings.HasPrefix(repo, officialGitHubOrg+"/") {
			return nil
		}
	case "git":
		url := strings.ToLower(strings.TrimSpace(source.URL))
		if strings.Contains(url, "github.com/"+officialGitHubOrg+"/") ||
			strings.Contains(url, "git@github.com:"+officialGitHubOrg+"/") {
			return nil
		}
	}
	return fmt.Errorf("marketplace name %q is reserved for official Anthropic marketplaces", name)
}

func (s KnownMarketplaceStore) Load() (KnownMarketplaces, error) {
	if s.Path == "" {
		return KnownMarketplaces{Version: 2, Marketplaces: map[string]MarketplaceSource{}}, nil
	}
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return KnownMarketplaces{Version: 2, Marketplaces: map[string]MarketplaceSource{}}, nil
	}
	if err != nil {
		return KnownMarketplaces{}, err
	}
	var known KnownMarketplaces
	if err := json.Unmarshal(data, &known); err != nil {
		return KnownMarketplaces{}, err
	}
	if known.Version == 0 {
		known.Version = 2
	}
	if known.Marketplaces == nil {
		known.Marketplaces = map[string]MarketplaceSource{}
	}
	for name, source := range known.Marketplaces {
		if err := ValidateMarketplaceName(name); err != nil {
			return KnownMarketplaces{}, err
		}
		if err := ValidateOfficialNameSource(name, source); err != nil {
			return KnownMarketplaces{}, err
		}
	}
	return known, nil
}

func (s KnownMarketplaceStore) Save(known KnownMarketplaces) error {
	if s.Path == "" {
		return nil
	}
	if known.Version == 0 {
		known.Version = 2
	}
	if known.Marketplaces == nil {
		known.Marketplaces = map[string]MarketplaceSource{}
	}
	for name, source := range known.Marketplaces {
		if err := ValidateMarketplaceName(name); err != nil {
			return err
		}
		if err := ValidateOfficialNameSource(name, source); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(known, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.Path, append(data, '\n'), 0o644)
}
