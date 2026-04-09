package memdir

import (
	"regexp"
	"strings"
)

// SecretMatch identifies a high-confidence secret pattern found in content.
// Matched secret values are intentionally not returned.
type SecretMatch struct {
	RuleID string
	Label  string
}

type secretRule struct {
	id    string
	label string
	re    *regexp.Regexp
}

var anthropicPrefix = strings.Join([]string{"sk", "ant", "api"}, "-")

// Curated high-confidence rules adapted from the TS implementation. The goal
// is to prevent obvious credentials from being written into shared team memory.
var secretRules = []secretRule{
	{id: "aws-access-token", label: "AWS Access Token", re: regexp.MustCompile(`\b((?:A3T[A-Z0-9]|AKIA|ASIA|ABIA|ACCA)[A-Z2-7]{16})\b`)},
	{id: "anthropic-api-key", label: "Anthropic API Key", re: regexp.MustCompile(`\b(` + regexp.QuoteMeta(anthropicPrefix) + `03-[a-zA-Z0-9_\-]{93}AA)(?:[\x60'"\s;]|\\[nr]|$)`)},
	{id: "anthropic-admin-api-key", label: "Anthropic Admin API Key", re: regexp.MustCompile(`\b(sk-ant-admin01-[a-zA-Z0-9_\-]{93}AA)(?:[\x60'"\s;]|\\[nr]|$)`)},
	{id: "openai-api-key", label: "OpenAI API Key", re: regexp.MustCompile(`\b(sk-(?:proj|svcacct|admin)-(?:[A-Za-z0-9_-]{74}|[A-Za-z0-9_-]{58})T3BlbkFJ(?:[A-Za-z0-9_-]{74}|[A-Za-z0-9_-]{58})\b|sk-[a-zA-Z0-9]{20}T3BlbkFJ[a-zA-Z0-9]{20})(?:[\x60'"\s;]|\\[nr]|$)`)},
	{id: "github-pat", label: "GitHub PAT", re: regexp.MustCompile(`ghp_[0-9a-zA-Z]{36}`)},
	{id: "github-fine-grained-pat", label: "GitHub Fine-Grained PAT", re: regexp.MustCompile(`github_pat_\w{82}`)},
	{id: "github-app-token", label: "GitHub App Token", re: regexp.MustCompile(`(?:ghu|ghs)_[0-9a-zA-Z]{36}`)},
	{id: "github-oauth", label: "GitHub OAuth", re: regexp.MustCompile(`gho_[0-9a-zA-Z]{36}`)},
	{id: "gitlab-pat", label: "GitLab PAT", re: regexp.MustCompile(`glpat-[\w-]{20}`)},
	{id: "slack-bot-token", label: "Slack Bot Token", re: regexp.MustCompile(`xoxb-[0-9]{10,13}-[0-9]{10,13}[a-zA-Z0-9-]*`)},
	{id: "npm-access-token", label: "NPM Access Token", re: regexp.MustCompile(`\b(npm_[a-zA-Z0-9]{36})(?:[\x60'"\s;]|\\[nr]|$)`)},
	{id: "stripe-access-token", label: "Stripe Access Token", re: regexp.MustCompile(`\b((?:sk|rk)_(?:test|live|prod)_[a-zA-Z0-9]{10,99})(?:[\x60'"\s;]|\\[nr]|$)`)},
	{id: "private-key", label: "Private Key", re: regexp.MustCompile(`(?is)-----BEGIN[ A-Z0-9_-]{0,100}PRIVATE KEY(?: BLOCK)?-----[\s\S-]{64,}?-----END[ A-Z0-9_-]{0,100}PRIVATE KEY(?: BLOCK)?-----`)},
}

// ScanForSecrets returns at most one match per secret rule.
func ScanForSecrets(content string) []SecretMatch {
	matches := make([]SecretMatch, 0, len(secretRules))
	for _, rule := range secretRules {
		if rule.re.MatchString(content) {
			matches = append(matches, SecretMatch{
				RuleID: rule.id,
				Label:  rule.label,
			})
		}
	}
	return matches
}
